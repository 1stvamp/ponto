# Browser-free summary card renderer design

**Status:** approved
**Date:** 2026-07-10
**Type:** feature

## Goal

Render the `ponto summary --format image` card in-process with a Go vector
library instead of headless Chrome, and support both PNG and SVG output.

## Motivation

`renderCardPNG` rasterises a hand-built HTML card by driving headless chromium
(`chromedp.FullScreenshot`). The card is a fixed layout we generate ourselves,
so a browser is not needed to lay it out. Drawing it directly with a Go vector
library means `summary --format image` runs anywhere, with no chrome/chromium on
PATH, which matters in CI. SVG output then comes almost for free.

This does not remove `chromedp` from the binary: the graph image (`--gen-image`,
`screenshot.go`) still needs it, because its layout (cytoscape + klay) only
exists in the browser. The win is a browser-free *runtime* for the card path.

## Decisions

- Tier indicators are drawn as vector shapes coloured by tier (no emoji font):
  `dots` -> filled circle; `signs` -> danger octagon, caution triangle, safe
  check; `none` -> no icon.
- Fonts are the Go fonts from `golang.org/x/image/font/gofont`
  (`goregular`, `gobold`, `gomono`), loaded from their byte slices. No font
  asset files are shipped.
- Output supports PNG and SVG, chosen with a new `--image-format svg|png` flag
  on the `summary` subcommand (default `png`), mirroring `--gen-image`.

## CLI

```
ponto summary --format image                          # writes ponto-summary.png
ponto summary --format image --image-format svg       # writes ponto-summary.svg
ponto summary --format image -o card                  # writes card.png
```

## New dependency

- `github.com/tdewolff/canvas` (+ its transitive image deps) for vector drawing
  and PNG/SVG rendering.
- `golang.org/x/image/font/gofont/{goregular,gobold,gomono}` for embedded fonts.

## Components

### New file: `card.go`

Top-level entry, replacing `renderCardPNG`:

```go
// renderCard draws the plan summary card and writes it to outPath as PNG or
// SVG (format is "png" or "svg"). It uses tdewolff/canvas and the embedded Go
// fonts, so it needs no browser at runtime.
func renderCard(m summaryModel, emoji, format, outPath string) error
```

Behaviour:

- Validate `format`: return an error for anything other than `"png"` or `"svg"`.
- Build one `canvas.Canvas` sized to the card (508 units wide; height computed
  from content, see Layout). Draw with a `canvas.Context`.
- Render: PNG at 2x for crispness (`renderers.Write(outPath, c, canvas.DPMM(2.0))`),
  SVG at 1x (`renderers.Write(outPath, c)`). `renderers.Write` selects the
  encoder from the file extension, and the caller guarantees the extension.
- Return any font-load, draw, or write error (no `log.Fatal`).

Helpers in `card.go`:

- Font setup, done once per render:
  ```go
  func loadCardFonts() (ui, uiBold, mono *canvas.FontFamily, err error)
  ```
  Each family is created with `canvas.NewFontFamily(name)` and populated from a
  Go font byte slice via the family's load-from-bytes method (e.g.
  `LoadFont(goregular.TTF, 0, canvas.FontRegular)`); confirm the exact method
  name against the pinned canvas version during implementation.
- Colour parsing: `func hexColor(s string) color.RGBA` for the `#RRGGBB` tokens,
  and `func rgba(r, g, b uint8, a float64) color.RGBA` for the soft/border tints.
- Top-down coordinate mapping: canvas' origin is bottom-left with Y up, but the
  card is laid out top-down. Provide `func top(cardH, y float64) float64 { return cardH - y }`
  (or an equivalent transform) and route every draw through it so the layout
  code reads top-down.
- Tier icon:
  ```go
  // drawTierIcon draws the tier indicator at (x, y) with the given diameter in
  // the tier colour. set is the resolved emojiSet; "" glyphs (the "none" set)
  // draw nothing.
  func drawTierIcon(ctx *canvas.Context, x, y, size float64, tier, setName string, col color.RGBA)
  ```
  - `dots`: filled circle of diameter `size`.
  - `signs`: `danger` filled regular octagon; `caution` filled upward triangle;
    `safe` a check mark drawn as a stroked path. All in `col`.
  - `none`: draw nothing.

### Layout (verbatim values from `renderSummaryCardHTML`)

Card is 508 wide with 28 padding, so the inner card is 452 wide. Blocks, in
order, with the exact colours and font sizes from the current HTML:

- Base fills: page/card body `#101215`; card panel background approximated as a
  solid `#131519` (midpoint of the `#16181C -> #101215` gradient; use a canvas
  linear gradient only if trivial in the pinned version). Card border 1px in the
  tier `bord` tint, corner radius 14.
  - `bord`: danger `rgba(255,107,107,0.35)`, caution `rgba(255,180,84,0.32)`,
    safe `rgba(78,232,142,0.30)`.
- Header: padding 20x22; background the tier `soft` tint
  (danger `rgba(255,107,107,0.10)`, caution `rgba(255,180,84,0.10)`,
  safe `rgba(78,232,142,0.10)`); 1px bottom border in `bord`.
  - Label `Terraform Plan`: `#B5B8C0`, 12.5px, weight 600.
  - Verdict icon via `drawTierIcon` (verdict tier), roughly 34px box; to its
    right the verdict title in the tier hex at 23px weight 700
    (`titleCase(verdicts[verdict].title)`), and below it the subtitle
    `verdicts[verdict].sub` in `#878C99` at 12.5px.
- Tiles: one per group in `m.groups`, laid out as three equal columns separated
  by 1px lines in `#212327`, each tile background `#101215`, padding 15x14,
  centred: the tier icon (~18px), the count `len(g.resources)` in the group hex
  at 27px weight 700, and the label `g.label` in `#878C99` at 11px weight 600,
  uppercase.
- Bar: height 6; one segment per group with width proportional to
  `len(g.resources)` (flex weight), filled with the group hex. Groups with zero
  resources take zero width.
- Notable changes: section padding 16/20/12; header `Notable changes` in
  `#5F6570` at 10.5px weight 600 uppercase. Then up to 5 rows drawn from
  `m.resources`, skipping `tier == "safe"`, each row padding 5 top/bottom,
  element gap 9: tier icon (~13px, width 16), glyph `r.glyph` in mono at 12px
  weight 700 in the tier hex (width 12), the address `r.short` in mono 12px
  `#D7D9DD` truncated with an ellipsis to the available width, and the action
  `(r.action)` in the tier hex at 10.5px weight 600 uppercase, right-aligned.
- Footer: 1px top border `#212327`, padding 12/20/15, mono 11px: `main` in
  `#878C99`, a middle dot, and `%d changes` with `m.counts.total`, in `#6E7480`.

Height is the sum of the block heights for the number of rows actually shown;
compute it before creating the canvas.

For the `none` emoji set, icon draws are skipped; leaving the icon column blank
is acceptable (minor horizontal gap), do not re-flow the layout for it.

### `summary.go` changes

- Delete `renderCardPNG` and `renderSummaryCardHTML` (the latter's only caller
  is `renderCardPNG`).
- Remove the now-unused imports from `summary.go`: `github.com/chromedp/chromedp`,
  `encoding/base64`, and `time` if nothing else in the file uses it (verify with
  the compiler; `screenshot.go` keeps its own chromedp import).
- Change `runSummary` to accept the image format and call `renderCard`:
  ```go
  func runSummary(r *ponto, format, emoji, output, imageFormat string, interactive bool) (int, error)
  ```
  In the `case "image":` branch:
  ```go
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

### `main.go` changes

In `newSummaryCmd()`:

- Add to `sumFlags`:
  ```go
  sumFlags.String("image-format", "png", "Image format for --format image: png or svg")
  ```
- In the `RunE`, validate and pass it:
  ```go
  imageFormat := v.GetString("image-format")
  if v.GetString("format") == "image" && imageFormat != "png" && imageFormat != "svg" {
      return fmt.Errorf("invalid --image-format %q: must be \"png\" or \"svg\"", imageFormat)
  }
  code, err := runSummary(&r, v.GetString("format"), v.GetString("emoji"), v.GetString("output"), imageFormat, v.GetBool("interactive"))
  ```
- The subcommand's `SetUsageFunc` already prints `sumFlags.FlagUsages()`, so the
  new flag shows up with no further change.

### `mise.toml`

Add an example task exercising the card image. Unlike `run:image-example`, it
needs neither `build:ui` nor a chrome binary, which is the point of this change:

```toml
[tasks."run:summary-image-example"]
description = "Render the safety-graded plan summary card to an image (build/ponto-summary-example.png)"
# No build:ui dep and no chrome needed: the card renderer is browser-free.
run = "mkdir -p build && go run . summary --format image --working-dir example/random-test --output build/ponto-summary-example"
```

## Error handling

- `renderCard` returns errors for an invalid format, font-load failure, or write
  failure; `runSummary` wraps them as `unable to render card image`.
- `--image-format` is validated in `newSummaryCmd` before work starts, only when
  `--format image` is in effect.
- The `--format image` danger exit code (2) is unchanged.

## Testing

New `card_test.go`:

- `TestRenderCardPNG`: for a small hand-built `summaryModel` with all three
  tiers, `renderCard(m, "dots", "png", tmp)` writes a file whose first bytes are
  the PNG signature (`\x89PNG\r\n\x1a\n`) and whose `image.DecodeConfig` width is
  the expected 2x card width; size is non-trivial (> 1KB). Repeat for
  `emoji="signs"` and `emoji="none"` (no error, valid PNG).
- `TestRenderCardSVG`: `renderCard(m, "dots", "svg", tmp)` writes a file starting
  with `<svg` (allowing an XML prolog) that contains the tier hex colours
  `#FF6B6B`, `#FFB454`, and `#4EE88E`.
- `TestRenderCardInvalidFormat`: `renderCard(m, "dots", "gif", tmp)` returns a
  non-nil error and writes no file.
- Existing `summary_test.go`, `main_test.go`, and the other suites keep passing;
  update any that referenced the removed `renderCardPNG`/`renderSummaryCardHTML`
  or the old `runSummary` signature.

Visual check during implementation: render the bundled example
(`go run . summary --format image --working-dir example/random-test -o /tmp/card`)
and inspect `/tmp/card.png` for fidelity against the current browser card.

## Out of scope

- No change to the terminal, markdown, or TUI summary renderers.
- No change to the graph image path (`--gen-image` keeps using chromedp).
- No attempt to render colour emoji; tier indicators are vector shapes.
