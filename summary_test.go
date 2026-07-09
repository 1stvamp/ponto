package main

import (
	"strings"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"
)

func TestClassifyAction(t *testing.T) {
	cases := []struct {
		name    string
		actions tfjson.Actions
		action  string
		tier    string
		ok      bool
	}{
		{"create", tfjson.Actions{tfjson.ActionCreate}, "create", "safe", true},
		{"read", tfjson.Actions{tfjson.ActionRead}, "read", "safe", true},
		{"update", tfjson.Actions{tfjson.ActionUpdate}, "update", "caution", true},
		{"delete", tfjson.Actions{tfjson.ActionDelete}, "destroy", "danger", true},
		{"replace-dbc", tfjson.Actions{tfjson.ActionDelete, tfjson.ActionCreate}, "replace", "danger", true},
		{"replace-cbd", tfjson.Actions{tfjson.ActionCreate, tfjson.ActionDelete}, "replace", "danger", true},
		{"noop", tfjson.Actions{tfjson.ActionNoop}, "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			action, _, tier, ok := classifyAction(c.actions)
			if ok != c.ok || action != c.action || tier != c.tier {
				t.Errorf("classifyAction(%v) = (%q,%q,%v), want (%q,%q,%v)", c.actions, action, tier, ok, c.action, c.tier, c.ok)
			}
		})
	}
}

func rc(addr, module string, actions tfjson.Actions, change *tfjson.Change) *tfjson.ResourceChange {
	change.Actions = actions
	return &tfjson.ResourceChange{Address: addr, ModuleAddress: module, Change: change}
}

func TestClassifyPlanVerdictAndCounts(t *testing.T) {
	plan := &tfjson.Plan{
		ResourceChanges: []*tfjson.ResourceChange{
			rc("aws_x.create", "", tfjson.Actions{tfjson.ActionCreate}, &tfjson.Change{After: map[string]interface{}{"a": 1}}),
			rc("aws_x.update", "", tfjson.Actions{tfjson.ActionUpdate}, &tfjson.Change{Before: map[string]interface{}{"a": 1}, After: map[string]interface{}{"a": 2}}),
			rc("module.db.aws_db.primary", "module.db", tfjson.Actions{tfjson.ActionDelete, tfjson.ActionCreate}, &tfjson.Change{ReplacePaths: []interface{}{[]interface{}{"engine"}}}),
			rc("aws_x.gone", "", tfjson.Actions{tfjson.ActionDelete}, &tfjson.Change{Before: map[string]interface{}{"a": 1}}),
			rc("aws_x.noop", "", tfjson.Actions{tfjson.ActionNoop}, &tfjson.Change{}),
		},
	}
	m := classifyPlan(plan)

	if m.verdict != "danger" {
		t.Errorf("verdict = %q, want danger", m.verdict)
	}
	// no-op omitted → 4 classified resources
	if len(m.resources) != 4 {
		t.Errorf("resources = %d, want 4", len(m.resources))
	}
	// add = creates(1) + replaces(1) = 2; change = 1; destroy = destroy(1)+replace(1) = 2; replace = 1
	if m.counts.add != 2 || m.counts.change != 1 || m.counts.destroy != 2 || m.counts.replace != 1 {
		t.Errorf("counts = %+v, want add=2 change=1 destroy=2 replace=1", m.counts)
	}
	if m.tierCount["danger"] != 2 || m.tierCount["caution"] != 1 || m.tierCount["safe"] != 1 {
		t.Errorf("tierCount = %v", m.tierCount)
	}
	// groups worst-first, empty tiers omitted
	if len(m.groups) != 3 || m.groups[0].key != "danger" {
		t.Errorf("groups order wrong: %+v", m.groups)
	}
}

func TestClassifyPlanVerdictEscalation(t *testing.T) {
	// only updates → caution
	caution := classifyPlan(&tfjson.Plan{ResourceChanges: []*tfjson.ResourceChange{
		rc("a.b", "", tfjson.Actions{tfjson.ActionUpdate}, &tfjson.Change{Before: map[string]interface{}{"x": 1}, After: map[string]interface{}{"x": 2}}),
	}})
	if caution.verdict != "caution" {
		t.Errorf("verdict = %q, want caution", caution.verdict)
	}
	// only creates → safe
	safe := classifyPlan(&tfjson.Plan{ResourceChanges: []*tfjson.ResourceChange{
		rc("a.b", "", tfjson.Actions{tfjson.ActionCreate}, &tfjson.Change{After: map[string]interface{}{"x": 1}}),
	}})
	if safe.verdict != "safe" {
		t.Errorf("verdict = %q, want safe", safe.verdict)
	}
}

func TestBuildDiff(t *testing.T) {
	ch := &tfjson.Change{
		Before:         map[string]interface{}{"engine": "14.7", "gone": "x"},
		After:          map[string]interface{}{"engine": "15.4", "added": "y"},
		AfterUnknown:   map[string]interface{}{"id": true},
		AfterSensitive: map[string]interface{}{"added": true},
		ReplacePaths:   []interface{}{[]interface{}{"engine"}},
	}
	lines := map[string]diffLine{}
	for _, d := range buildDiff(ch) {
		key := strings.SplitN(d.text, " ", 2)[0]
		lines[key] = d
	}
	if d, ok := lines["engine"]; !ok || d.sign != "~" || !strings.Contains(d.text, "forces replacement") {
		t.Errorf("engine diff = %+v, want ~ with forces replacement", lines["engine"])
	}
	if d, ok := lines["gone"]; !ok || d.sign != "-" {
		t.Errorf("gone diff = %+v, want -", lines["gone"])
	}
	if d, ok := lines["added"]; !ok || d.sign != "+" || !strings.Contains(d.text, "(sensitive value)") {
		t.Errorf("added diff = %+v, want + (sensitive value)", lines["added"])
	}
	if d, ok := lines["id"]; !ok || d.sign != "+" || !strings.Contains(d.text, "(known after apply)") {
		t.Errorf("id diff = %+v, want + (known after apply)", lines["id"])
	}
}

func TestRenderersSmoke(t *testing.T) {
	m := classifyPlan(&tfjson.Plan{ResourceChanges: []*tfjson.ResourceChange{
		rc("aws_x.gone", "", tfjson.Actions{tfjson.ActionDelete}, &tfjson.Change{Before: map[string]interface{}{"a": 1}}),
	}})
	term := renderSummaryTerminal(m, "none")
	for _, want := range []string{"REVIEW REQUIRED", "Danger", "aws_x.gone"} {
		if !strings.Contains(term, want) {
			t.Errorf("terminal render missing %q", want)
		}
	}
	md := renderSummaryMarkdown(m, "dots")
	for _, want := range []string{"## 🔴 Terraform Plan", "```diff", "shields.io/badge/Danger"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown render missing %q", want)
		}
	}
	card := renderSummaryCardHTML(m, "dots")
	if !strings.Contains(card, "Terraform Plan") || !strings.Contains(card, "DOCTYPE") {
		t.Error("card html missing structure")
	}
}
