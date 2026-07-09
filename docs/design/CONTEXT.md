# Ponto design context

This is a handoff pack for a UX/design pass on Ponto. It orients you fast,
points at the code, and hands you screenshots and example inputs so you can
see the two surfaces without building anything first. The repo itself is the
source of truth: this doc just saves you the archaeology.

Ponto is an interactive Terraform (and OpenTofu) plan and state visualiser. It
takes a plan and shows you what will change and how the resources relate. It
grew out of a fork of the abandoned [im2nguyen/rover](https://github.com/im2nguyen/rover),
so some of the DNA (the cytoscape graph, the colour legend) is inherited, not
designed from scratch.

There are two front-ends over the same plan data, and the goal of this pass is
to improve the UX of **both**:

- a **web UI** (Vue 2 + cytoscape), served locally or emitted as a standalone bundle
- a **terminal UI / TUI** (Go + bubbletea), launched with `-i` / `--tui`

## What is in this pack

- `assets/ponto-tui-walkthrough.gif`: a ~20s recording of the TUI going through its states
- `assets/ponto-tui-changed-view.png`: the TUI default view (changed + connected only)
- `assets/ponto-tui-all-view.png`: the same plan with the "all" toggle on
- `assets/ponto-web-changed-filter.png`: the web UI on a mixed plan, changed filter on
- `assets/ponto-web-full.png`: the web UI on a first-apply plan (everything a create)
- `examples/example.tf`: the bundled `random-test` config the screenshots were taken from
- `examples/plan-first-apply.json`: a real plan JSON for that config (12 creates)
- `examples/plan-mixed.json`: the same plan hand-edited to a mix of update/replace/delete/no-op (see note below)

The mixed plan is synthetic: I took the real first-apply plan and rewrote the
`resource_changes[].change.actions` verbs so there is something to filter. Its
action verbs are made up, but the shape (graph, dependencies, config) is real,
and Ponto reads it exactly like any other plan. It exists so the changed-vs-all
behaviour actually does something on screen (a first apply is all creates, so
"changed" and "all" look identical, which hides the whole point of the filter).

## Running it yourself

Everything is driven through mise (`mise tasks` for the full list). To poke at
the same states in the screenshots:

- web UI, mixed plan: `go run . --plan-json-path docs/design/examples/plan-mixed.json --working-dir example/random-test`, then open http://localhost:7668
- TUI, mixed plan: `go run . --tui --plan-json-path docs/design/examples/plan-mixed.json --working-dir example/random-test`
- or just point it at a real config: `mise run run:example` (web) and `mise run run:tui-example` (TUI)

The `--plan-json-path` flag skips running terraform, so you get an instant,
deterministic load with no provider downloads. Good for iterating on the UI.

## Shared model: how a plan is drawn

Both surfaces render the same thing, so keep the semantics consistent across
them. A plan is a set of resources, each with a change action, plus edges: a
resource depends on the things it reads from, and is depended on by the things
that read it (Ponto calls the downstream set the "blast radius").

Action encoding, shared between the web legend and the TUI (`tui.go`
`actionStyle`, and the web legend component):

- create: green, `+`
- update: blue, `~`
- delete: red, `-`
- replace: amber/yellow, `±`
- no-op / read: dimmed, `.`

Resource kinds also get their own styling in the web legend (variable, output,
data, module, local), which the TUI mostly does not surface. That divergence is
one of the things worth looking at.

Note the encoding is colour-first. The TUI adds a glyph, the web UI leans almost
entirely on fill colour. Accessibility (colour-blind safety, contrast) is a real
question for a tool whose whole job is colour-coding change.

## The web UI

Files live under `ui/src`:

- `App.vue`: top-level layout and the changed/unchanged filter state
- `components/Graph/Graph.vue`: the cytoscape graph, klay layout, and the SVG/PNG export
- `components/Explorer.vue`, `File.vue`, `ResourceCard.vue`, `ResourceDetail.vue`: the resource list and detail
- `components/MainNav.vue`: the header

Layout (see `assets/ponto-web-full.png`): a left column with a verbose
Legend/Instructions block and an empty Details panel, a large Graph panel, and a
Resources list stacked underneath. Top-right has "Save Graph" (SVG), "Save PNG",
and a "Show unchanged" checkbox.

The changed filter defaults to on: the graph shows only changed resources plus
what connects them, and "Show unchanged" reveals the rest. Compare
`ponto-web-changed-filter.png` (mixed plan, filtered) with the full first-apply
shot.

Tech constraints you cannot wish away:

- Vue **2.6** with vue-cli 4 / webpack 4. The build needs `NODE_OPTIONS=--openssl-legacy-provider` on modern Node, which tells you how old the toolchain is. A big framework jump (Vue 3, Vite) is out of scope for a UX pass, so design within what Vue 2 SFCs and this component set can do.
- cytoscape with the klay layout, plus `cytoscape-svg` and `cytoscape-node-html-label`. The graph rendering and node markup are constrained by what these plugins do.
- the whole UI is embedded into the Go binary via `go:embed ui/dist`, and can be served or emitted as a standalone static bundle, so it has to work as plain static files (no backend calls at view time).

## The TUI

All of it is in `tui.go` (bubbletea v1 API, with bubbles `list`/`viewport`/`help`
and lipgloss). See `assets/ponto-tui-walkthrough.gif` for the flow.

States: a launcher screen, a loading spinner while terraform runs, an explorer,
and an error screen. The explorer is a two-pane layout: a resource list on the
left (custom delegate, action-coloured), a detail viewport on the right showing
the selected resource's action, what it depends on, and its blast radius. A
header line carries the plan summary ("1 to change · 1 to destroy · 1 to
replace") and the current view mode. A footer carries the key hints.

Keys: `up`/`down` (or `k`/`j`) to move, `/` to fuzzy-filter, `a` to toggle
changed-vs-all, `?` for help, `q` to quit.

The default view is "changed + connected": the changed resources, their direct
dependencies, and their downstream blast radius, with pure no-op noise hidden.
`a` flips to the full graph. The two screenshots (`ponto-tui-changed-view.png`
vs `ponto-tui-all-view.png`) show the same plan collapsing from 13 rows to 4.

Constraint: this is a terminal UI, so it is text, a fixed colour palette, and a
grid of cells. The TUI has no graph/topology view at all: it is a list plus a
detail pane. Whether it should gain some sense of structure is an open question.

## Known pain points

Two sets. First, things the design should probably not break. Second, my own
observations from building and screenshotting this: treat these as candidates,
not a spec. I am not the user, so weigh them accordingly.

### Constraints (keep these)

- keep the create/update/delete/replace/no-op colour+glyph semantics consistent across web and TUI
- keep the LICENSE and the rover copyright notice intact (this is a fork)
- the web UI must keep working as an embedded, backend-less static bundle
- Vue 2 / cytoscape / bubbletea v1 are fixed for this pass

### My observations (candidates, mine not the user's)

Cross-surface:

- the two front-ends have drifted apart in capability. The web UI has a graph but no plan-summary count in the header; the TUI has the count but no graph. A user moving between them relearns the tool each time.
- action is encoded by colour almost everywhere. There is no colour-blind-safe mode, and the web UI barely uses the glyphs the TUI already has.

Web UI:

- the graph respects the changed filter but the Resources list underneath does not: it always shows everything. So "Show unchanged off" filters one half of the screen and not the other (visible in `ponto-web-changed-filter.png`). That is confusing.
- the Details panel sits empty ("Please select a resource on your right") taking up prime top-left space until you click something.
- the Legend/Instructions block is long and static, and eats the left column above the fold.
- there is no search or filter over the Resources list at all, whereas the TUI has fuzzy filtering.
- on a large plan the cytoscape graph gets dense fast (the changed filter is the current answer to that, but it is a blunt one).
- "Save Graph" and "Save PNG" are two separate buttons doing near-identical things, tucked in the header.

TUI:

- the `?` help overlay largely repeats the footer hints, so it is not clear what it adds.
- filtering is fuzzy over the resource id only. You cannot filter by action ("show me just the deletes") even though action is the thing people scan for.
- the detail pane is mostly empty vertical space for a small plan, and the blast-radius list can run long with no obvious truncation on big ones.
- there is no way to see structure/topology, only a flat list. The dependency and blast-radius info is text in the detail pane, not something you can navigate.

## What good would look like

The brief is open: improve the UX of both surfaces. Two coherent front-ends that
feel like one tool would be the win, whether that means the web UI borrowing the
TUI's plan summary and action filtering, the TUI borrowing some sense of the
web's structure, or reconsidering the shared model underneath. Start from the
screenshots and the running app, not from these notes.
