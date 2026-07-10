package main

import (
	"fmt"
	"io"
	"log"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/harmonica"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
)

// frameMsg drives the animation loop (spring-eased scroll + entrance bar).
type frameMsg struct{}

func animTick() tea.Cmd {
	return tea.Tick(time.Second/60, func(time.Time) tea.Msg { return frameMsg{} })
}

// The interactive plan summary: the same safety-graded model as the static
// terminal renderer, but navigable — move between resources and expand any of
// them (keyboard or mouse click) to reveal its attribute diff, like clicking a
// row in the web UI.

type summaryTUIKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Toggle key.Binding
	All    key.Binding
	Help   key.Binding
	Quit   key.Binding
}

func (k summaryTUIKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Toggle, k.All, k.Help, k.Quit}
}

func (k summaryTUIKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Up, k.Down}, {k.Toggle, k.All}, {k.Help, k.Quit}}
}

var summaryKeys = summaryTUIKeyMap{
	Up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/↓", "move")),
	Down:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Toggle: key.NewBinding(key.WithKeys("enter", " ", "tab"), key.WithHelp("enter", "expand/collapse")),
	All:    key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "expand/collapse all")),
	Help:   key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
}

type summaryTUIModel struct {
	model   summaryModel
	emoji   string
	flatRes []resChange // resources in display (group) order

	expanded map[string]bool
	cursor   int // index into flatRes

	vp      viewport.Model
	help    help.Model
	ready   bool
	focused bool
	width   int
	height  int
	hover   int // flatRes index under the mouse, or -1

	headerHeight int
	resStartLine []int // flatRes index -> first content line

	// animation state
	scroll     harmonica.Spring
	scrollPos  float64
	scrollVel  float64
	scrollGoal float64
	bar        harmonica.Spring
	barPos     float64 // entrance bar fill 0..1
	barVel     float64
	animating  bool
}

func newSummaryTUIModel(m summaryModel, emoji string) summaryTUIModel {
	var flat []resChange
	for _, g := range m.groups {
		flat = append(flat, g.resources...)
	}
	return summaryTUIModel{
		model:        m,
		emoji:        emoji,
		flatRes:      flat,
		expanded:     map[string]bool{},
		vp:           viewport.New(0, 0),
		help:         help.New(),
		resStartLine: make([]int, len(flat)),
		hover:        -1,
		focused:      true,
		// underdamped spring for scroll, snappier spring for the entrance bar
		scroll: harmonica.NewSpring(harmonica.FPS(60), 8.0, 1.0),
		bar:    harmonica.NewSpring(harmonica.FPS(60), 5.0, 1.0),
	}
}

func (m summaryTUIModel) Init() tea.Cmd { return animTick() }

func (m summaryTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.Width = msg.Width
		m.ready = true
		m.layout()
		m.rebuild()
		m.ensureVisible()
		return m, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, summaryKeys.Quit):
			return m, tea.Quit
		case key.Matches(msg, summaryKeys.Up):
			m.moveCursor(-1)
			return m, m.startAnim()
		case key.Matches(msg, summaryKeys.Down):
			m.moveCursor(1)
			return m, m.startAnim()
		case key.Matches(msg, summaryKeys.Toggle):
			m.toggleCurrent()
			return m, m.startAnim()
		case key.Matches(msg, summaryKeys.All):
			m.toggleAll()
			return m, m.startAnim()
		case key.Matches(msg, summaryKeys.Help):
			m.help.ShowAll = !m.help.ShowAll
			m.layout()
			m.rebuild()
			return m, nil
		}

	case tea.FocusMsg:
		m.focused = true
		return m, nil
	case tea.BlurMsg:
		m.focused = false
		return m, nil

	case frameMsg:
		return m, m.animate()

	case tea.MouseMsg:
		if !m.ready {
			return m, nil
		}
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.setScrollTarget(m.scrollTarget() - 3)
			return m, m.startAnim()
		case tea.MouseButtonWheelDown:
			m.setScrollTarget(m.scrollTarget() + 3)
			return m, m.startAnim()
		case tea.MouseButtonLeft:
			if msg.Action == tea.MouseActionPress {
				in := func(id string) bool {
					z := zone.Get(id)
					return z != nil && z.InBounds(msg)
				}
				switch {
				case in("foot-q"):
					return m, tea.Quit
				case in("foot-?"):
					m.help.ShowAll = !m.help.ShowAll
					m.layout()
					m.rebuild()
					return m, nil
				case in("foot-e"):
					m.toggleAll()
					return m, m.startAnim()
				case in("foot-enter"):
					m.toggleCurrent()
					return m, m.startAnim()
				}
				if sel := m.zoneHit(msg); sel >= 0 {
					m.cursor = sel
					addr := m.flatRes[sel].address
					m.expanded[addr] = !m.expanded[addr]
					m.rebuild()
					m.ensureVisible()
					return m, m.startAnim()
				}
			}
			return m, nil
		case tea.MouseButtonNone:
			// motion → hover highlight
			if h := m.zoneHit(msg); h != m.hover {
				m.hover = h
				m.rebuild()
			}
			return m, nil
		}
	}
	return m, nil
}

// zoneHit returns the flatRes index whose row is under the mouse, or -1.
func (m *summaryTUIModel) zoneHit(msg tea.MouseMsg) int {
	for i := range m.flatRes {
		if z := zone.Get(m.zoneID(i)); z != nil && z.InBounds(msg) {
			return i
		}
	}
	return -1
}

func (m *summaryTUIModel) zoneID(i int) string { return fmt.Sprintf("sumres-%d", i) }

// scroll target is tracked as the viewport YOffset the spring settles toward.
func (m *summaryTUIModel) scrollTarget() float64 { return m.scrollPos }
func (m *summaryTUIModel) setScrollTarget(target float64) {
	max := float64(m.vp.TotalLineCount() - m.vp.Height)
	if max < 0 {
		max = 0
	}
	if target < 0 {
		target = 0
	}
	if target > max {
		target = max
	}
	m.scrollGoal = target
}

func (m *summaryTUIModel) startAnim() tea.Cmd {
	if m.animating {
		return nil
	}
	m.animating = true
	return animTick()
}

// animate advances the springs one frame and re-ticks until everything settles.
func (m *summaryTUIModel) animate() tea.Cmd {
	m.barPos, m.barVel = m.bar.Update(m.barPos, m.barVel, 1.0)
	m.scrollPos, m.scrollVel = m.scroll.Update(m.scrollPos, m.scrollVel, m.scrollGoal)
	m.vp.SetYOffset(int(math.Round(m.scrollPos)))

	barDone := math.Abs(m.barPos-1.0) < 0.001 && math.Abs(m.barVel) < 0.001
	scrollDone := math.Abs(m.scrollPos-m.scrollGoal) < 0.4 && math.Abs(m.scrollVel) < 0.4
	if barDone && scrollDone {
		m.animating = false
		m.barPos, m.barVel = 1.0, 0
		m.scrollPos = m.scrollGoal
		m.vp.SetYOffset(int(math.Round(m.scrollPos)))
		return nil
	}
	return animTick()
}

func (m *summaryTUIModel) moveCursor(delta int) {
	if len(m.flatRes) == 0 {
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor > len(m.flatRes)-1 {
		m.cursor = len(m.flatRes) - 1
	}
	m.rebuild()
	m.ensureVisible()
}

func (m *summaryTUIModel) toggleCurrent() {
	if len(m.flatRes) == 0 {
		return
	}
	addr := m.flatRes[m.cursor].address
	m.expanded[addr] = !m.expanded[addr]
	m.rebuild()
	m.ensureVisible()
}

func (m *summaryTUIModel) toggleAll() {
	anyClosed := false
	for _, r := range m.flatRes {
		if !m.expanded[r.address] {
			anyClosed = true
			break
		}
	}
	for _, r := range m.flatRes {
		m.expanded[r.address] = anyClosed
	}
	m.rebuild()
	m.ensureVisible()
}

func (m *summaryTUIModel) layout() {
	if !m.ready {
		return
	}
	header := m.headerView()
	m.headerHeight = lipgloss.Height(header)
	footerHeight := lipgloss.Height(m.footerView())
	bodyHeight := m.height - m.headerHeight - footerHeight
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	m.vp.Width = m.width
	m.vp.Height = bodyHeight
}

func (m summaryTUIModel) headerView() string {
	e := emojiSets[m.emoji]
	t := tierTokens[m.model.verdict]
	v := verdicts[m.model.verdict]
	title := v.title
	if vem := verdictEmoji(e, m.model.verdict); vem != "" {
		title = vem + "  " + v.title
	}
	banner := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(t.hex)).
		Foreground(lipgloss.Color(t.hex)).
		Bold(true).
		Padding(0, 1).
		Render(title)

	c := m.model.counts
	col := func(hex, s string) string { return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render(s) }
	counts := dimStyle.Render("Plan: ") +
		col(tierTokens["safe"].hex, fmt.Sprintf("%d to add", c.add)) + dimStyle.Render(", ") +
		col(tierTokens["caution"].hex, fmt.Sprintf("%d to change", c.change)) + dimStyle.Render(", ") +
		col(tierTokens["danger"].hex, fmt.Sprintf("%d to destroy", c.destroy)) + dimStyle.Render("  ·  ") +
		col(tierTokens["danger"].hex, fmt.Sprintf("%d replace", c.replace))

	return lipgloss.JoinVertical(lipgloss.Left,
		" "+banner,
		"  "+dimStyle.Render(v.sub),
		"  "+counts,
		"  "+m.tierBar(40),
		"",
	)
}

// tierBar draws a proportion bar weighted by tier counts; it fills in from the
// left on entrance (barPos 0..1) for a subtle animated reveal.
func (m summaryTUIModel) tierBar(width int) string {
	total := len(m.flatRes)
	if total == 0 {
		return ""
	}
	filled := int(math.Round(float64(width) * clamp01(m.barPos)))
	var b strings.Builder
	pos := 0
	for _, g := range m.model.groups {
		seg := int(math.Round(float64(width) * float64(len(g.resources)) / float64(total)))
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(g.hex))
		for i := 0; i < seg; i++ {
			if pos < filled {
				b.WriteString(style.Render("█"))
			} else {
				b.WriteString(dimStyle.Render("░"))
			}
			pos++
		}
	}
	for pos < width {
		b.WriteString(dimStyle.Render("░"))
		pos++
	}
	return b.String()
}

// rebuild renders the scrollable body, marking each resource row as a mouse zone.
func (m *summaryTUIModel) rebuild() {
	e := emojiSets[m.emoji]
	var b strings.Builder
	m.resStartLine = make([]int, len(m.flatRes))
	line := 0
	emit := func(s string) {
		b.WriteString(s)
		b.WriteByte('\n')
		line++
	}

	hoverStyle := lipgloss.NewStyle().Background(lipgloss.Color("#1A1B1F"))
	sel := 0
	for _, g := range m.model.groups {
		gem := tierEmoji(e, g.key)
		gStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(g.hex))
		prefix := ""
		if gem != "" {
			prefix = gem + " "
		}
		emit(fmt.Sprintf("%s%s %s", prefix, gStyle.Render(g.label), dimStyle.Render(fmt.Sprintf("(%d)", len(g.resources)))))

		for _, r := range g.resources {
			thisSel := sel
			m.resStartLine[thisSel] = line
			expanded := m.expanded[r.address]
			caret := "▸"
			if expanded {
				caret = "▾"
			}
			rem := tierEmoji(e, r.tier)
			rp := ""
			if rem != "" {
				rp = rem + " "
			}
			rowText := fmt.Sprintf("  %s %s%s %s%s", caret, rp, gStyle.Render(r.glyph), dimStyle.Render(r.modulePrefix), r.short)
			switch {
			case thisSel == m.cursor:
				rowText = selStyle.Render(padTo(rowText, m.vp.Width))
			case thisSel == m.hover:
				rowText = hoverStyle.Render(padTo(rowText, m.vp.Width))
			}
			emit(zone.Mark(m.zoneID(thisSel), rowText))

			if expanded {
				if r.reason != "" {
					emit("      " + dimStyle.Render("↳ "+r.reason))
				}
				for _, d := range r.diff {
					hex := signHex(d.sign)
					emit("        " + lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render(d.sign+" "+d.text))
				}
			}
			sel++
		}
		emit("")
	}

	m.vp.SetContent(strings.TrimRight(b.String(), "\n"))
}

// ensureVisible sets the scroll goal so the selected resource stays on screen;
// the spring animates the viewport toward it.
func (m *summaryTUIModel) ensureVisible() {
	if len(m.flatRes) == 0 || m.cursor >= len(m.resStartLine) {
		return
	}
	start := m.resStartLine[m.cursor]
	goal := m.scrollGoal
	if float64(start) < goal {
		goal = float64(start)
	} else if start > int(goal)+m.vp.Height-1 {
		goal = float64(start - m.vp.Height + 1)
	}
	m.setScrollTarget(goal)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// signHex maps a diff sign to a truecolor hex.
func signHex(sign string) string {
	switch sign {
	case "+":
		return tierTokens["safe"].hex
	case "-":
		return tierTokens["danger"].hex
	case "~":
		return tierTokens["caution"].hex
	default:
		return "#6E7480"
	}
}

func (m summaryTUIModel) View() string {
	if !m.ready {
		return "loading…"
	}
	out := lipgloss.JoinVertical(lipgloss.Left,
		m.headerView(),
		m.vp.View(),
		m.footerView(),
	)
	// resolve the mouse zones marked during rebuild
	return zone.Scan(out)
}

// footerView renders the clickable key hints (short, or expanded on ?).
func (m summaryTUIModel) footerView() string {
	footer := footerItems(summaryKeys.ShortHelp())
	if m.help.ShowAll {
		footer = footerFull(summaryKeys.FullHelp())
	}
	if !m.focused {
		footer = lipgloss.NewStyle().Faint(true).Render(footer)
	}
	return footer
}

// padTo pads a (possibly styled) line with spaces to a visible width.
func padTo(s string, width int) string {
	if w := lipgloss.Width(s); w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}

// runSummaryTUI launches the interactive plan summary.
func runSummaryTUI(m summaryModel, emoji string) error {
	log.SetOutput(io.Discard)
	zone.NewGlobal()
	defer zone.Close()
	mm := newSummaryTUIModel(m, emoji)
	_, err := tea.NewProgram(mm,
		tea.WithAltScreen(),
		tea.WithMouseAllMotion(), // clicks + hover motion
		tea.WithReportFocus(),    // dim when the terminal loses focus
	).Run()
	return err
}
