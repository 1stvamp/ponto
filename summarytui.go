package main

import (
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

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

	vp     viewport.Model
	help   help.Model
	ready  bool
	width  int
	height int

	headerHeight int
	// line bookkeeping for scroll + mouse hit-testing, rebuilt each render
	lineToRes    []int // content line -> flatRes index, or -1
	resStartLine []int // flatRes index -> first content line
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
	}
}

func (m summaryTUIModel) Init() tea.Cmd { return nil }

func (m summaryTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.Width = msg.Width
		m.ready = true
		m.layout()
		m.rebuild()
		return m, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, summaryKeys.Quit):
			return m, tea.Quit
		case key.Matches(msg, summaryKeys.Up):
			m.moveCursor(-1)
			return m, nil
		case key.Matches(msg, summaryKeys.Down):
			m.moveCursor(1)
			return m, nil
		case key.Matches(msg, summaryKeys.Toggle):
			m.toggleCurrent()
			return m, nil
		case key.Matches(msg, summaryKeys.All):
			m.toggleAll()
			return m, nil
		case key.Matches(msg, summaryKeys.Help):
			m.help.ShowAll = !m.help.ShowAll
			m.layout()
			m.rebuild()
			return m, nil
		}

	case tea.MouseMsg:
		if !m.ready {
			return m, nil
		}
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.vp.ScrollUp(3)
			return m, nil
		case tea.MouseButtonWheelDown:
			m.vp.ScrollDown(3)
			return m, nil
		case tea.MouseButtonLeft:
			if msg.Action == tea.MouseActionPress {
				m.clickAt(msg.Y)
			}
			return m, nil
		}
	}
	return m, nil
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
}

func (m *summaryTUIModel) toggleCurrent() {
	if len(m.flatRes) == 0 {
		return
	}
	addr := m.flatRes[m.cursor].address
	m.expanded[addr] = !m.expanded[addr]
	m.rebuild()
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
}

// clickAt maps a screen row (in the body area) to a resource and toggles it.
func (m *summaryTUIModel) clickAt(screenY int) {
	line := screenY - m.headerHeight + m.vp.YOffset
	if line < 0 || line >= len(m.lineToRes) {
		return
	}
	if sel := m.lineToRes[line]; sel >= 0 {
		m.cursor = sel
		addr := m.flatRes[sel].address
		m.expanded[addr] = !m.expanded[addr]
		m.rebuild()
	}
}

func (m *summaryTUIModel) layout() {
	if !m.ready {
		return
	}
	header := m.headerView()
	m.headerHeight = lipgloss.Height(header)
	footerHeight := lipgloss.Height(m.help.View(summaryKeys))
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
		BorderForeground(lipgloss.Color(t.ansi)).
		Foreground(lipgloss.Color(t.ansi)).
		Bold(true).
		Padding(0, 1).
		Render(title)

	c := m.model.counts
	col := func(a, s string) string { return lipgloss.NewStyle().Foreground(lipgloss.Color(a)).Render(s) }
	counts := dimStyle.Render("Plan: ") +
		col("42", fmt.Sprintf("%d to add", c.add)) + dimStyle.Render(", ") +
		col("214", fmt.Sprintf("%d to change", c.change)) + dimStyle.Render(", ") +
		col("196", fmt.Sprintf("%d to destroy", c.destroy)) + dimStyle.Render("  ·  ") +
		col("196", fmt.Sprintf("%d replace", c.replace))

	return lipgloss.JoinVertical(lipgloss.Left,
		" "+banner,
		"  "+dimStyle.Render(v.sub),
		"  "+counts,
		"",
	)
}

// rebuild renders the scrollable body and refreshes the line bookkeeping.
func (m *summaryTUIModel) rebuild() {
	e := emojiSets[m.emoji]
	var b strings.Builder
	m.lineToRes = m.lineToRes[:0]
	m.resStartLine = make([]int, len(m.flatRes))

	line := 0
	emit := func(s string, sel int) {
		b.WriteString(s)
		b.WriteByte('\n')
		m.lineToRes = append(m.lineToRes, sel)
		line++
	}

	sel := 0
	for _, g := range m.model.groups {
		gem := tierEmoji(e, g.key)
		gStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(g.ansi))
		prefix := ""
		if gem != "" {
			prefix = gem + " "
		}
		emit(fmt.Sprintf("%s%s %s", prefix, gStyle.Render(g.label), dimStyle.Render(fmt.Sprintf("(%d)", len(g.resources)))), -1)

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
			if thisSel == m.cursor {
				rowText = selStyle.Render(padTo(rowText, m.vp.Width))
			}
			emit(rowText, thisSel)

			if expanded {
				if r.reason != "" {
					emit("      "+dimStyle.Render("↳ "+r.reason), thisSel)
				}
				for _, d := range r.diff {
					col := signColor[d.sign]
					if col == "" {
						col = "244"
					}
					emit("        "+lipgloss.NewStyle().Foreground(lipgloss.Color(col)).Render(d.sign+" "+d.text), thisSel)
				}
			}
			sel++
		}
		emit("", -1)
	}

	m.vp.SetContent(strings.TrimRight(b.String(), "\n"))
	m.ensureVisible()
}

// ensureVisible scrolls the viewport so the selected resource stays on screen.
func (m *summaryTUIModel) ensureVisible() {
	if len(m.flatRes) == 0 || m.cursor >= len(m.resStartLine) {
		return
	}
	start := m.resStartLine[m.cursor]
	if start < m.vp.YOffset {
		m.vp.SetYOffset(start)
	} else if start > m.vp.YOffset+m.vp.Height-1 {
		m.vp.SetYOffset(start - m.vp.Height + 1)
	}
}

func (m summaryTUIModel) View() string {
	if !m.ready {
		return "loading…"
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.headerView(),
		m.vp.View(),
		m.help.View(summaryKeys),
	)
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
	mm := newSummaryTUIModel(m, emoji)
	_, err := tea.NewProgram(mm, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}
