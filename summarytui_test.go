package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	tfjson "github.com/hashicorp/terraform-json"
	zone "github.com/lrstanley/bubblezone"
)

func summaryTUIFixture(t *testing.T) summaryTUIModel {
	t.Helper()
	zone.NewGlobal() // the model marks mouse zones during render

	plan := &tfjson.Plan{
		ResourceChanges: []*tfjson.ResourceChange{
			rc("aws_db.primary", "", tfjson.Actions{tfjson.ActionDelete, tfjson.ActionCreate}, &tfjson.Change{
				Before: map[string]interface{}{"engine": "14.7"}, After: map[string]interface{}{"engine": "15.4"},
				ReplacePaths: []interface{}{[]interface{}{"engine"}},
			}),
			rc("aws_x.gone", "", tfjson.Actions{tfjson.ActionDelete}, &tfjson.Change{Before: map[string]interface{}{"bucket": "b"}}),
			rc("aws_x.new", "", tfjson.Actions{tfjson.ActionCreate}, &tfjson.Change{After: map[string]interface{}{"name": "n"}}),
		},
	}
	m := newSummaryTUIModel(classifyPlan(plan), "none")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return next.(summaryTUIModel)
}

func TestSummaryTUIFlatOrderWorstFirst(t *testing.T) {
	m := summaryTUIFixture(t)
	if len(m.flatRes) != 3 {
		t.Fatalf("flatRes = %d, want 3", len(m.flatRes))
	}
	// danger group first: replace then destroy (sorted by address), then safe create
	if m.flatRes[0].tier != "danger" || m.flatRes[2].tier != "safe" {
		t.Errorf("order wrong: %s .. %s", m.flatRes[0].tier, m.flatRes[2].tier)
	}
}

func TestSummaryTUIExpandOnEnter(t *testing.T) {
	m := summaryTUIFixture(t)
	// nothing expanded → no diff text yet
	if strings.Contains(m.View(), "# forces replacement") {
		t.Fatal("diff shown before expand")
	}
	// cursor on the first (replace) resource; press enter to expand
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(summaryTUIModel)
	if !m.expanded[m.flatRes[0].address] {
		t.Fatal("resource not expanded after enter")
	}
	if !strings.Contains(m.View(), "forces replacement") {
		t.Error("expanded diff missing the forces-replacement marker")
	}
}

func TestSummaryTUIMoveAndToggleAll(t *testing.T) {
	m := summaryTUIFixture(t)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = next.(summaryTUIModel)
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1 after down", m.cursor)
	}
	// expand all
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = next.(summaryTUIModel)
	for _, r := range m.flatRes {
		if !m.expanded[r.address] {
			t.Errorf("%s not expanded after 'e'", r.address)
		}
	}
	// collapse all
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = next.(summaryTUIModel)
	for _, r := range m.flatRes {
		if m.expanded[r.address] {
			t.Errorf("%s still expanded after collapse", r.address)
		}
	}
}
