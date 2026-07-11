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
// coordinates. NewTextLine anchors on the baseline, so the line is shifted
// down by the face's ascent to place its cap-height top at y.
func drawText(ctx *canvas.Context, top func(float64) float64, x, y float64, fam *canvas.FontFamily, size float64, col color.Color, s string) {
	face := fam.Face(size, col, canvas.FontRegular, canvas.FontNormal)
	line := canvas.NewTextLine(face, s, canvas.Left)
	ctx.DrawText(x, top(y)-face.Metrics().Ascent, line)
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
	ctx.DrawText(cx, top(y)-face.Metrics().Ascent, canvas.NewTextLine(face, s, canvas.Center))
}

func drawTextRight(ctx *canvas.Context, top func(float64) float64, rx, y float64, fam *canvas.FontFamily, size float64, col color.Color, s string) {
	face := fam.Face(size, col, canvas.FontRegular, canvas.FontNormal)
	ctx.DrawText(rx, top(y)-face.Metrics().Ascent, canvas.NewTextLine(face, s, canvas.Right))
}
