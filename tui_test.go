package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// db is created; app (created) and logs (unchanged) both depend on db, so logs
// is in the blast radius; unrelated is an unconnected no-op.
func tuiTestGraph() Graph {
	return Graph{
		Nodes: []Node{
			{Data: NodeData{ID: "aws_db.main", Change: "create"}, Classes: "resource-name create"},
			{Data: NodeData{ID: "aws_app.web", Change: "create"}, Classes: "resource-name create"},
			{Data: NodeData{ID: "aws_logs.audit", Change: "no-op"}, Classes: "resource-name no-op"},
			{Data: NodeData{ID: "aws_unrelated.x", Change: "no-op"}, Classes: "resource-name no-op"},
			{Data: NodeData{ID: "aws_db", Change: ""}, Classes: "resource-type"}, // container, not a resource row
		},
		Edges: []Edge{
			{Data: EdgeData{Source: "aws_app.web", Target: "aws_db.main"}},
			{Data: EdgeData{Source: "aws_logs.audit", Target: "aws_db.main"}},
		},
	}
}

func readyExplorer(t *testing.T) tuiModel {
	t.Helper()
	m := newTUIModel(&ponto{Graph: tuiTestGraph(), WorkingDir: ".", TfPath: "/bin/terraform"})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(tuiModel)
	next, _ = m.Update(assetsReadyMsg{})
	m = next.(tuiModel)
	if m.state != stateExplorer {
		t.Fatalf("expected explorer state, got %d", m.state)
	}
	return m
}

func TestBuildGraphDataFilter(t *testing.T) {
	m := readyExplorer(t)

	if got := m.counts["create"]; got != 2 {
		t.Errorf("create count = %d, want 2", got)
	}
	if got := m.counts["no-op"]; got != 2 {
		t.Errorf("no-op count = %d, want 2", got)
	}
	// only the 4 "-name" nodes are resources; the container is excluded
	if len(m.resources) != 4 {
		t.Errorf("resources = %d, want 4", len(m.resources))
	}
	// changed-only keep: db (changed), app (changed), logs (blast radius); not unrelated
	for _, id := range []string{"aws_db.main", "aws_app.web", "aws_logs.audit"} {
		if !m.changedKeep[id] {
			t.Errorf("expected %s in changed keep set", id)
		}
	}
	if m.changedKeep["aws_unrelated.x"] {
		t.Errorf("unrelated no-op should not be in changed keep set")
	}
}

func TestExplorerItemsAndToggle(t *testing.T) {
	m := readyExplorer(t)

	if n := len(m.list.Items()); n != 3 {
		t.Errorf("changed-only list = %d items, want 3", n)
	}
	// toggle to all
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = next.(tuiModel)
	if !m.showAll {
		t.Fatal("expected showAll after 'a'")
	}
	if n := len(m.list.Items()); n != 4 {
		t.Errorf("all list = %d items, want 4", n)
	}
}

func TestExplorerViewAndDetail(t *testing.T) {
	m := readyExplorer(t)

	view := m.View()
	for _, want := range []string{"Ponto", "aws_db.main", "to create"} {
		if !strings.Contains(view, want) {
			t.Errorf("explorer view missing %q", want)
		}
	}

	// detail for the changed db should list its dependents (blast radius)
	detail := m.detailContent(resItem{id: "aws_db.main", change: "create"})
	for _, want := range []string{"aws_app.web", "aws_logs.audit", "blast radius"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail missing %q", want)
		}
	}
}
