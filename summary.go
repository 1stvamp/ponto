package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/chromedp/chromedp"
	tfjson "github.com/hashicorp/terraform-json"
)

// A safety-graded plan digest: one classification pass maps each change to a
// Safe / Caution / Danger tier, then the same model renders to a terminal
// summary, a markdown PR comment, or a shareable image card. Destructive
// changes are triple-encoded (colour + emoji + glyph + text) so they survive
// colour-blindness, greyscale and emoji-less terminals.

type tierToken struct {
	key   string
	label string
	hex   string
	ansi  string
	badge string
}

var tierTokens = map[string]tierToken{
	"danger":  {"danger", "Danger", "#FF6B6B", "196", "red"},
	"caution": {"caution", "Caution", "#FFB454", "214", "orange"},
	"safe":    {"safe", "Safe", "#4EE88E", "42", "brightgreen"},
}

// tier render order: worst first, so the scary changes are seen before scrolling.
var tierOrder = []string{"danger", "caution", "safe"}

type emojiSet struct{ danger, caution, safe, vDanger, vCaution, vSafe string }

var emojiSets = map[string]emojiSet{
	"dots":  {"🔴", "🟡", "🟢", "🔴", "🟡", "🟢"},
	"signs": {"🛑", "⚠️", "✅", "⛔", "⚠️", "✅"},
	"none":  {"", "", "", "", "", ""},
}

type verdictCopy struct{ title, sub string }

var verdicts = map[string]verdictCopy{
	"danger":  {"REVIEW REQUIRED", "This plan destroys or replaces live resources."},
	"caution": {"APPLY WITH CARE", "In-place changes to existing resources."},
	"safe":    {"SAFE TO APPLY", "Only additive changes. Nothing is destroyed."},
}

type diffLine struct {
	sign string // "+", "-", "~"
	text string
}

type resChange struct {
	action       string
	glyph        string
	tier         string
	address      string
	modulePrefix string
	short        string
	reason       string
	diff         []diffLine
}

type summaryCounts struct {
	add, change, destroy, replace, total int
}

type tierGroup struct {
	tierToken
	resources []resChange
}

type summaryModel struct {
	resources []resChange
	tierCount map[string]int
	counts    summaryCounts
	verdict   string
	groups    []tierGroup
}

// classifyAction maps a change's action array to an (action, glyph, tier). It
// returns ok=false for no-op/unknown changes, which are omitted.
func classifyAction(a tfjson.Actions) (action, glyph, tier string, ok bool) {
	switch {
	case a.Replace():
		return "replace", "±", "danger", true
	case a.Delete():
		return "destroy", "−", "danger", true
	case a.Update():
		return "update", "~", "caution", true
	case a.Create():
		return "create", "+", "safe", true
	case a.Read():
		return "read", "⊂", "safe", true
	default:
		return "", "", "", false
	}
}

func fmtVal(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return fmt.Sprintf("%q", t)
	case []interface{}:
		if len(t) > 3 {
			return fmt.Sprintf("[…%d items]", len(t))
		}
		b, _ := json.Marshal(t)
		return string(b)
	case map[string]interface{}:
		return "{ … }"
	default:
		return fmt.Sprintf("%v", t)
	}
}

// replacePath renders the first replace_paths entry as a dotted attribute path.
func replacePath(paths []interface{}) string {
	if len(paths) == 0 {
		return ""
	}
	p, ok := paths[0].([]interface{})
	if !ok {
		return ""
	}
	parts := make([]string, 0, len(p))
	for _, seg := range p {
		parts = append(parts, fmt.Sprintf("%v", seg))
	}
	return strings.Join(parts, ".")
}

func asMap(v interface{}) map[string]interface{} {
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	return map[string]interface{}{}
}

// buildDiff walks before/after/after_unknown to produce per-attribute diff
// lines, honouring sensitive/unknown values and replace_paths markers.
func buildDiff(ch *tfjson.Change) []diffLine {
	before := asMap(ch.Before)
	after := asMap(ch.After)
	unknown := asMap(ch.AfterUnknown)
	sensitive := asMap(ch.AfterSensitive)

	forces := map[string]bool{}
	for _, p := range ch.ReplacePaths {
		if path, ok := p.([]interface{}); ok && len(path) > 0 {
			if k, ok := path[0].(string); ok {
				forces[k] = true
			}
		}
	}

	keySet := map[string]bool{}
	for k := range before {
		keySet[k] = true
	}
	for k := range after {
		keySet[k] = true
	}
	for k := range unknown {
		keySet[k] = true
	}
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var lines []diffLine
	for _, k := range keys {
		_, inB := before[k]
		_, inAfterMap := after[k]
		isUnknown := unknown[k] == true
		inA := inAfterMap || isUnknown

		var val string
		switch {
		case sensitive[k] == true:
			val = "(sensitive value)"
		case isUnknown:
			val = "(known after apply)"
		default:
			val = fmtVal(after[k])
		}
		suffix := ""
		if forces[k] {
			suffix = "  # forces replacement"
		}

		// collapse big block lists (e.g. many rule{} blocks) to keep it readable
		if arr, ok := after[k].([]interface{}); ok && len(arr) > 3 {
			lines = append(lines, diffLine{"+", fmt.Sprintf("%s { … } × %d", k, len(arr))})
			continue
		}

		switch {
		case inA && !inB:
			lines = append(lines, diffLine{"+", fmt.Sprintf("%s = %s%s", k, val, suffix)})
		case inB && !inA:
			lines = append(lines, diffLine{"-", fmt.Sprintf("%s = %s", k, fmtVal(before[k]))})
		default:
			bj, _ := json.Marshal(before[k])
			aj, _ := json.Marshal(after[k])
			if string(bj) != string(aj) {
				lines = append(lines, diffLine{"~", fmt.Sprintf("%s = %s → %s%s", k, fmtVal(before[k]), val, suffix)})
			}
		}
	}
	return lines
}

// classifyPlan is the shared model consumed by all three renderers.
func classifyPlan(plan *tfjson.Plan) summaryModel {
	var resources []resChange
	if plan != nil {
		for _, rc := range plan.ResourceChanges {
			if rc.Change == nil {
				continue
			}
			action, glyph, tier, ok := classifyAction(rc.Change.Actions)
			if !ok {
				continue
			}
			modulePrefix := ""
			if rc.ModuleAddress != "" {
				modulePrefix = rc.ModuleAddress + "."
			}
			short := strings.TrimPrefix(rc.Address, modulePrefix)
			reason := ""
			switch action {
			case "replace":
				if p := replacePath(rc.Change.ReplacePaths); p != "" {
					reason = "forces replacement — " + p + " change"
				} else {
					reason = "forces replacement — destroy then recreate"
				}
			case "destroy":
				reason = "resource permanently removed"
			}
			resources = append(resources, resChange{
				action:       action,
				glyph:        glyph,
				tier:         tier,
				address:      rc.Address,
				modulePrefix: modulePrefix,
				short:        short,
				reason:       reason,
				diff:         buildDiff(rc.Change),
			})
		}
	}

	tierCount := map[string]int{"danger": 0, "caution": 0, "safe": 0}
	byAction := map[string]int{}
	for _, r := range resources {
		tierCount[r.tier]++
		byAction[r.action]++
	}
	counts := summaryCounts{
		add:     byAction["create"] + byAction["replace"],
		change:  byAction["update"],
		destroy: byAction["destroy"] + byAction["replace"],
		replace: byAction["replace"],
		total:   len(resources),
	}
	verdict := "safe"
	if tierCount["danger"] > 0 {
		verdict = "danger"
	} else if tierCount["caution"] > 0 {
		verdict = "caution"
	}

	var groups []tierGroup
	for _, key := range tierOrder {
		var rs []resChange
		for _, r := range resources {
			if r.tier == key {
				rs = append(rs, r)
			}
		}
		if len(rs) == 0 {
			continue
		}
		// within a tier, order by module path then address
		sort.SliceStable(rs, func(i, j int) bool {
			if rs[i].modulePrefix != rs[j].modulePrefix {
				return rs[i].modulePrefix < rs[j].modulePrefix
			}
			return rs[i].address < rs[j].address
		})
		groups = append(groups, tierGroup{tierToken: tierTokens[key], resources: rs})
	}

	return summaryModel{
		resources: resources,
		tierCount: tierCount,
		counts:    counts,
		verdict:   verdict,
		groups:    groups,
	}
}

func verdictEmoji(e emojiSet, verdict string) string {
	switch verdict {
	case "danger":
		return e.vDanger
	case "caution":
		return e.vCaution
	default:
		return e.vSafe
	}
}

func tierEmoji(e emojiSet, tier string) string {
	switch tier {
	case "danger":
		return e.danger
	case "caution":
		return e.caution
	default:
		return e.safe
	}
}

// ---------- terminal renderer ----------

var signColor = map[string]string{"+": "42", "-": "196", "~": "214", " ": "244"}

func renderSummaryTerminal(m summaryModel, emoji string) string {
	e := emojiSets[emoji]
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	t := tierTokens[m.verdict]
	v := verdicts[m.verdict]
	tierStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(t.ansi))

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", dim.Render("$ ponto plan --summary"))

	// verdict banner: a bordered box coloured by the verdict tier. Keep the
	// emoji outside lipgloss's width math (emoji are double-width and break
	// box alignment); the box wraps the text-only title, emoji sits before it.
	vem := verdictEmoji(e, m.verdict)
	title := v.title
	if vem != "" {
		title = vem + "  " + v.title
	}
	banner := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(t.ansi)).
		Foreground(lipgloss.Color(t.ansi)).
		Bold(true).
		Padding(0, 1).
		Render(title)
	fmt.Fprintf(&b, "%s\n", banner)
	fmt.Fprintf(&b, "%s\n\n", dim.Render("  "+v.sub))

	c := m.counts
	col := func(ansi, s string) string { return lipgloss.NewStyle().Foreground(lipgloss.Color(ansi)).Render(s) }
	fmt.Fprintf(&b, "%s%s%s%s%s%s%s%s%s\n\n",
		dim.Render("Plan: "),
		col("42", fmt.Sprintf("%d to add", c.add)),
		dim.Render(", "),
		col("214", fmt.Sprintf("%d to change", c.change)),
		dim.Render(", "),
		col("196", fmt.Sprintf("%d to destroy", c.destroy)),
		dim.Render("  ·  "),
		col("196", fmt.Sprintf("%d replace", c.replace)),
		"",
	)

	for _, g := range m.groups {
		gem := tierEmoji(e, g.key)
		gStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(g.ansi))
		prefix := ""
		if gem != "" {
			prefix = gem + " "
		}
		fmt.Fprintf(&b, "%s%s %s\n", prefix, gStyle.Render(g.label), dim.Render(fmt.Sprintf("(%d)", len(g.resources))))
		for _, r := range g.resources {
			rem := tierEmoji(e, r.tier)
			rp := ""
			if rem != "" {
				rp = rem + " "
			}
			fmt.Fprintf(&b, "  %s%s %s%s\n", rp, gStyle.Render(r.glyph), dim.Render(r.modulePrefix), r.short)
			if r.reason != "" {
				fmt.Fprintf(&b, "      %s\n", dim.Render("↳ "+r.reason))
			}
			for _, d := range r.diff {
				c := signColor[d.sign]
				if c == "" {
					c = "244"
				}
				fmt.Fprintf(&b, "        %s\n", lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render(d.sign+" "+d.text))
			}
		}
		b.WriteString("\n")
	}
	_ = tierStyle
	return b.String()
}

// ---------- markdown renderer ----------

func renderSummaryMarkdown(m summaryModel, emoji string) string {
	e := emojiSets[emoji]
	v := verdicts[m.verdict]
	title := titleCase(v.title)
	var b strings.Builder
	fmt.Fprintf(&b, "## %s Terraform Plan — %s\n\n", verdictEmoji(e, m.verdict), title)
	fmt.Fprintf(&b, "> %s\n\n", v.sub)

	badges := make([]string, 0, len(m.groups))
	for _, g := range m.groups {
		badges = append(badges, fmt.Sprintf("![%s](https://img.shields.io/badge/%s-%d-%s)", g.label, g.label, len(g.resources), g.badge))
	}
	b.WriteString(strings.Join(badges, " ") + "\n\n")

	b.WriteString("| Tier | Count | Actions |\n|------|:-----:|---------|\n")
	acts := map[string]string{"danger": "replace · destroy", "caution": "update in-place", "safe": "create"}
	for _, g := range m.groups {
		fmt.Fprintf(&b, "| %s **%s** | %d | %s |\n", tierEmoji(e, g.key), g.label, len(g.resources), acts[g.key])
	}
	c := m.counts
	fmt.Fprintf(&b, "\n`Plan: %d to add, %d to change, %d to destroy`\n\n", c.add, c.change, c.destroy)

	for _, g := range m.groups {
		open := ""
		if g.key != "safe" {
			open = " open"
		}
		plural := "s"
		if len(g.resources) == 1 {
			plural = ""
		}
		fmt.Fprintf(&b, "<details%s>\n<summary>%s <b>%s</b> — %d resource%s</summary>\n\n```diff\n", open, tierEmoji(e, g.key), g.label, len(g.resources), plural)
		for _, r := range g.resources {
			lead := "!"
			if r.action == "destroy" {
				lead = "-"
			} else if r.action == "create" {
				lead = "+"
			}
			fmt.Fprintf(&b, "%s %s%s   (%s)\n", lead, r.modulePrefix, r.short, r.action)
			for _, d := range r.diff {
				fmt.Fprintf(&b, "%s %s\n", d.sign, d.text)
			}
		}
		b.WriteString("```\n\n</details>\n\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func titleCase(s string) string {
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// ---------- image card renderer ----------

func htmlEsc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

func renderSummaryCardHTML(m summaryModel, emoji string) string {
	e := emojiSets[emoji]
	t := tierTokens[m.verdict]
	v := verdicts[m.verdict]
	vem := verdictEmoji(e, m.verdict)
	soft := map[string]string{"danger": "rgba(255,107,107,0.10)", "caution": "rgba(255,180,84,0.10)", "safe": "rgba(78,232,142,0.10)"}[m.verdict]
	bord := map[string]string{"danger": "rgba(255,107,107,0.35)", "caution": "rgba(255,180,84,0.32)", "safe": "rgba(78,232,142,0.30)"}[m.verdict]
	title := titleCase(v.title)

	var tiles, bar strings.Builder
	for _, g := range m.groups {
		fmt.Fprintf(&tiles, `<div style="background:#101215;padding:15px 14px;text-align:center;"><div style="font-size:18px;margin-bottom:4px;">%s</div><div style="font-size:27px;font-weight:700;color:%s;line-height:1;">%d</div><div style="font-size:11px;font-weight:600;color:#878C99;text-transform:uppercase;letter-spacing:.05em;margin-top:5px;">%s</div></div>`,
			tierEmoji(e, g.key), g.hex, len(g.resources), g.label)
		fmt.Fprintf(&bar, `<div style="flex:%d;background:%s;"></div>`, len(g.resources), g.hex)
	}

	var notable strings.Builder
	shown := 0
	for _, r := range m.resources {
		if r.tier == "safe" || shown >= 5 {
			continue
		}
		shown++
		fmt.Fprintf(&notable, `<div style="display:flex;align-items:center;gap:9px;padding:5px 0;"><span style="font-size:13px;width:16px;text-align:center;">%s</span><span style="font-family:monospace;font-weight:700;font-size:12px;width:12px;text-align:center;color:%s;">%s</span><span style="font-family:monospace;font-size:12px;color:#D7D9DD;flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;">%s</span><span style="font-size:10.5px;font-weight:600;color:%s;text-transform:uppercase;">(%s)</span></div>`,
			tierEmoji(e, r.tier), tierTokens[r.tier].hex, r.glyph, htmlEsc(r.short), tierTokens[r.tier].hex, r.action)
	}

	return fmt.Sprintf(`<!DOCTYPE html><html><head><meta charset="utf-8"><style>*{box-sizing:border-box}html,body{margin:0;width:508px;background:#101215}body{font-family:Geist,system-ui,sans-serif;padding:28px}</style></head><body>
<div style="width:452px;background:linear-gradient(168deg,#16181C,#101215 60%%);border:1px solid %s;border-radius:14px;overflow:hidden;">
<div style="background:%s;border-bottom:1px solid %s;padding:20px 22px;">
<div style="display:flex;align-items:center;gap:8px;margin-bottom:11px;"><span style="font-size:12.5px;font-weight:600;color:#B5B8C0;">Terraform Plan</span></div>
<div style="display:flex;align-items:center;gap:12px;"><span style="font-size:34px;">%s</span><div><div style="font-size:23px;font-weight:700;color:%s;line-height:1.1;">%s</div><div style="font-size:12.5px;color:#878C99;margin-top:3px;">%s</div></div></div>
</div>
<div style="display:grid;grid-template-columns:1fr 1fr 1fr;gap:1px;background:#212327;">%s</div>
<div style="display:flex;height:6px;">%s</div>
<div style="padding:16px 20px 12px;"><div style="font-size:10.5px;font-weight:600;letter-spacing:.06em;text-transform:uppercase;color:#5F6570;margin-bottom:10px;">Notable changes</div>%s</div>
<div style="display:flex;align-items:center;gap:10px;padding:12px 20px 15px;border-top:1px solid #212327;font-family:monospace;font-size:11px;color:#6E7480;"><span style="color:#878C99;">main</span><span>·</span><span>%d changes</span></div>
</div></body></html>`,
		t.hex, soft, bord, vem, t.hex, title, v.sub, tiles.String(), bar.String(), notable.String(), m.counts.total)
}

// renderCardPNG rasterises the card HTML to a PNG using headless chromium.
func renderCardPNG(html, outPath string) error {
	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()
	ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	dataURL := "data:text/html;charset=utf-8;base64," + base64.StdEncoding.EncodeToString([]byte(html))
	var buf []byte
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(508, 560),
		chromedp.Navigate(dataURL),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.FullScreenshot(&buf, 100),
	); err != nil {
		return err
	}
	return os.WriteFile(outPath, buf, 0o644)
}

// runSummary classifies the plan and renders it in the requested format.
// It returns the process exit code: 2 when the plan is destructive (Danger),
// so it can be dropped into CI as a gate.
func runSummary(r *ponto, format, emoji, output string, interactive bool) (int, error) {
	if _, ok := emojiSets[emoji]; !ok {
		return 1, fmt.Errorf("invalid --emoji %q: must be dots, signs or none", emoji)
	}
	// --format tui is an alias for the interactive terminal view.
	if format == "tui" {
		format = "terminal"
		interactive = true
	}
	if interactive {
		// The alt-screen TUI can't share the screen with plan logs.
		log.SetOutput(io.Discard)
	}
	if err := r.getPlan(); err != nil {
		return 1, fmt.Errorf("unable to load plan: %w", err)
	}
	m := classifyPlan(r.Plan)

	switch format {
	case "terminal":
		if interactive {
			if err := runSummaryTUI(m, emoji); err != nil {
				return 1, err
			}
			break
		}
		fmt.Print(renderSummaryTerminal(m, emoji))
	case "markdown":
		fmt.Print(renderSummaryMarkdown(m, emoji))
	case "image":
		out := output
		if !strings.HasSuffix(out, ".png") {
			out += ".png"
		}
		if err := renderCardPNG(renderSummaryCardHTML(m, emoji), out); err != nil {
			return 1, fmt.Errorf("unable to render card image: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Wrote %s\n", out)
	default:
		return 1, fmt.Errorf("invalid --format %q: must be terminal, markdown or image", format)
	}

	if m.verdict == "danger" {
		return 2, nil
	}
	return 0, nil
}
