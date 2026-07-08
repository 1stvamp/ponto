# Filter the graph to changed resources by default (#6)

Date: 2026-07-08
Status: draft (approach approved, pending spec review)

## Why

Big plans hang the browser. Batching the cytoscape adds (#11) took a ~3000-node
render from ~162s to ~57s, but that's still too slow and a real 17MB cloud plan
is much larger again. The layout and the sheer number of rendered nodes are what
cost now, so the only thing that really helps is rendering fewer nodes.

For a plan/apply review you don't care about the whole world, you care about
what's changing and what those changes touch. So by default we render the
changes plus the resources connected to them, and let you opt back into the full
graph. That's both the perf fix and the right default for reviewing a plan.

This is a client-side filter. The graph data (nodes, edges, per-node change) is
already fetched once and sits in the browser, so filtering is just a re-render.
No server change, no data-format change.

## The data we've got

- Every resource node carries `data.change`: one of `create`, `update`,
  `delete`, `replace`, `read`, `no-op`. Container nodes (type/file/module) and
  variables/outputs/locals carry no change.
- Nodes nest via `data.parent` (resource name to resource type to file to
  module).
- Edges carry `data.source` and `data.target`, and the convention is **source
  depends on target**. So the things a resource depends on are the targets of its
  outgoing edges; the things that depend on it (its blast radius) are the sources
  of its incoming edges.

## The default view (changes only)

Render this set:

1. **Changed resources**: `change` in `create`, `update`, `delete`, `replace`.
   (`no-op` and `read` count as unchanged.)
2. **Full downstream blast radius**: every resource that transitively depends on
   a changed resource, any number of hops. Walk edges backwards (target to
   source) from the changed set to a fixpoint. This is the "what could this
   change affect" context that drives an approve/deny decision, so it's included
   even though those resources are unchanged.
3. **1-hop upstream dependencies**: the direct dependencies of each changed
   resource (targets of its outgoing edges), one hop only. Enough to see what a
   change rests on without dragging in the whole upstream tree.
4. **Ancestors**: the parent chain (type/file/module) of everything included
   above, so the nesting still renders.

Edges: keep an edge only when both ends are in the visible set.

## Toggle

A "Show unchanged" control in the top nav, default off. Turning it on re-runs the
render over the full graph. Small plans and deliberate exploration still get the
whole thing.

## Guard

When "Show unchanged" is on and the full graph is large (> 500 nodes), show a
one-line dismissable warning that the full render may be slow and to consider the
static image (`-genImage`). It doesn't block the render, it just warns.

## No-change fallback

If a plan has no changed resources at all, the filtered set would be empty, so
fall back to rendering the full graph with a short note saying there's nothing to
filter to.

## Scope

Client-side only:

- `ui/src/components/Graph/Graph.vue`: the filter + traversal, and honouring the
  toggle flag in `renderGraph`.
- `ui/src/components/MainNav.vue`: the "Show unchanged" control and the guard
  message.
- `ui/src/App.vue`: wire the flag between the two.

Then rebuild the embedded `ui/dist`. It extends `renderGraph`, so it stacks on
the batching branch (#11).

## Verification

The ~3000-node stress plan is all creates, so everything counts as changed. That
exercises the guard and the worst case, but it doesn't show the filter earning
its keep. So build a second plan with a big mostly-no-op state and a handful of
changes with a dependency chain, and check:

- the default view drops to the changes plus their blast radius plus 1-hop deps,
  and the render time drops with it,
- a downstream dependent that is itself unchanged still shows up,
- the toggle brings back the full graph,
- the guard warning fires above the threshold,
- a no-change plan falls back to the full graph with the note.

## Out of scope

Server-side or cheaper layout, canvas virtualisation. If the filtered set is
still huge on a pathological plan (a change near the root of a wide dependency
tree pulls in a big blast radius), that's the case for the deeper work, and #6
stays open for it.
