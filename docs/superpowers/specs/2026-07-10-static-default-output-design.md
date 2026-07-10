# Ponto 1.1.0: Static-default output design

**Status:** approved
**Date:** 2026-07-10
**Type:** breaking change (1.1.0)

## Goal

Make a single self-contained `ponto.html` the default output, add a `--open`
flag that opens it in the browser, and retire the HTTP server and its `/api`
endpoints entirely.

## Motivation

Ponto's default has been to build the visualisation assets and then serve them
on `0.0.0.0:7668`. That needs a running process, an open port, and a browser
pointed at localhost. For the common case (generate a plan view, look at it,
share it) a single HTML file you can open or attach is simpler and has no moving
parts. vite-plugin-singlefile already inlines the JS/CSS bundle into one
`index.html`, so producing a genuinely self-contained artifact is a small step
from the existing `--standalone` zip path.

## CLI surface

```
ponto                 # writes ./ponto.html   (was: serve on :7668)
ponto --open          # writes ./ponto.html, then opens it in the browser
ponto -o infra        # writes ./infra.html
ponto --gen-image     # writes ponto.svg / ponto.png (now rendered via file://)
ponto -i / --tui      # unchanged: interactive terminal UI
ponto summary ...     # unchanged subcommand
ponto --standalone    # deprecated no-op alias: warns, then writes ponto.html
```

### Removed

- The HTTP server: `startServer` and `server.go`.
- Endpoints `/api/`, `/health`, and the `enableCors` helper.
- The `--address` / `-a` flag and its `PONTO_ADDRESS` env binding.

### Kept / deprecated

- `--standalone` / `-s` stays registered so old scripts do not error. It now
  produces the default static output and prints a one-line deprecation warning
  to stderr.
- `-o` / `--output` still sets the base name; the default base name stays
  `ponto`, so the default file is `ponto.html`.

## Output artifact

A single `ponto.html`:

- Starts from the singlefile `ui/dist/index.html` (bundle already inlined).
- Has three inline `<script>` blocks injected immediately before `</head>`:
  `window.graph = {…}`, `window.map = {…}`, `window.rso = {…}`. These are the
  same three payloads the UI already reads in standalone mode (see
  `ui/src/App.vue` `standaloneGlobals()`), so no UI change is needed.
- Contains no external references that matter for rendering. The
  `<link rel="icon" href="./favicon.ico">` line is left as-is; it 404s
  harmlessly over `file://` and does not affect the visualisation.

The plan payload is not inlined: the web UI consumes only graph/map/rso. The
full plan is still available through `ponto summary` and the `--gen-image`
path, neither of which reads it from the HTML.

## Components

### New: `static.go`

```go
// generateStaticHTML reads the embedded singlefile index.html and writes a
// single self-contained file with the graph/map/rso payloads inlined as
// window globals before </head>.
func (r *ponto) generateStaticHTML(fe fs.FS, filename string) error
```

- Reads `index.html` from the embedded `fe` (the `ui/dist` sub-FS).
- Builds inline scripts via a small helper `payloadScript(name string, v any) (string, error)`
  that marshals `v` to JSON and returns `<script>window.<name> = <json></script>`.
- Splits the HTML on the first `</head>`, inserts the three scripts, rejoins,
  writes the file with `os.WriteFile`.
- Returns an error (does not `log.Fatal`) so callers control exit behaviour.

### New: `open.go`

```go
// openInBrowser opens path with the OS default handler. A failure to find or
// run an opener is logged as a warning and returns nil, so headless/CI runs
// still succeed with the file written.
func openInBrowser(path string) error
```

- Linux: `xdg-open`. macOS: `open`. Windows: `rundll32 url.dll,FileProtocolHandler`.
- Resolves `path` to an absolute path first.
- If the opener binary is not found (`exec.LookPath` fails) or the command
  errors, log `warning: could not open <path> automatically: <reason>` and
  return nil. Writing the file already succeeded; opening is best-effort.

### Changed: `main.go`

- `run()`:
  - Remove the server branch and the `frontendFS` construction used only for
    serving.
  - Default and `--standalone` branches both call `generateStaticHTML` to
    `<output>.html`. `--standalone` first prints the deprecation warning.
  - After a successful write, if `--open` is set, call `openInBrowser`.
  - Log `Wrote <file>` on success.
- Remove the `--address` flag from `outputFlags` and stop reading
  `v.GetString("address")`.
- Add `--open` to `outputFlags` as a bool (`false` default). It is not
  mutually exclusive with anything: it only acts on the default/standalone
  static path and is ignored (with no warning) for `--gen-image` and `--tui`.
- Bump `var version = "1.0.0"` to `"1.1.0"`.
- Update the root command `Long` description: no longer "By default it serves
  a web UI".

### Changed: `screenshot.go`

```go
func screenshot(fileURL, format, fileName string)
```

- Drop the `*http.Server` parameter and the final `s.Shutdown(...)`.
- Navigate to `fileURL` (a `file://` URL) instead of `http://<addr>`.
- The `--gen-image` flow in `run()` now: generate the static HTML to a temp
  file, build its `file://` URL, call `screenshot`, remove the temp file.
- `moveFile` is unchanged.

### Changed/removed: `zip.go`

- Delete the zip writer (`generateZip`, `AddEmbeddedToZip`, `AddFileToZip`) and
  the `archive/zip` import.
- `static.go` uses `os.WriteFile` and needs no temp-file helper. The
  `--gen-image` path creates its temp target with `os.CreateTemp` (then
  `generateStaticHTML` overwrites that path), so the old `createTempFile`
  helper is removed with the rest of `zip.go`.

### Deleted: `server.go`

Removed entirely.

## Docker and docs

- `Dockerfile`: drop `EXPOSE 7668`. The entrypoint still runs `ponto`; a
  container run now writes `ponto.html` into the mounted `/src` volume.
- README: rewrite the Quickstart and Docker sections around
  "run ponto, get `ponto.html`, open it", e.g.
  `docker run --rm -v $(pwd):/src ghcr.io/1stvamp/ponto` leaves `ponto.html`
  in the current directory. Remove `-p 7668:7668` and the "navigate to it"
  wording. Document `--open` for native use.
- `mise.toml`: existing tasks (`run:image-example`, `run:tui-example`, the
  summary tasks) are unaffected. No serve task exists to remove.

## Error handling

- `generateStaticHTML` returns errors for a missing/short `index.html` (no
  `</head>` found) or a write failure. `run()` surfaces them as the command
  error (exit 1).
- `openInBrowser` never hard-fails: a headless environment logs a warning and
  the run still exits 0 with the file written.
- `--gen-image` behaviour and exit semantics are unchanged from 1.0.0.

## Testing

- `static_test.go`: `generateStaticHTML` against a fake `fs.FS` whose
  `index.html` contains a `</head>` sentinel. Assert the output file exists,
  contains `window.graph`, `window.map`, `window.rso`, and that the injected
  scripts appear before `</head>`. Assert a clear error when `index.html` has
  no `</head>`.
- `payloadScript` unit test: marshals a value and wraps it in a `<script>`
  tag with the right global name; errors surface JSON marshal failures.
- `open_test.go`: `openInBrowser` on a bogus path in an environment with no
  opener returns nil (best-effort) and does not panic. Keep it hermetic; do
  not actually launch a browser.
- Existing `main_test.go`, `summary_test.go`, `tui_test.go`,
  `summarytui_test.go` continue to pass. Adjust any test that referenced the
  removed server/`--address`.

## Out of scope

- No change to the graph/map/rso/summary generation logic.
- No change to the Vue UI or its build.
- No new remote-viewing story to replace the server; Docker users mount a
  volume and open the written file.
```