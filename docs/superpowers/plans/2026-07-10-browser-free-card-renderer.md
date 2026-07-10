# Browser-free Card Renderer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render the `ponto summary --format image` card in-process with `tdewolff/canvas` and embedded Go fonts (PNG and SVG), removing the headless-Chrome dependency from the card path.

**Architecture:** A new `card.go` draws the fixed-layout summary card with `tdewolff/canvas`, reproducing the blocks and colours of the current HTML card, and writes PNG or SVG via `renderers.Write`. `renderCardPNG` and `renderSummaryCardHTML` are deleted and `summary.go` drops its `chromedp` import; `screenshot.go` keeps chromedp for the graph. A new `--image-format png|svg` flag selects the output.

**Tech Stack:** Go 1.24, cobra/pflag/viper, `github.com/tdewolff/canvas` (new), `golang.org/x/image/font/gofont/{goregular,gobold,gomono}` (new).

## Global Constraints

- Module `ponto`, Go 1.24. This plan intentionally ADDS `github.com/tdewolff/canvas` and `golang.org/x/image/font/gofont/*`; it does not remove chromedp (the graph still needs it).
- Exact card colours and font sizes are copied verbatim from `renderSummaryCardHTML` in `summary.go` (enumerated in Task 1).
- Tier indicators are vector shapes: `dots` -> filled circle; `signs` -> danger octagon, caution triangle, safe check; `none` -> nothing. Never attempt colour-emoji glyphs.
- Fonts come from the `gofont` byte slices; ship no `.ttf` asset files.
- `--image-format` accepts only `png` or `svg`; default `png`.
- Use GitButler (`but`) for commits on branch `feat/browser-free-card-renderer`; no Claude attribution; British spelling and no em/en dashes in comments and docs.
- Bundled fixture config for manual runs: `example/random-test`.

---

### Task 1: `card.go` browser-free renderer

**Files:**
- Create: `card.go`
- Create: `card_test.go`
- Modify: `go.mod`, `go.sum` (via `go get`)

**Interfaces:**
- Consumes: `summaryModel`, `tierGroup`, `resChange`, `tierTokens`, `emojiSets`, `verdicts`, `titleCase`, `verdictEmoji`/`tierEmoji` semantics (all in `summary.go`, same package).
- Produces: `func renderCard(m summaryModel, emoji, format, outPath string) error`.

- [ ] **Step 1: Add the dependencies**

Run:
```bash
go get github.com/tdewolff/canvas@latest
go get golang.org/x/image/font/gofont/goregular golang.org/x/image/font/gofont/gobold golang.org/x/image/font/gofont/gomono
go mod tidy
```
Expected: `go.mod` gains `github.com/tdewolff/canvas` and `golang.org/x/image`. If the module proxy is unreachable in this environment, stop and report BLOCKED (network access to the Go proxy is required for this task).

- [ ] **Step 2: Confirm the canvas API symbols compile**

Before writing the renderer, verify the exact symbol names in the resolved version (the file-based `LoadFontFile` is documented, the byte-slice loader and shape constructors are used below):
```bash
go doc github.com/tdewolff/canvas.FontFamily | grep -i load
go doc github.com/tdewolff/canvas | grep -iE "func (Circle|Rectangle|RoundedRectangle|RegularPolygon)"
go doc github.com/tdewolff/canvas/renderers.Write
```
Expected: a `LoadFont(b []byte, index int, style FontStyle) error` (byte loader), the shape constructors `Circle`, `RoundedRectangle`, `RegularPolygon`, and `renderers.Write(filename string, c *canvas.Canvas, opts ...interface{})`. If a name differs, use the actual name from `go doc` in the code below (adjust `LoadFont`/shape calls accordingly); everything else in the plan stays the same.

- [ ] **Step 3: Write the failing tests**

Create `card_test.go`:
```go
package main

import (
	"bytes"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleModel() summaryModel {
	danger := tierTokens["danger"]
	caution := tierTokens["caution"]
	safe := tierTokens["safe"]
	return summaryModel{
		verdict:   "danger",
		counts:    summaryCounts{add: 1, change: 1, destroy: 1, replace: 1, total: 4},
		tierCount: map[string]int{"danger": 2, "caution": 1, "safe": 1},
		groups: []tierGroup{
			{tierToken: danger, resources: []resChange{
				{action: "destroy", glyph: "−", tier: "danger", short: "aws_s3_bucket.logs"},
				{action: "replace", glyph: "±", tier: "danger", short: "aws_instance.web"},
			}},
			{tierToken: caution, resources: []resChange{
				{action: "update", glyph: "~", tier: "caution", short: "aws_iam_role.app"},
			}},
			{tierToken: safe, resources: []resChange{
				{action: "create", glyph: "+", tier: "safe", short: "aws_sns_topic.alerts"},
			}},
		},
		resources: []resChange{
			{action: "destroy", glyph: "−", tier: "danger", short: "aws_s3_bucket.logs"},
			{action: "replace", glyph: "±", tier: "danger", short: "aws_instance.web"},
			{action: "update", glyph: "~", tier: "caution", short: "aws_iam_role.app"},
			{action: "create", glyph: "+", tier: "safe", short: "aws_sns_topic.alerts"},
		},
	}
}

func TestRenderCardPNG(t *testing.T) {
	for _, set := range []string{"dots", "signs", "none"} {
		t.Run(set, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "card.png")
			if err := renderCard(sampleModel(), set, "png", out); err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")) {
				t.Errorf("output is not a PNG (bad magic)")
			}
			if len(b) < 1024 {
				t.Errorf("PNG suspiciously small: %d bytes", len(b))
			}
			cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
			if err != nil {
				t.Fatalf("decode config: %v", err)
			}
			if cfg.Width != 1016 { // 508 card units rendered at 2x
				t.Errorf("width = %d, want 1016", cfg.Width)
			}
		})
	}
}

func TestRenderCardSVG(t *testing.T) {
	out := filepath.Join(t.TempDir(), "card.svg")
	if err := renderCard(sampleModel(), "dots", "svg", out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "<svg") {
		t.Errorf("output does not look like SVG")
	}
	for _, hex := range []string{"#FF6B6B", "#FFB454", "#4EE88E"} {
		if !strings.Contains(strings.ToUpper(s), hex) {
			t.Errorf("SVG missing tier colour %s", hex)
		}
	}
}

func TestRenderCardInvalidFormat(t *testing.T) {
	out := filepath.Join(t.TempDir(), "card.gif")
	if err := renderCard(sampleModel(), "dots", "gif", out); err == nil {
		t.Fatal("expected an error for an unsupported format")
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("no file should be written on invalid format")
	}
}
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `go test ./... -run 'RenderCard' -v`
Expected: FAIL — `undefined: renderCard`.

- [ ] **Step 5: Write `card.go`**

Create `card.go`. The code below is complete; the exact vertical baseline offsets are tuned at the visual-check step (Step 7), so treat the y-positions as the starting layout.

```go
package main

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
)

const (
	cardW    = 508.0 // canvas width in units (1 unit == 1 CSS px)
	cardPad  = 28.0
	innerW   = cardW - 2*cardPad // 452
	pngScale = 2.0               // render PNG at 2x for crispness
)

// greys and accents pulled verbatim from renderSummaryCardHTML.
var (
	colBody      = mustHex("#101215")
	colPanel     = mustHex("#131519") // solid stand-in for the #16181C->#101215 gradient
	colSep       = mustHex("#212327")
	colLabel     = mustHex("#B5B8C0")
	colSub       = mustHex("#878C99")
	colTileLabel = mustHex("#878C99")
	colNoteHdr   = mustHex("#5F6570")
	colAddr      = mustHex("#D7D9DD")
	colFoot      = mustHex("#6E7480")
	colFootMain  = mustHex("#878C99")
)

// soft header tint and border per tier (rgba with the alpha from the CSS).
var tierSoft = map[string]color.RGBA{
	"danger":  {255, 107, 107, 26}, // 0.10
	"caution": {255, 180, 84, 26},
	"safe":    {78, 232, 142, 26},
}
var tierBord = map[string]color.RGBA{
	"danger":  {255, 107, 107, 89}, // 0.35
	"caution": {255, 180, 84, 82},  // 0.32
	"safe":    {78, 232, 142, 77},  // 0.30
}

func mustHex(s string) color.RGBA {
	s = strings.TrimPrefix(s, "#")
	n, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		panic("bad hex colour " + s)
	}
	return color.RGBA{uint8(n >> 16), uint8(n >> 8), uint8(n), 255}
}

type cardFonts struct{ ui, uiBold, mono *canvas.FontFamily }

func loadCardFonts() (cardFonts, error) {
	var f cardFonts
	for _, spec := range []struct {
		name string
		data []byte
		dst  **canvas.FontFamily
	}{
		{"ui", goregular.TTF, &f.ui},
		{"ui-bold", gobold.TTF, &f.uiBold},
		{"mono", gomono.TTF, &f.mono},
	} {
		fam := canvas.NewFontFamily(spec.name)
		if err := fam.LoadFont(spec.data, 0, canvas.FontRegular); err != nil {
			return f, fmt.Errorf("load font %s: %w", spec.name, err)
		}
		*spec.dst = fam
	}
	return f, nil
}

// renderCard draws the plan summary card and writes it to outPath as PNG or SVG.
// It uses tdewolff/canvas and the embedded Go fonts, so it needs no browser.
func renderCard(m summaryModel, emoji, format, outPath string) error {
	if format != "png" && format != "svg" {
		return fmt.Errorf("invalid image format %q: must be \"png\" or \"svg\"", format)
	}
	fonts, err := loadCardFonts()
	if err != nil {
		return err
	}

	shown := notableRows(m)
	h := cardHeight(len(shown))
	c := canvas.New(cardW, h)
	ctx := canvas.NewContext(c)

	// top(y) maps a top-down y (0 at the top) to canvas' bottom-left origin.
	top := func(y float64) float64 { return h - y }

	drawCard(ctx, top, m, shown, emoji, fonts)

	if format == "svg" {
		return renderers.Write(outPath, c)
	}
	return renderers.Write(outPath, c, canvas.DPMM(pngScale))
}

// notableRows returns the up-to-5 non-safe resources shown in the card.
func notableRows(m summaryModel) []resChange {
	var out []resChange
	for _, r := range m.resources {
		if r.tier == "safe" || len(out) >= 5 {
			continue
		}
		out = append(out, r)
	}
	return out
}

// cardHeight sums the fixed block heights plus one line per notable row.
func cardHeight(rows int) float64 {
	const (
		padTop     = 28.0
		header     = 96.0
		tiles      = 86.0
		bar        = 6.0
		noteHeader = 40.0
		rowH       = 25.0
		footer     = 44.0
		padBottom  = 28.0
	)
	return padTop + header + tiles + bar + noteHeader + float64(rows)*rowH + footer + padBottom
}

// drawText draws a single left-anchored line; y is the text top in top-down
// coordinates. It converts to a baseline for canvas.
func drawText(ctx *canvas.Context, top func(float64) float64, x, y float64, fam *canvas.FontFamily, size float64, col color.Color, s string) {
	face := fam.Face(size, col, canvas.FontRegular, canvas.FontNormal)
	line := canvas.NewTextLine(face, s, canvas.Left)
	// NewTextLine anchors at the top; place its top at y.
	ctx.DrawText(x, top(y), line)
}

func fillRect(ctx *canvas.Context, top func(float64) float64, x, y, w, hgt float64, col color.Color) {
	ctx.SetFillColor(col)
	ctx.DrawPath(x, top(y+hgt), canvas.Rectangle(w, hgt))
}

// drawTierIcon draws the tier indicator centred at (cx, cy-top) with the given
// diameter. setName selects the shape family; "none" draws nothing.
func drawTierIcon(ctx *canvas.Context, top func(float64) float64, cx, cy, dia float64, tier, setName string) {
	if setName == "none" {
		return
	}
	col := mustHex(tierTokens[tier].hex)
	ctx.SetFillColor(col)
	r := dia / 2
	y := top(cy)
	switch setName {
	case "signs":
		switch tier {
		case "danger":
			ctx.DrawPath(cx, y, canvas.RegularPolygon(8, r, true))
		case "caution":
			ctx.DrawPath(cx, y, canvas.RegularPolygon(3, r, true))
		default: // safe: a check mark
			ctx.SetStrokeColor(col)
			ctx.SetStrokeWidth(dia * 0.16)
			check := &canvas.Path{}
			check.MoveTo(cx-r*0.6, y)
			check.LineTo(cx-r*0.1, y-r*0.5)
			check.LineTo(cx+r*0.7, y+r*0.6)
			ctx.DrawPath(0, 0, check)
			ctx.SetStrokeColor(color.Transparent)
		}
	default: // dots: filled circle
		ctx.DrawPath(cx, y, canvas.Circle(r))
	}
}

// ellipsize truncates s (assumed monospace) to fit width w at the given font
// size, appending a single-character ellipsis when it overflows.
func ellipsize(s string, w, size float64) string {
	const advance = 0.6 // monospace advance ~0.6em; good enough for a card
	max := int(w / (size * advance))
	if max <= 1 || len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func drawCard(ctx *canvas.Context, top func(float64) float64, m summaryModel, shown []resChange, emoji string, f cardFonts) {
	x0 := cardPad
	tierHex := mustHex(tierTokens[m.verdict].hex)

	// panel background + border
	ctx.SetFillColor(colBody)
	ctx.DrawPath(0, 0, canvas.Rectangle(cardW, top(0)))
	ctx.SetFillColor(colPanel)
	ctx.SetStrokeColor(tierBord[m.verdict])
	ctx.SetStrokeWidth(1)
	ctx.DrawPath(x0, top(cardPad+cardHeightInner(len(shown))), canvas.RoundedRectangle(innerW, cardHeightInner(len(shown)), 14))
	ctx.SetStrokeColor(color.Transparent)

	y := cardPad

	// header
	fillRect(ctx, top, x0, y, innerW, 96, tierSoft[m.verdict])
	drawText(ctx, top, x0+22, y+20, f.ui, 12.5, colLabel, "Terraform Plan")
	drawTierIcon(ctx, top, x0+34, y+62, 30, m.verdict, emoji)
	drawText(ctx, top, x0+58, y+46, f.uiBold, 23, tierHex, titleCase(verdicts[m.verdict].title))
	drawText(ctx, top, x0+58, y+72, f.ui, 12.5, colSub, verdicts[m.verdict].sub)
	y += 96

	// tiles: three equal columns separated by 1px lines
	tileW := (innerW - 2) / 3
	for i, g := range m.groups {
		tx := x0 + float64(i)*(tileW+1)
		fillRect(ctx, top, tx, y, tileW, 86, colBody)
		cx := tx + tileW/2
		drawTierIcon(ctx, top, cx, y+22, 18, g.key, emoji)
		countStr := strconv.Itoa(len(g.resources))
		drawTextCentered(ctx, top, cx, y+40, f.uiBold, 27, mustHex(g.hex), countStr)
		drawTextCentered(ctx, top, cx, y+70, f.ui, 11, colTileLabel, strings.ToUpper(g.label))
	}
	// separators
	for i := 1; i < 3; i++ {
		fillRect(ctx, top, x0+float64(i)*tileW+float64(i-1), y, 1, 86, colSep)
	}
	y += 86

	// weighted colour bar
	total := 0
	for _, g := range m.groups {
		total += len(g.resources)
	}
	bx := x0
	for _, g := range m.groups {
		if total == 0 {
			break
		}
		w := innerW * float64(len(g.resources)) / float64(total)
		fillRect(ctx, top, bx, y, w, 6, mustHex(g.hex))
		bx += w
	}
	y += 6

	// notable changes
	y += 16
	drawText(ctx, top, x0+20, y, f.ui, 10.5, colNoteHdr, "NOTABLE CHANGES")
	y += 22
	for _, r := range shown {
		rc := mustHex(tierTokens[r.tier].hex)
		drawTierIcon(ctx, top, x0+28, y+7, 13, r.tier, emoji)
		drawText(ctx, top, x0+40, y, f.mono, 12, rc, r.glyph)
		addr := ellipsize(r.short, innerW-120, 12)
		drawText(ctx, top, x0+58, y, f.mono, 12, colAddr, addr)
		drawTextRight(ctx, top, x0+innerW-16, y, f.uiBold, 10.5, rc, "("+strings.ToUpper(r.action)+")")
		y += 25
	}

	// footer
	y += 12
	fillRect(ctx, top, x0, y-12, innerW, 1, colSep)
	drawText(ctx, top, x0+20, y, f.mono, 11, colFootMain, "main")
	drawText(ctx, top, x0+52, y, f.mono, 11, colFoot, "· "+strconv.Itoa(m.counts.total)+" changes")
}

// cardHeightInner is the panel height (card height minus the outer padding).
func cardHeightInner(rows int) float64 { return cardHeight(rows) - 2*cardPad }

func drawTextCentered(ctx *canvas.Context, top func(float64) float64, cx, y float64, fam *canvas.FontFamily, size float64, col color.Color, s string) {
	face := fam.Face(size, col, canvas.FontRegular, canvas.FontNormal)
	ctx.DrawText(cx, top(y), canvas.NewTextLine(face, s, canvas.Center))
}

func drawTextRight(ctx *canvas.Context, top func(float64) float64, rx, y float64, fam *canvas.FontFamily, size float64, col color.Color, s string) {
	face := fam.Face(size, col, canvas.FontRegular, canvas.FontNormal)
	ctx.DrawText(rx, top(y), canvas.NewTextLine(face, s, canvas.Right))
}
```

Note for the implementer: the Go font families load a single weight per family here (regular bytes into `FontRegular`); `uiBold` uses the `gobold` bytes, so request `canvas.FontRegular` from every face (do not ask a family for a bold it did not load). If `NewTextLine`/`DrawText` anchor text by baseline rather than top in the resolved version, adjust the `drawText*` helpers' y by the face ascent; the visual check in Step 7 is where this is confirmed.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./... -run 'RenderCard' -v`
Expected: PASS (PNG magic + width 1016 for dots/signs/none, SVG contains the three tier hexes, invalid format errors).

- [ ] **Step 7: Visual check and gofmt/vet**

Run:
```bash
gofmt -w card.go && go vet ./...
go run . summary --format image --working-dir example/random-test -o /tmp/card
go run . summary --format image --image-format svg --working-dir example/random-test -o /tmp/card
```
Wait — `runSummary` is not wired to `renderCard` yet (Task 2). Instead, add a temporary throwaway `TestZZCard` that calls `renderCard(sampleModel(), "signs", "png", "/tmp/card.png")`, run it, open `/tmp/card.png`, and confirm the header/tiles/bar/notable/footer read correctly (tune the y-offsets in `drawCard`/`drawText` if text sits high or low), then delete the throwaway test. Do not commit the throwaway.

- [ ] **Step 8: Commit**

```bash
but diff
but commit feat/browser-free-card-renderer -c -m "feat(summary): browser-free card renderer (canvas, png and svg)" --changes <card.go, card_test.go, go.mod, go.sum ids>
```

---

### Task 2: Wire the renderer into the CLI and add the example task

**Files:**
- Modify: `summary.go` (delete `renderCardPNG` + `renderSummaryCardHTML`; new `runSummary` signature + image branch; drop unused imports)
- Modify: `main.go` (`--image-format` flag on the summary subcommand, validation, thread into `runSummary`)
- Modify: `mise.toml` (new `run:summary-image-example` task)

**Interfaces:**
- Consumes: `renderCard(m summaryModel, emoji, format, outPath string) error` (Task 1).
- Produces: `runSummary(r *ponto, format, emoji, output, imageFormat string, interactive bool) (int, error)`.

- [ ] **Step 1: Delete the browser card functions in `summary.go`**

Remove the entire `renderCardPNG` function and the entire `renderSummaryCardHTML` function.

- [ ] **Step 2: Update the `runSummary` signature and image branch**

Change the signature:
```go
func runSummary(r *ponto, format, emoji, output, imageFormat string, interactive bool) (int, error) {
```
Replace the `case "image":` body with:
```go
	case "image":
		ext := "." + imageFormat
		out := output
		if !strings.HasSuffix(out, ext) {
			out += ext
		}
		if err := renderCard(m, emoji, imageFormat, out); err != nil {
			return 1, fmt.Errorf("unable to render card image: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Wrote %s\n", out)
```

- [ ] **Step 3: Drop now-unused imports in `summary.go`**

Remove `github.com/chromedp/chromedp` and `encoding/base64` from the `summary.go` import block. Leave `time` only if it is still used elsewhere in the file. Let the compiler in Step 6 confirm; remove exactly the imports it reports as unused.

- [ ] **Step 4: Add and validate `--image-format` in `main.go`**

In `newSummaryCmd()` `sumFlags`, add:
```go
	sumFlags.String("image-format", "png", "Image format for --format image: png or svg")
```
In the subcommand `RunE`, before calling `runSummary`, validate and pass it:
```go
			imageFormat := v.GetString("image-format")
			if v.GetString("format") == "image" && imageFormat != "png" && imageFormat != "svg" {
				return fmt.Errorf("invalid --image-format %q: must be \"png\" or \"svg\"", imageFormat)
			}
			code, err := runSummary(&r, v.GetString("format"), v.GetString("emoji"), v.GetString("output"), imageFormat, v.GetBool("interactive"))
```
(Replace the existing `runSummary(&r, ...)` call with the line above.)

- [ ] **Step 5: Add the mise example task**

In `mise.toml`, after the `run:summary-tui-example` task, add:
```toml
[tasks."run:summary-image-example"]
description = "Render the safety-graded plan summary card to an image (build/ponto-summary-example.png)"
# No build:ui dep and no chrome needed: the card renderer is browser-free.
run = "mkdir -p build && go run . summary --format image --working-dir example/random-test --output build/ponto-summary-example"
```

- [ ] **Step 6: Build, vet, and run the full suite**

Run: `gofmt -l . && go vet ./... && go build ./... && go test ./...`
Expected: no `gofmt -l` output, no vet errors, build succeeds, all tests pass. If `go vet`/build reports an unused import in `summary.go`, remove it and re-run.

- [ ] **Step 7: End-to-end check against the fixture**

Run:
```bash
go run . summary --format image --working-dir example/random-test -o /tmp/e2e-card 2>&1
go run . summary --format image --image-format svg --working-dir example/random-test -o /tmp/e2e-card 2>&1
ls -l /tmp/e2e-card.png /tmp/e2e-card.svg
go run . summary --format image --image-format gif --working-dir example/random-test -o /tmp/e2e-card 2>&1; echo "exit=$?"
```
Expected: `Wrote /tmp/e2e-card.png` and `Wrote /tmp/e2e-card.svg`, both files present and non-trivial; the `gif` run prints an `invalid --image-format` error and exits non-zero.

- [ ] **Step 8: Commit**

```bash
but diff
but commit feat/browser-free-card-renderer -c -m "feat(summary): render card without a browser, add --image-format and example task" --changes <summary.go, main.go, mise.toml ids>
```

---

## Notes for the implementer

- The card's visual fidelity (text baselines, exact spacing) is tuned at the Task 1 visual-check step; the tests assert structural facts (PNG magic, width, SVG tier colours), not pixels.
- Do not touch `screenshot.go` or the graph `--gen-image` path; chromedp stays for the graph.
- Keep `renderSummaryTerminal`, `renderSummaryMarkdown`, and the TUI renderers unchanged.
- `titleCase`, `verdicts`, `tierTokens`, `emojiSets`, `summaryModel`, `tierGroup`, `resChange`, `summaryCounts` are all defined in `summary.go`; reuse them, do not redefine.
