# A terminal TUI for Ponto (bubbletea)

Date: 2026-07-08
Status: draft (approach approved, pending spec review)

## Why

Ponto's only front end today is the web UI: it generates the plan, serves an
interactive graph, and you open a browser. That's great for exploring a graph,
but for a quick "what's this plan going to do" over SSH, in a tmux pane, or
without a browser handy, it's heavy. This adds a terminal UI so you can review a
plan where you already are.

It's a launcher that opens into a plan explorer:

1. A small launch screen that shows the resolved config and lets you kick it off
   (or tweak the couple of things worth tweaking).
2. A plan explorer: a navigable, two-pane view of the plan, defaulting to what's
   changing, with per-resource detail and its connections.

Sub-project 2 of the CLI/TUI rework, sitting on the cobra CLI from #15. This
spec is the design; the build follows on approval.

## Flag

`-i`, `--tui` (with `--interactive` as an alias) selects TUI mode, alongside the
existing `--standalone` and `--gen-image`, and joins the mutually-exclusive mode
group. Default (no mode) still serves the web UI.

## Stack

- [bubbletea](https://github.com/charmbracelet/bubbletea) for the
  Model/Update/View loop.
- [bubbles](https://github.com/charmbracelet/bubbles) for the ready-made
  components: `list`, `viewport`, `spinner`, `help`, `key`.
- [lipgloss](https://github.com/charmbracelet/lipgloss) for layout and styling.

New code lives in a `tui` package so `main.go` just wires the flag to
`tui.Run(...)`. The web UI, graph, and image paths are untouched.

## Data

The TUI does not start a server. It reuses the existing pipeline: build the
`ponto` value from the flags exactly as serve/standalone/image do, call
`generateAssets()` (which populates `r.Plan`, `r.RSO`, `r.Map`, `r.Graph`), then
render those Go structs in the terminal. The change action per resource, the
module/file nesting, and the dependency edges are all already in `r.Map` /
`r.Graph`, which is what the web UI consumes too, so the terminal and the browser
show the same model.

## Screens

### 1. Launch

A single screen shown before the plan is generated:

- Shows the resolved config: working dir, plan source (a provided
  `--plan-json-path` / `--plan-path`, TFC, or "will run terraform plan"), the
  terraform binary that was resolved, workspace.
- One key to start (`enter`), one to quit (`q` / `ctrl-c`).
- If a plan is going to be generated (no pre-supplied plan), starting shows a
  spinner with the same progress lines the CLI logs ("Initializing
  Terraform...", etc.) until assets are ready.

Keeping the launcher minimal on purpose: it confirms what's about to happen and
gives a place for the spinner, rather than being a full config editor. Editing
config is what flags are for.

### 2. Explorer

Two panes, list on the left, detail on the right, with a header and a help/key
bar along the bottom (the standard bubbles layout):

- **Header**: the plan summary counts, e.g. `12 to create · 3 to change · 1 to
  destroy` (omitting zero categories), plus which filter is active.
- **List (left)**: resources grouped by their module / file, each row coloured
  by action (create/update/delete/replace/no-op) to match the web UI's legend.
  Defaults to changed resources plus the resources connected to them (the same
  changed-by-default rule as the web graph, #6); a key toggles showing
  everything.
- **Detail (right)**: for the selected resource, a scrollable viewport showing
  its type, address, action, and its dependencies and dependents (the blast
  radius), drawn from `r.Graph`. This is the "what does this change touch"
  context that matters for an approve/deny read.

**Keys** (shown in the help bar): `up`/`down` or `j`/`k` to move, `enter` to
focus the detail pane, `tab` to switch panes, `/` to filter by name, `a` to
toggle changed-only vs all, `?` for full help, `q` / `ctrl-c` to quit. All via
the `bubbles/key` and `bubbles/help` components so the bindings are declared
once and rendered in the help bar automatically.

## Scope

- Add bubbletea / bubbles / lipgloss deps.
- New `tui` package: launch model, explorer model (list + detail + header +
  help), and a `Run` entry point that takes the built `ponto` value.
- `main.go`: add the `-i/--tui` flag to the mode group and dispatch to
  `tui.Run` when set.
- No web UI, graph, image, or server changes.

## Verification

- `ponto --tui` on `example/random-test` launches, generates the plan, and shows
  the explorer; arrow/filter/toggle keys work; the detail pane shows a selected
  resource's dependencies; `q` quits cleanly and restores the terminal.
- `ponto --tui --plan-json-path plan.json` skips terraform and goes straight to
  the explorer.
- `--tui` with `--standalone` or `--gen-image` errors (mutually exclusive).
- Builds/vets/gofmt clean; the binary still builds in the Docker image (the TUI
  is just more Go, no new system deps).

## Out of scope (later, if wanted)

- Editing/among-plan actions (approve, apply) from the TUI: this is a viewer.
- A full ASCII rendering of the dependency graph: the detail pane lists
  connections rather than drawing the graph. A drawn graph could come later.
- Mouse support and themes beyond a sensible default.
