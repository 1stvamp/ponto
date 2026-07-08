package main

import (
	"fmt"
	"io"
	"log"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// runTUI drives the interactive terminal UI: a launch screen, a spinner while
// the plan is generated, then a two-pane plan explorer. It owns asset
// generation so it can show progress instead of logging over the screen.
func runTUI(r *ponto) error {
	// The plan pipeline logs to the standard logger; in the alt-screen TUI that
	// would corrupt the display, so silence it (and terraform's own output) for
	// the duration.
	log.SetOutput(io.Discard)
	r.Verbose = false

	m := newTUIModel(r)
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

type tuiState int

const (
	stateLaunch tuiState = iota
	stateLoading
	stateExplorer
	stateError
)

// --- messages -------------------------------------------------------------

type assetsReadyMsg struct{ err error }

func generateCmd(r *ponto) tea.Cmd {
	return func() tea.Msg {
		return assetsReadyMsg{err: r.generateAssets()}
	}
}

// --- key map --------------------------------------------------------------

type tuiKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Filter key.Binding
	All    key.Binding
	Help   key.Binding
	Quit   key.Binding
}

func (k tuiKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Filter, k.All, k.Help, k.Quit}
}

func (k tuiKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Up, k.Down}, {k.Filter, k.All}, {k.Help, k.Quit}}
}

var keys = tuiKeyMap{
	Up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Filter: key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	All:    key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "changed/all")),
	Help:   key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
}

// --- styles ---------------------------------------------------------------

var (
	titleStyle  = lipgloss.NewStyle().Bold(true)
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	selStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	headerStyle = lipgloss.NewStyle().Padding(0, 1)
	paneStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
)

// action -> colour + glyph, matching the web legend.
func actionStyle(change string) (string, lipgloss.Style) {
	switch change {
	case "create":
		return "+", lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	case "update":
		return "~", lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	case "delete":
		return "-", lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	case "replace":
		return "±", lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	default: // no-op, read, empty
		return ".", dimStyle
	}
}

func isChangedAction(change string) bool {
	switch change {
	case "create", "update", "delete", "replace":
		return true
	}
	return false
}

// --- resource items -------------------------------------------------------

type resItem struct {
	id     string
	change string
}

func (i resItem) FilterValue() string { return i.id }

type resDelegate struct{}

func (d resDelegate) Height() int                             { return 1 }
func (d resDelegate) Spacing() int                            { return 0 }
func (d resDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d resDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(resItem)
	if !ok {
		return
	}
	glyph, style := actionStyle(it.change)
	prefix := "  "
	if index == m.Index() {
		prefix = "> "
	}
	line := fmt.Sprintf("%s%s %s", prefix, glyph, it.id)
	if index == m.Index() {
		fmt.Fprint(w, selStyle.Render(line))
	} else {
		fmt.Fprint(w, style.Render(line))
	}
}

// --- model ----------------------------------------------------------------

type tuiModel struct {
	r     *ponto
	state tuiState
	err   error

	spinner  spinner.Model
	list     list.Model
	detail   viewport.Model
	help     help.Model
	ready    bool // window size known
	width    int
	height   int
	showAll  bool
	planNote string // set when there are no changes (fallback to all)

	// derived from the graph, once assets are ready
	resources    []resItem           // every resource, sorted
	changedKeep  map[string]bool     // ids to show in changed-only mode
	dependencies map[string][]string // id -> what it depends on
	dependents   map[string][]string // id -> what depends on it
	counts       map[string]int      // action -> count
	lastSelected string
}

func newTUIModel(r *ponto) tuiModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	return tuiModel{
		r:       r,
		state:   stateLaunch,
		spinner: sp,
		help:    help.New(),
	}
}

func (m tuiModel) Init() tea.Cmd { return nil }

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.Width = msg.Width
		m.ready = true
		m.layout()
		return m, nil

	case tea.KeyMsg:
		// While filtering, let the list consume keys (except it handles esc/enter itself).
		if m.state == stateExplorer && m.list.FilterState() == list.Filtering {
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			m.syncDetail()
			return m, cmd
		}
		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit
		case m.state == stateLaunch && msg.String() == "enter":
			m.state = stateLoading
			return m, tea.Batch(m.spinner.Tick, generateCmd(m.r))
		case m.state == stateError:
			return m, nil // any non-quit key ignored
		case m.state == stateExplorer && key.Matches(msg, keys.Help):
			m.help.ShowAll = !m.help.ShowAll
			m.layout()
			return m, nil
		case m.state == stateExplorer && key.Matches(msg, keys.All):
			m.showAll = !m.showAll
			m.refreshItems()
			m.syncDetail()
			return m, nil
		}

	case spinner.TickMsg:
		if m.state == stateLoading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}

	case assetsReadyMsg:
		if msg.err != nil {
			m.state = stateError
			m.err = msg.err
			return m, nil
		}
		m.buildGraphData()
		m.state = stateExplorer
		m.initExplorer()
		m.layout()
		m.refreshItems()
		m.syncDetail()
		return m, nil
	}

	// Forward to the explorer widgets.
	if m.state == stateExplorer {
		var cmds []tea.Cmd
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
		m.detail, cmd = m.detail.Update(msg)
		cmds = append(cmds, cmd)
		m.syncDetail()
		return m, tea.Batch(cmds...)
	}
	return m, nil
}

func (m tuiModel) View() string {
	switch m.state {
	case stateLaunch:
		return m.launchView()
	case stateLoading:
		return fmt.Sprintf("\n  %s Generating plan (terraform init/plan can take a moment)…\n", m.spinner.View())
	case stateError:
		return fmt.Sprintf("\n  %s\n\n  %s\n",
			lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("Error: "+m.err.Error()),
			dimStyle.Render("press q to quit"))
	case stateExplorer:
		return m.explorerView()
	}
	return ""
}

// --- launch ---------------------------------------------------------------

func (m tuiModel) launchView() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n  %s\n\n", titleStyle.Render("Ponto"))
	fmt.Fprintf(&b, "  %-14s %s\n", "Working dir:", m.r.WorkingDir)
	fmt.Fprintf(&b, "  %-14s %s\n", "Plan source:", m.planSource())
	fmt.Fprintf(&b, "  %-14s %s\n", "Terraform:", m.r.TfPath)
	if m.r.WorkspaceName != "" {
		fmt.Fprintf(&b, "  %-14s %s\n", "Workspace:", m.r.WorkspaceName)
	}
	fmt.Fprintf(&b, "\n  %s\n", dimStyle.Render("enter: start   q: quit"))
	return b.String()
}

func (m tuiModel) planSource() string {
	switch {
	case m.r.PlanJSONPath != "":
		return "provided JSON plan (" + m.r.PlanJSONPath + ")"
	case m.r.PlanPath != "":
		return "provided plan (" + m.r.PlanPath + ")"
	case m.r.TFCWorkspaceName != "":
		return "Terraform Cloud (" + m.r.TFCWorkspaceName + ")"
	default:
		return "will run terraform plan"
	}
}

// --- explorer -------------------------------------------------------------

func (m *tuiModel) initExplorer() {
	l := list.New(nil, resDelegate{}, 0, 0)
	l.Title = "Resources"
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	m.list = l
	m.detail = viewport.New(0, 0)
}

func (m *tuiModel) buildGraphData() {
	m.dependencies = map[string][]string{}
	m.dependents = map[string][]string{}
	m.changedKeep = map[string]bool{}
	m.counts = map[string]int{}

	// edges: source depends on target
	for _, e := range m.r.Graph.Edges {
		s, t := e.Data.Source, e.Data.Target
		m.dependencies[s] = append(m.dependencies[s], t)
		m.dependents[t] = append(m.dependents[t], s)
	}

	// resource/data leaf nodes carry "-name" in their classes
	var changed []string
	for _, n := range m.r.Graph.Nodes {
		if !strings.Contains(n.Classes, "-name") {
			continue
		}
		m.resources = append(m.resources, resItem{id: n.Data.ID, change: n.Data.Change})
		m.counts[n.Data.Change]++
		if isChangedAction(n.Data.Change) {
			changed = append(changed, n.Data.ID)
		}
	}
	sort.Slice(m.resources, func(i, j int) bool { return m.resources[i].id < m.resources[j].id })

	// changed-only keep set: changed + full downstream blast radius + 1-hop upstream
	keep := map[string]bool{}
	for _, id := range changed {
		keep[id] = true
	}
	queue := append([]string{}, changed...)
	for len(queue) > 0 {
		id := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		for _, s := range m.dependents[id] {
			if !keep[s] {
				keep[s] = true
				queue = append(queue, s)
			}
		}
	}
	for _, id := range changed {
		for _, t := range m.dependencies[id] {
			keep[t] = true
		}
	}
	m.changedKeep = keep
	// No changes at all: fall back to showing everything.
	if len(changed) == 0 {
		m.showAll = true
		m.planNote = "no changes"
	}
}

func (m *tuiModel) refreshItems() {
	var items []list.Item
	for _, r := range m.resources {
		if m.showAll || m.changedKeep[r.id] {
			items = append(items, r)
		}
	}
	m.list.SetItems(items)
}

func (m *tuiModel) syncDetail() {
	sel, ok := m.list.SelectedItem().(resItem)
	if !ok {
		m.detail.SetContent(dimStyle.Render("no resource selected"))
		return
	}
	if sel.id == m.lastSelected {
		return
	}
	m.lastSelected = sel.id
	m.detail.SetContent(m.detailContent(sel))
	m.detail.GotoTop()
}

func (m *tuiModel) detailContent(it resItem) string {
	var b strings.Builder
	glyph, style := actionStyle(it.change)
	action := it.change
	if action == "" {
		action = "no-op"
	}
	fmt.Fprintf(&b, "%s\n", titleStyle.Render(it.id))
	fmt.Fprintf(&b, "%s %s\n\n", style.Render(glyph), action)

	deps := uniqueSorted(m.dependencies[it.id])
	fmt.Fprintf(&b, "%s\n", titleStyle.Render("Depends on"))
	if len(deps) == 0 {
		fmt.Fprintf(&b, "%s\n", dimStyle.Render("  (nothing)"))
	} else {
		for _, d := range deps {
			fmt.Fprintf(&b, "  %s\n", d)
		}
	}

	users := uniqueSorted(m.dependents[it.id])
	fmt.Fprintf(&b, "\n%s\n", titleStyle.Render("Depended on by (blast radius)"))
	if len(users) == 0 {
		fmt.Fprintf(&b, "%s\n", dimStyle.Render("  (nothing)"))
	} else {
		for _, u := range users {
			fmt.Fprintf(&b, "  %s\n", u)
		}
	}
	return b.String()
}

func uniqueSorted(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func (m *tuiModel) layout() {
	if !m.ready || m.state != stateExplorer {
		return
	}
	helpHeight := 1
	if m.help.ShowAll {
		helpHeight = 3
	}
	headerHeight := 2
	// account for pane borders (2) + header + help
	bodyHeight := m.height - headerHeight - helpHeight - 2
	if bodyHeight < 3 {
		bodyHeight = 3
	}
	leftWidth := m.width * 2 / 5
	if leftWidth < 24 {
		leftWidth = 24
	}
	rightWidth := m.width - leftWidth - 6 // borders + gap
	if rightWidth < 20 {
		rightWidth = 20
	}
	m.list.SetSize(leftWidth, bodyHeight)
	m.detail.Width = rightWidth
	m.detail.Height = bodyHeight
}

func (m tuiModel) explorerView() string {
	if !m.ready {
		return "loading…"
	}
	// header
	filter := "changed + connected"
	if m.showAll {
		filter = "all"
	}
	if m.planNote != "" {
		filter = m.planNote + " (showing all)"
	}
	header := headerStyle.Render(fmt.Sprintf("%s   %s   %s",
		titleStyle.Render("Ponto"), m.summary(), dimStyle.Render("view: "+filter)))

	left := paneStyle.Render(m.list.View())
	right := paneStyle.Render(m.detail.View())
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	footer := m.help.View(keys)
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m tuiModel) summary() string {
	order := []struct {
		action, label string
	}{
		{"create", "create"}, {"update", "change"}, {"delete", "destroy"}, {"replace", "replace"},
	}
	var parts []string
	for _, o := range order {
		if n := m.counts[o.action]; n > 0 {
			_, style := actionStyle(o.action)
			parts = append(parts, style.Render(fmt.Sprintf("%d to %s", n, o.label)))
		}
	}
	if len(parts) == 0 {
		return dimStyle.Render("no changes")
	}
	return strings.Join(parts, dimStyle.Render(" · "))
}
