package main

import (
	"fmt"
	"io"
	"log"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	tfjson "github.com/hashicorp/terraform-json"
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
	Up           key.Binding
	Down         key.Binding
	Filter       key.Binding
	FilterAction key.Binding
	Tree         key.Binding
	All          key.Binding
	Help         key.Binding
	Quit         key.Binding
}

func (k tuiKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Filter, k.FilterAction, k.Tree, k.All, k.Help, k.Quit}
}

func (k tuiKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Up, k.Filter}, {k.FilterAction, k.Tree}, {k.All, k.Help}, {k.Quit}}
}

var keys = tuiKeyMap{
	Up:           key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/↓", "move")),
	Down:         key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Filter:       key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
	FilterAction: key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "filter action")),
	Tree:         key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "tree/flat")),
	All:          key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "changed/all")),
	Help:         key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Quit:         key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
}

// --- styles ---------------------------------------------------------------

var (
	titleStyle   = lipgloss.NewStyle().Bold(true)
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	selStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("236"))
	headerStyle  = lipgloss.NewStyle().Padding(0, 1)
	paneStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1)
	listTitle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(lipgloss.Color("#7655FD")).Padding(0, 1)
	sectionStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	connectStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

// action glyphs, shared with the web legend.
func actionGlyph(change string) string {
	switch change {
	case "create":
		return "+"
	case "update":
		return "~"
	case "delete":
		return "-"
	case "replace":
		return "±"
	default: // no-op, read, empty
		return "."
	}
}

// action -> colour + glyph, matching the web legend. cbSafe swaps to the
// Okabe-Ito colour-blind-safe palette.
func actionStyleCB(change string, cbSafe bool) (string, lipgloss.Style) {
	normal := map[string]string{"create": "42", "update": "39", "delete": "196", "replace": "214"}
	cb := map[string]string{"create": "36", "update": "74", "delete": "173", "replace": "179"}
	pal := normal
	if cbSafe {
		pal = cb
	}
	if c, ok := pal[change]; ok {
		return actionGlyph(change), lipgloss.NewStyle().Foreground(lipgloss.Color(c))
	}
	return ".", dimStyle
}

// actionStyle keeps the original signature for callers that do not toggle CB.
func actionStyle(change string) (string, lipgloss.Style) {
	return actionStyleCB(change, false)
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
	prefix string // tree connector (empty in flat mode)
	label  string // display label (full id flat, short id in tree)
	tree   bool
}

func (i resItem) FilterValue() string { return i.id }

type resDelegate struct{ cbSafe bool }

func (d resDelegate) Height() int                             { return 1 }
func (d resDelegate) Spacing() int                            { return 0 }
func (d resDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d resDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(resItem)
	if !ok {
		return
	}
	glyph, style := actionStyleCB(it.change, d.cbSafe)
	sel := index == m.Index()

	var line string
	if it.tree {
		line = fmt.Sprintf("%s%s %s", it.prefix, glyph, it.label)
	} else {
		arrow := "  "
		if sel {
			arrow = "> "
		}
		line = fmt.Sprintf("%s%s %s", arrow, glyph, it.label)
	}
	if sel {
		// Fill the row so the selection background spans the pane width.
		if pw := m.Width(); pw > lipgloss.Width(line) {
			line += strings.Repeat(" ", pw-lipgloss.Width(line))
		}
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

	spinner      spinner.Model
	list         list.Model
	detail       viewport.Model
	help         help.Model
	ready        bool // window size known
	width        int
	height       int
	showAll      bool
	planNote     string // set when there are no changes (fallback to all)
	tree         bool   // dependency-forest list vs flat list
	actionFilter string // "all" or a specific change action
	cbSafe       bool   // colour-blind-safe palette

	// derived from the graph, once assets are ready
	resources    []resItem              // every resource, sorted
	changedKeep  map[string]bool        // ids to show in changed-only mode
	dependencies map[string][]string    // id -> what it depends on
	dependents   map[string][]string    // id -> what depends on it
	counts       map[string]int         // action -> count
	change       map[string]string      // id -> change action
	config       map[string][][2]string // id -> ordered [key,value] config pairs
	lastSelected string
}

func newTUIModel(r *ponto) tuiModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	return tuiModel{
		r:            r,
		state:        stateLaunch,
		spinner:      sp,
		help:         help.New(),
		tree:         true,
		actionFilter: "all",
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
		case m.state == stateExplorer && key.Matches(msg, keys.FilterAction):
			m.actionFilter = nextActionFilter(m.actionFilter)
			m.refreshItems()
			m.syncDetail()
			return m, nil
		case m.state == stateExplorer && key.Matches(msg, keys.Tree):
			m.tree = !m.tree
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
	l := list.New(nil, resDelegate{cbSafe: m.cbSafe}, 0, 0)
	l.Title = "Resources"
	// Pre-render the title ourselves, so keep the list's own title style a passthrough.
	l.Styles.Title = lipgloss.NewStyle()
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
	m.change = map[string]string{}
	m.config = m.buildConfig()

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
		m.change[n.Data.ID] = n.Data.Change
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

// nextActionFilter cycles the action filter: all -> create -> update -> delete -> replace -> all.
func nextActionFilter(cur string) string {
	order := []string{"all", "create", "update", "delete", "replace"}
	for i, a := range order {
		if a == cur {
			return order[(i+1)%len(order)]
		}
	}
	return "all"
}

// visibleLeaves returns the resource ids passing the changed and action filters.
func (m *tuiModel) visibleLeaves() []resItem {
	var out []resItem
	for _, r := range m.resources {
		if !m.showAll && !m.changedKeep[r.id] {
			continue
		}
		if m.actionFilter != "all" {
			cur := r.change
			if cur == "" {
				cur = "no-op"
			}
			if cur != m.actionFilter {
				continue
			}
		}
		out = append(out, r)
	}
	return out
}

func (m *tuiModel) refreshItems() {
	vis := m.visibleLeaves()

	var items []list.Item
	if m.tree {
		ids := make([]string, len(vis))
		for i, r := range vis {
			ids[i] = r.id
		}
		forest := buildForest(ids, m.dependencies)
		for _, n := range forest {
			items = append(items, resItem{
				id:     n.id,
				change: m.change[n.id],
				prefix: treeConnector(n.prefixLasts),
				label:  shortID(n.id),
				tree:   true,
			})
		}
	} else {
		for _, r := range vis {
			items = append(items, resItem{id: r.id, change: r.change, label: r.id})
		}
	}
	m.list.SetItems(items)
	m.list.Title = listTitle.Render("Resources") + "  " + dimStyle.Render(fmt.Sprintf("%d shown", len(vis)))
}

// shortID keeps the last two dotted segments (e.g. module.x.random_pet.pet -> random_pet.pet).
func shortID(id string) string {
	parts := strings.Split(id, ".")
	if len(parts) <= 2 {
		return id
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

// forestNode is a node placed in a dependency forest with per-depth "is last child" flags.
type forestNode struct {
	id          string
	prefixLasts []bool
}

// buildForest arranges ids into a dependency forest: a node's parent is its
// first in-set dependency; nodes with no in-set dependency are roots.
func buildForest(ids []string, deps map[string][]string) []forestNode {
	set := map[string]bool{}
	for _, id := range ids {
		set[id] = true
	}
	depIn := map[string][]string{}
	childOf := map[string][]string{}
	var roots []string
	for _, id := range ids {
		var in []string
		for _, d := range deps[id] {
			if set[d] {
				in = append(in, d)
			}
		}
		sort.Strings(in)
		depIn[id] = in
		if len(in) == 0 {
			roots = append(roots, id)
		} else {
			childOf[in[0]] = append(childOf[in[0]], id)
		}
	}
	sort.Strings(roots)
	for k := range childOf {
		sort.Strings(childOf[k])
	}

	var out []forestNode
	seen := map[string]bool{}
	var walk func(id string, prefixLasts []bool)
	walk = func(id string, prefixLasts []bool) {
		if seen[id] {
			return
		}
		seen[id] = true
		out = append(out, forestNode{id: id, prefixLasts: append([]bool{}, prefixLasts...)})
		var kids []string
		for _, k := range childOf[id] {
			if !seen[k] {
				kids = append(kids, k)
			}
		}
		for i, k := range kids {
			walk(k, append(prefixLasts, i == len(kids)-1))
		}
	}
	for _, r := range roots {
		walk(r, []bool{true})
	}
	return out
}

// treeConnector renders box-drawing connectors from a node's ancestor is-last flags.
func treeConnector(prefixLasts []bool) string {
	var s strings.Builder
	for i := 1; i < len(prefixLasts); i++ {
		if i == len(prefixLasts)-1 {
			if prefixLasts[i] {
				s.WriteString("└─ ")
			} else {
				s.WriteString("├─ ")
			}
		} else {
			if prefixLasts[i] {
				s.WriteString("   ")
			} else {
				s.WriteString("│  ")
			}
		}
	}
	return s.String()
}

// buildConfig walks the plan configuration and returns, per resource/data id,
// its configurable attributes as ordered key/value pairs. Values render as a
// reference (e.g. random_integer.pet_length.result) or a constant literal.
func (m *tuiModel) buildConfig() map[string][][2]string {
	out := map[string][][2]string{}
	if m.r.Plan == nil || m.r.Plan.Config == nil || m.r.Plan.Config.RootModule == nil {
		return out
	}
	var walk func(mod *tfjson.ConfigModule, prefix string)
	walk = func(mod *tfjson.ConfigModule, prefix string) {
		if mod == nil {
			return
		}
		for _, res := range mod.Resources {
			addr := res.Address
			// Node ids drop the leading "data." on data sources.
			if res.Mode == tfjson.DataResourceMode {
				addr = strings.TrimPrefix(addr, "data.")
			}
			id := prefix + addr
			var pairs [][2]string
			keys := make([]string, 0, len(res.Expressions))
			for k := range res.Expressions {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if v := exprValue(res.Expressions[k]); v != "" {
					pairs = append(pairs, [2]string{k, v})
				}
			}
			if len(pairs) > 0 {
				out[id] = pairs
			}
		}
		for name, call := range mod.ModuleCalls {
			if call != nil {
				walk(call.Module, prefix+"module."+name+".")
			}
		}
	}
	walk(m.r.Plan.Config.RootModule, "")
	return out
}

// exprValue renders a config expression as a reference or constant literal.
func exprValue(e *tfjson.Expression) string {
	if e == nil || e.ExpressionData == nil {
		return ""
	}
	if len(e.References) > 0 {
		return e.References[0]
	}
	if e.ConstantValue == tfjson.UnknownConstantValue {
		return "(known after apply)"
	}
	if e.ConstantValue == nil {
		return ""
	}
	if s, ok := e.ConstantValue.(string); ok {
		return strconv.Quote(s)
	}
	return fmt.Sprintf("%v", e.ConstantValue)
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
	glyph, style := actionStyleCB(it.change, m.cbSafe)
	action := it.change
	if action == "" {
		action = "no-op"
	}
	fmt.Fprintf(&b, "%s\n", titleStyle.Render(it.id))
	fmt.Fprintf(&b, "%s %s\n\n", style.Render(glyph), style.Render(action))

	// Dependency map: transitive upstream chain, the node, then downstream.
	fmt.Fprintf(&b, "%s\n", sectionStyle.Render("Dependency map"))
	up := transitive(it.id, m.dependencies)
	down := transitive(it.id, m.dependents)
	mapIDs := append(append([]string{}, up...), it.id)
	mapIDs = append(mapIDs, down...)
	mapIDs = uniqueSorted(mapIDs)
	for _, n := range buildForest(mapIDs, m.dependencies) {
		g, gs := actionStyleCB(m.change[n.id], m.cbSafe)
		conn := connectStyle.Render("  " + treeConnector(n.prefixLasts))
		if n.id == it.id {
			fmt.Fprintf(&b, "%s%s %s%s\n", conn, gs.Render(g), selStyle.Render(n.id), selStyle.Render("  ◄"))
			continue
		}
		idStyle := dimStyle
		if m.change[n.id] != "" {
			idStyle = gs
		}
		fmt.Fprintf(&b, "%s%s %s\n", conn, gs.Render(g), idStyle.Render(n.id))
	}

	// Config: the resource's configurable attributes.
	fmt.Fprintf(&b, "\n%s\n", sectionStyle.Render("Config"))
	if cfg := m.config[it.id]; len(cfg) > 0 {
		for _, kv := range cfg {
			fmt.Fprintf(&b, "%s%s\n", dimStyle.Render(fmt.Sprintf("  %-11s= ", kv[0])), kv[1])
		}
	} else {
		fmt.Fprintf(&b, "%s\n", dimStyle.Render("  (no configurable attributes)"))
	}
	return b.String()
}

// transitive returns all ids reachable from start through the given adjacency
// map (depth-first), excluding start itself.
func transitive(start string, adj map[string][]string) []string {
	var out []string
	seen := map[string]bool{start: true}
	var walk func(id string)
	walk = func(id string) {
		for _, n := range adj[id] {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
				walk(n)
			}
		}
	}
	walk(start)
	return out
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
	headerHeight := 2 // header line + action-filter bar
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
	// header: Ponto + summary on the left, view mode on the right
	filter := "changed + connected"
	if m.showAll {
		filter = "all"
	}
	if m.planNote != "" {
		filter = m.planNote + " (showing all)"
	}
	hleft := titleStyle.Render("Ponto") + "  " + m.summary()
	hright := dimStyle.Render("view: " + filter)
	gap := m.width - 2 - lipgloss.Width(hleft) - lipgloss.Width(hright)
	if gap < 1 {
		gap = 1
	}
	header := headerStyle.Render(hleft + strings.Repeat(" ", gap) + hright)
	filterBar := headerStyle.Render(m.filterBar())

	left := paneStyle.Render(m.list.View())
	right := paneStyle.Render(m.detail.View())
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	footer := m.help.View(keys)
	return lipgloss.JoinVertical(lipgloss.Left, header, filterBar, body, footer)
}

// filterBar renders the action-filter segments: filter: [all] + create ~ update ...
func (m tuiModel) filterBar() string {
	segs := []struct{ key, label string }{
		{"all", "all"}, {"create", "create"}, {"update", "update"}, {"delete", "delete"}, {"replace", "replace"},
	}
	parts := []string{dimStyle.Render("filter:")}
	for _, s := range segs {
		on := m.actionFilter == s.key
		glyph := ""
		if s.key != "all" {
			glyph = actionGlyph(s.key) + " "
		}
		text := glyph + s.label
		if on {
			text = "[" + text + "]"
		}
		var style lipgloss.Style
		switch {
		case on && s.key == "all":
			style = titleStyle
		case on:
			_, style = actionStyleCB(s.key, m.cbSafe)
			style = style.Bold(true)
		default:
			style = dimStyle
		}
		parts = append(parts, style.Render(text))
	}
	return strings.Join(parts, " ")
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
