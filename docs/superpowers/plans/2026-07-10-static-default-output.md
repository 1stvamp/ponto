# Static-default Output Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a single self-contained `ponto.html` the default output, add a `--open` flag to open it, and retire the HTTP server and `/api` endpoints.

**Architecture:** The Vue UI already reads its `graph`/`map`/`rso` payloads from `window.*` globals in standalone mode. We produce one HTML file by injecting those three payloads as inline `<script>` blocks into the vite-plugin-singlefile `index.html` (which already has the JS/CSS bundle inlined). The default `run()` path writes that file instead of serving; `--gen-image` screenshots it over `file://`; the server, `/api`, `/health`, `enableCors`, and `--address` are deleted.

**Tech Stack:** Go 1.24, cobra/pflag/viper, `//go:embed ui/dist`, chromedp (for `--gen-image`). No new dependencies.

## Global Constraints

- Module `ponto`, Go 1.24. No new Go dependencies.
- `var version` in `main.go` must read `"1.1.0"`.
- Use GitButler (`but`) for all commits on branch `feat/static-default-output`.
- No Claude attribution in commit messages.
- British spelling and no em/en dashes in docs and comments.
- `json.Marshal` HTML-escapes `<`, `>`, `&` by default, so inlined payloads are safe inside `<script>` tags; do not disable that escaping.
- Example plan fixtures already in the repo: `docs/design/examples/plan-mixed.json`, `docs/design/examples/plan-first-apply.json`.

---

### Task 1: Static HTML generator (`static.go`)

**Files:**
- Create: `static.go`
- Test: `static_test.go`

**Interfaces:**
- Consumes: the `ponto` struct fields `Graph Graph`, `Map *Map`, `RSO *ResourcesOverview` (defined in `main.go`).
- Produces:
  - `func payloadScript(name string, v any) (string, error)` — returns `<script>window.<name> = <json></script>`.
  - `func (r *ponto) generateStaticHTML(fe fs.FS, filename string) error` — reads `index.html` from `fe`, injects graph/map/rso payload scripts before `</head>`, writes `filename`.

- [ ] **Step 1: Write the failing tests**

Create `static_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestPayloadScript(t *testing.T) {
	s, err := payloadScript("graph", map[string]int{"a": 1})
	if err != nil {
		t.Fatal(err)
	}
	if s != `<script>window.graph = {"a":1}</script>` {
		t.Errorf("unexpected script: %q", s)
	}
}

func TestPayloadScriptEscapesHTML(t *testing.T) {
	s, err := payloadScript("rso", "</script><b>")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s, "</script><b>") {
		t.Errorf("payload not HTML-escaped, could break out of the script tag: %q", s)
	}
	if !strings.Contains(s, `<`) {
		t.Errorf("expected escaped < (\\u003c) in %q", s)
	}
}

func TestGenerateStaticHTMLInlinesPayloads(t *testing.T) {
	fe := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte("<html><head><title>x</title></head><body><div id=app></div></body></html>"),
		},
	}
	r := &ponto{}
	out := filepath.Join(t.TempDir(), "ponto.html")
	if err := r.generateStaticHTML(fe, out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	for _, want := range []string{"window.graph", "window.map", "window.rso"} {
		if !strings.Contains(html, want) {
			t.Errorf("output missing %q", want)
		}
	}
	if strings.Index(html, "window.graph") > strings.Index(html, "</head>") {
		t.Error("payload scripts must be injected before </head>")
	}
}

func TestGenerateStaticHTMLErrorsWithoutHead(t *testing.T) {
	fe := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html><body></body></html>")},
	}
	r := &ponto{}
	err := r.generateStaticHTML(fe, filepath.Join(t.TempDir(), "x.html"))
	if err == nil {
		t.Fatal("expected an error when index.html has no </head>")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./... -run 'PayloadScript|GenerateStaticHTML' -v`
Expected: FAIL — `undefined: payloadScript` / `r.generateStaticHTML undefined`.

- [ ] **Step 3: Write `static.go`**

Create `static.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// payloadScript marshals v to JSON and wraps it as an inline script that sets a
// window global. json.Marshal HTML-escapes <, > and & by default, so the JSON
// cannot break out of the surrounding <script> tag.
func payloadScript(name string, v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("error producing %s JSON: %w", name, err)
	}
	return fmt.Sprintf("<script>window.%s = %s</script>", name, string(b)), nil
}

// generateStaticHTML reads the embedded singlefile index.html (its JS/CSS
// bundle is already inlined by vite-plugin-singlefile) and writes a single
// self-contained file with the graph/map/rso payloads injected as window
// globals immediately before </head>. The Vue UI reads these globals in
// standalone mode (see ui/src/App.vue standaloneGlobals()).
func (r *ponto) generateStaticHTML(fe fs.FS, filename string) error {
	raw, err := fs.ReadFile(fe, "index.html")
	if err != nil {
		return fmt.Errorf("unable to read embedded index.html: %w", err)
	}

	parts := strings.SplitN(string(raw), "</head>", 2)
	if len(parts) != 2 {
		return fmt.Errorf("embedded index.html has no </head>; cannot inject payloads")
	}

	var scripts strings.Builder
	for _, p := range []struct {
		name string
		v    any
	}{
		{"graph", r.Graph},
		{"map", r.Map},
		{"rso", r.RSO},
	} {
		s, err := payloadScript(p.name, p.v)
		if err != nil {
			return err
		}
		scripts.WriteString(s)
	}

	content := parts[0] + scripts.String() + "</head>" + parts[1]
	if err := os.WriteFile(filename, []byte(content), 0o644); err != nil {
		return fmt.Errorf("unable to write %s: %w", filename, err)
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./... -run 'PayloadScript|GenerateStaticHTML' -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
but diff
but commit feat/static-default-output -c -m "feat(static): single self-contained HTML generator" --changes <static.go id>,<static_test.go id>
```

---

### Task 2: Browser opener (`open.go`)

**Files:**
- Create: `open.go`
- Test: `open_test.go`

**Interfaces:**
- Produces:
  - `func browserOpenArgs(goos, path string) (string, []string)` — pure mapping from GOOS to opener binary + args.
  - `func openInBrowser(path string) error` — best-effort open; logs a warning and returns nil if there is no opener or it fails.

- [ ] **Step 1: Write the failing test**

Create `open_test.go`:

```go
package main

import "testing"

func TestBrowserOpenArgs(t *testing.T) {
	cases := []struct {
		goos     string
		wantBin  string
		wantArg0 string
	}{
		{"linux", "xdg-open", "/tmp/ponto.html"},
		{"darwin", "open", "/tmp/ponto.html"},
		{"windows", "rundll32", "url.dll,FileProtocolHandler"},
	}
	for _, c := range cases {
		t.Run(c.goos, func(t *testing.T) {
			bin, args := browserOpenArgs(c.goos, "/tmp/ponto.html")
			if bin != c.wantBin {
				t.Errorf("bin = %q, want %q", bin, c.wantBin)
			}
			if len(args) == 0 || args[0] != c.wantArg0 {
				t.Errorf("args = %v, want first %q", args, c.wantArg0)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./... -run TestBrowserOpenArgs -v`
Expected: FAIL — `undefined: browserOpenArgs`.

- [ ] **Step 3: Write `open.go`**

Create `open.go`:

```go
package main

import (
	"log"
	"os/exec"
	"path/filepath"
	"runtime"
)

// browserOpenArgs maps an OS to the command that opens a file with the default
// handler. Windows uses rundll32 so a plain path with no protocol still opens.
func browserOpenArgs(goos, path string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{path}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", path}
	default:
		return "xdg-open", []string{path}
	}
}

// openInBrowser opens path with the OS default handler. Opening is best-effort:
// on a headless or CI machine with no opener, it logs a warning and returns nil
// so the run still succeeds with the file already written.
func openInBrowser(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	bin, args := browserOpenArgs(runtime.GOOS, abs)
	resolved, err := exec.LookPath(bin)
	if err != nil {
		log.Printf("warning: could not open %s automatically: %s not found", abs, bin)
		return nil
	}
	if err := exec.Command(resolved, args...).Start(); err != nil {
		log.Printf("warning: could not open %s automatically: %s", abs, err)
		return nil
	}
	return nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./... -run TestBrowserOpenArgs -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
but diff
but commit feat/static-default-output -c -m "feat(open): cross-platform best-effort browser opener" --changes <open.go id>,<open_test.go id>
```

---

### Task 3: Rewire `run()`, retarget `--gen-image`, delete server and zip

**Files:**
- Modify: `main.go` (flags, `run()`, `Long`, `version`, remove `enableCors` and `net/http` import)
- Modify: `screenshot.go` (drop `*http.Server`, navigate `file://`)
- Delete: `server.go`
- Delete: `zip.go`

**Interfaces:**
- Consumes: `r.generateStaticHTML(fe, filename)` (Task 1), `openInBrowser(path)` (Task 2).
- Produces: new default CLI behaviour; `func screenshot(fileURL, format, fileName string)`.

- [ ] **Step 1: Delete `server.go` and `zip.go`**

```bash
rm server.go zip.go
```

- [ ] **Step 2: Rewrite `screenshot.go`**

Replace the whole file with (only the `screenshot` signature/body change; `moveFile` is unchanged):

```go
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/chromedp"
)

// Heavily inspired by: https://github.com/chromedp/examples/blob/master/download_file/main.go
func screenshot(fileURL, format, fileName string) {
	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()

	// create a timeout as a safety net to prevent any infinite wait loops
	ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// Which export button to drive, and the resulting file extension.
	clickSelector := "#saveGraph"
	ext := "svg"
	if format == "png" {
		clickSelector = "#savePng"
		ext = "png"
	}

	// this will be used to capture the file name later
	var downloadGUID string

	downloadComplete := make(chan bool)
	chromedp.ListenTarget(ctx, func(v interface{}) {
		if ev, ok := v.(*browser.EventDownloadProgress); ok {
			if ev.State == browser.DownloadProgressStateCompleted {
				downloadGUID = ev.GUID
				close(downloadComplete)
			}
		}
	})

	if err := chromedp.Run(ctx, chromedp.Tasks{
		browser.SetDownloadBehavior(browser.SetDownloadBehaviorBehaviorAllowAndName).
			WithDownloadPath(os.TempDir()).
			WithEventsEnabled(true),

		chromedp.Navigate(fileURL),
		// wait for graph to be visible
		chromedp.WaitVisible(`#cytoscape-div`),
		// find and click the export button ("Save Graph" for SVG, "Save PNG" for PNG)
		chromedp.Click(clickSelector, chromedp.NodeVisible),
	}); err != nil && !strings.Contains(err.Error(), "net::ERR_ABORTED") {
		// Note: Ignoring the net::ERR_ABORTED page error is essential here since downloads
		// will cause this error to be emitted, although the download will still succeed.
		log.Fatal(err)
	}
	// Don't block forever: if the download never fires (e.g. the export failed)
	// the context timeout needs to be able to kill us rather than hang.
	select {
	case <-downloadComplete:
	case <-ctx.Done():
		log.Fatalf("Timed out waiting for the %s export to download", ext)
	}

	dest := fmt.Sprintf("./%s.%s", fileName, ext)
	e := moveFile(fmt.Sprintf("%v/%v", os.TempDir(), downloadGUID), dest)
	if e != nil {
		log.Fatal(e)
	}

	log.Printf("Image generation complete: %s", dest)
}

// This function resolves the "invalid cross-device link" error for moving files
// between volumes for Docker.
// https://gist.github.com/var23rav/23ae5d0d4d830aff886c3c970b8f6c6b
func moveFile(sourcePath, destPath string) error {
	inputFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("Couldn't open source file: %s", err)
	}
	outputFile, err := os.Create(destPath)
	if err != nil {
		inputFile.Close()
		return fmt.Errorf("Couldn't open dest file: %s", err)
	}
	defer outputFile.Close()
	_, err = io.Copy(outputFile, inputFile)
	inputFile.Close()
	if err != nil {
		return fmt.Errorf("Writing to output file failed: %s", err)
	}
	// The copy was successful, so now delete the original file
	err = os.Remove(sourcePath)
	if err != nil {
		return fmt.Errorf("Failed removing original file: %s", err)
	}
	return nil
}
```

- [ ] **Step 3: Edit `main.go` flags and metadata**

In `newRootCmd()`:

Replace the `standalone` mode-flag description line:

```go
	modeFlags.BoolP("standalone", "s", false, "Deprecated: static HTML output is now the default")
```

Remove the `--address` output flag line entirely (delete this line):

```go
	outputFlags.StringP("address", "a", "0.0.0.0:7668", "Host:port for the Ponto server")
```

Add an `--open` output flag (place it right after the `output` flag line):

```go
	outputFlags.Bool("open", false, "Open the generated HTML in your browser")
```

Change the root command `Long` description to:

```go
		Long: "Ponto renders a Terraform plan or state as an interactive graph.\n" +
			"By default it writes a single self-contained HTML file; it can also emit\n" +
			"a static image or an interactive terminal UI.",
```

Change the `version` var near the top of the file:

```go
var version = "1.1.0"
```

In the `SetUsageFunc` block, change the output section header from `"Output and server:"` to `"Output:"`:

```go
		fmt.Fprintf(out, "Output:\n%s\n", outputFlags.FlagUsages())
```

- [ ] **Step 4: Rewrite the `run()` body in `main.go`**

First remove the now-dead `GenImage` field. Only the deleted `server.go` read it; `screenshot` takes the format as an argument. Delete the `GenImage bool` line from the `ponto` struct, and delete the `r.GenImage = genImage` assignment line in `run()` (keep `r.ImageFormat = imageFormat`; the gen-image branch reads `r.ImageFormat`).

Then replace the body from `fe, err := fs.Sub(...)` through the end of `run()` (the old `fs.Sub`, `frontendFS`, `standalone`, and `startServer` block) with:

```go
	fe, err := fs.Sub(frontend, "ui/dist")
	if err != nil {
		return err
	}

	// --gen-image: render the static HTML in a headless browser over file://
	// and drive its export button, then clean up the temp file.
	if genImage {
		tmp, err := os.CreateTemp("", "ponto-*.html")
		if err != nil {
			return err
		}
		tmpName := tmp.Name()
		tmp.Close()
		defer os.Remove(tmpName)

		if err := r.generateStaticHTML(fe, tmpName); err != nil {
			return err
		}
		screenshot("file://"+tmpName, r.ImageFormat, r.Output)
		return nil
	}

	if v.GetBool("standalone") {
		log.Println("warning: --standalone is deprecated; static HTML output is now the default")
	}

	htmlName := fmt.Sprintf("%s.html", r.Output)
	if err := r.generateStaticHTML(fe, htmlName); err != nil {
		return err
	}
	log.Printf("Wrote %s", htmlName)

	if v.GetBool("open") {
		return openInBrowser(htmlName)
	}
	return nil
```

- [ ] **Step 5: Remove `enableCors` and the `net/http` import from `main.go`**

Delete the function at the bottom of `main.go`:

```go
func enableCors(w *http.ResponseWriter) {
	(*w).Header().Set("Access-Control-Allow-Origin", "*")
}
```

Then remove the now-unused `"net/http"` line from the `main.go` import block.

- [ ] **Step 6: Build and run gofmt/vet**

Run: `gofmt -l . && go vet ./... && go build -o /tmp/ponto-11 .`
Expected: no output from `gofmt -l`, no vet errors, build succeeds.

- [ ] **Step 7: Verify the removed flags are gone**

Run: `/tmp/ponto-11 --address 0.0.0.0:9999 2>&1; echo "exit=$?"`
Expected: `Error: unknown flag: --address` and a non-zero exit.

Run: `/tmp/ponto-11 --help 2>&1 | grep -c address; echo "---"; /tmp/ponto-11 --help 2>&1 | grep -- --open`
Expected: `0` (no address mention) and a line showing `--open`.

- [ ] **Step 8: Verify the default writes a self-contained HTML**

Run:
```bash
cd /tmp && /tmp/ponto-11 --plan-json-path "$(git -C ~/projects/ponto rev-parse --show-toplevel)/docs/design/examples/plan-mixed.json" -o /tmp/ponto-static-test 2>&1
ls -l /tmp/ponto-static-test.html
grep -c "window.graph" /tmp/ponto-static-test.html
grep -c "window.map" /tmp/ponto-static-test.html
grep -c "window.rso" /tmp/ponto-static-test.html
```
Expected: `Wrote /tmp/ponto-static-test.html` logged, the file exists and is non-trivial in size, and each grep prints `1`.

- [ ] **Step 9: Verify the deprecated `--standalone` still works with a warning**

Run:
```bash
cd /tmp && /tmp/ponto-11 --standalone --plan-json-path "$(git -C ~/projects/ponto rev-parse --show-toplevel)/docs/design/examples/plan-mixed.json" -o /tmp/ponto-dep-test 2>&1 | grep -i deprecated
ls /tmp/ponto-dep-test.html
```
Expected: the deprecation warning is printed and `ponto-dep-test.html` exists.

- [ ] **Step 10: Run the full test suite**

Run: `go test ./...`
Expected: PASS (all packages).

- [ ] **Step 11: Commit**

```bash
but diff
but commit feat/static-default-output -c -m "feat(cli): write self-contained HTML by default, retire server" --changes <all main.go/screenshot.go/server.go/zip.go change ids>
```

---

### Task 4: Docs and Docker

**Files:**
- Modify: `Dockerfile` (remove `EXPOSE 7668`)
- Modify: `README.md` (Quickstart, Docker, plan-file, and native sections)

**Interfaces:** none (documentation and container metadata only).

- [ ] **Step 1: Remove the exposed port from `Dockerfile`**

Find and delete the `EXPOSE 7668` line in `Dockerfile`. (Verify with `grep -n EXPOSE Dockerfile` first; remove every `EXPOSE 7668`.)

- [ ] **Step 2: Rewrite the README Quickstart and Docker sections**

Replace the Docker quickstart block so it drops `-p 7668:7668` and the "navigate to it" wording. Use this content (adjust surrounding prose to match the file's existing voice):

````markdown
## Quickstart

The fastest way to get up and running with Ponto is through Docker.

Run the following in any Terraform workspace to generate a visualisation. This
mounts your current directory into the container and writes a single
self-contained `ponto.html` back into it:

```
$ docker run --rm -v $(pwd):/src ghcr.io/1stvamp/ponto
2021/07/02 06:46:23 Starting Ponto...
2021/07/02 06:46:25 Done generating assets.
2021/07/02 06:46:25 Wrote ponto.html
```

Open the resulting `ponto.html` in any browser. It is fully self-contained, so
you can also attach it to a PR or email it.

Or run it natively in a Terraform workspace, with `ponto` on your `PATH` (see
[Installation](#installation)) and `terraform` or `tofu` available:

```shell
$ ponto            # writes ./ponto.html
$ ponto --open     # writes ./ponto.html and opens it in your browser
```
````

- [ ] **Step 3: Update the plan-file section**

Replace the plan-file Docker example so it no longer maps a port:

````markdown
```
$ docker run --rm -v $(pwd)/plan.json:/src/plan.json ghcr.io/1stvamp/ponto:latest --plan-json-path=plan.json
```
````

Remove any remaining prose that says Ponto "runs on `0.0.0.0:7668`" or tells the
reader to navigate to that address. Where the old text described serving, say
Ponto writes `ponto.html`.

- [ ] **Step 4: Verify no stale server references remain in the README**

Run: `grep -n "7668\|navigate to it\|is running on" README.md`
Expected: no matches (empty output).

- [ ] **Step 5: Commit**

```bash
but diff
but commit feat/static-default-output -c -m "docs: static HTML workflow, drop server port from README and Dockerfile" --changes <Dockerfile and README.md change ids>
```

---

## Notes for the implementer

- `--open` is intentionally ignored (no warning) for `--gen-image` and `--tui`; those modes never reach the default static branch in `run()`.
- Do not inline the `plan` payload: the web UI reads only `graph`/`map`/`rso`. The full plan is still served by `ponto summary` and used by `--gen-image`.
- The `<link rel="icon" href="./favicon.ico">` in `index.html` is left as-is; it 404s harmlessly over `file://` and does not affect rendering.
- `openInBrowser` uses `.Start()` (not `.Run()`) so Ponto does not block waiting for the browser process to exit.
