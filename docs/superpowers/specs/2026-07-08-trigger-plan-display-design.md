# Phase 3: displaying Terraform plans in Trigger.dev workflows

Date: 2026-07-08
Status: draft (static-image part ready to implement; hosted-view part is a future plan)

## Context

The whole point of Ponto is to show a Terraform plan to someone as part of
Trigger.dev's cloud plan/apply workflows. The plan JSON comes from Trigger.dev's
own terraform/tofu run, which already holds the credentials and the state
access. Ponto only has to render it, so Ponto never needs to run `terraform
init`/`plan` itself and never needs credentials.

Two ways to deliver that, and they're worth doing in order rather than all at
once:

1. Now: a static graph image attached to the run output. No interactivity, no
   hosting, no credentials in Ponto.
2. Later: a temporary hosted interactive view, served off a simple throwaway
   static host (GitHub Pages / Vercel / Fly) and linked from the run.

## Part 1 (now): static image in the run output

You feed a pre-generated plan JSON in, you get one graph image file out, and the
process exits. That image gets attached to the run's output or logs. That's the
whole feature.

- Input: plan JSON via `-planJSONPath` (Trigger.dev generates it with `terraform
  show -json <planfile>`). No init, no plan, no provider downloads, no creds.
- Output: a graph image written to a known path, exit 0. SVG is the native
  export; PNG is a nice-to-have (chromium can rasterise it).
- Runs in the standard Docker image, which ships chromium (the capture needs a
  browser). The slim image can't do this, and that's already documented.

### What's broken today

Two upstream rover issues get in the way:

- **#105, the tiny SVG**: `Graph.vue` exports with `cy.svg({scale: 0.1, full:
  true})`. `scale: 0.1` renders the graph at 10%, so you get a tiny image.
  Export at `scale: 1` instead (full resolution). Worth exposing scale later so
  the capture path can ask for a bigger render.
- **#102, `-genImage` spins a server**: the capture flow starts the HTTP server,
  drives chromium to click "Save Graph", moves the downloaded file, then shuts
  the server down. It does exit, but it's undocumented and a bit fragile. For
  the Trigger.dev path `-genImage -planJSONPath plan.json` has to work end to
  end: a transient server plus chromium is fine, but it must not hang, must exit
  non-zero when the capture fails (rather than logging and carrying on), and the
  output filename must be controllable.

### Work

1. `ui/src/components/Graph/Graph.vue`: change the SVG export scale from `0.1` to
   `1`, then rebuild `ui/dist` (it's embedded in the binary).
2. Solidify `-genImage` + `-planJSONPath` in `main.go` / `screenshot.go`: render
   from the provided plan with no init/plan, write the image, exit cleanly, fail
   loudly on capture errors, and let the output name follow the existing name
   flags.
3. Prove it in the standard Docker image: `docker run --rm -v "$PWD:/src"
   ghcr.io/1stvamp/ponto -genImage -planJSONPath plan.json` gives you a full-size
   `ponto.svg`.
4. Add the pipeline recipe to the README (feed plan JSON, get an image, attach to
   the run).

### Out of scope here

Interactivity, hosting, multiple plans. Perfect PNG fidelity beyond what a
chromium screenshot gives you. Large-plan performance (#140) doesn't matter for a
static image (a screenshot of a huge graph is still fine); it matters for the
interactive view.

## Part 2 (later, planned): temporary hosted interactive view

Serve the full interactive Ponto UI for a given run off a short-lived static
host, and link that URL from the Trigger.dev run, so a reviewer can actually
explore the plan instead of squinting at a picture.

The nice thing here is there's almost no new rendering code. Ponto's standalone
mode (`-standalone`) already emits a self-contained `ponto.zip` (the HTML plus JS
with the `map`/`rso`/`graph` data baked in as static JS), and that unzipped asset
is a static site you can drop on any static host. So the hosted view is a deploy
step bolted onto standalone mode.

### Plan sketch

1. Trigger.dev task: plan JSON, then `terraform show -json`, then Ponto
   `-standalone`, giving you `ponto.zip` (self-contained, no server needed at
   view time).
2. Unzip and deploy the static asset to a throwaway target:
   - GitHub Pages: simplest and free. Publish per-run under a path on a
     `gh-pages` branch or a dedicated artifacts repo, and expire old paths with a
     scheduled cleanup.
   - Vercel: `vercel deploy --prebuilt` of the static dir gives a per-run preview
     URL, good previews, easy retention.
   - Fly: only if you actually need a running server. Heavier, so not the default
     for a static asset.
3. Surface the resulting URL in the Trigger.dev run output.

### Open questions (for when this gets picked up)

- Which host: GitHub Pages (free, simple), Vercel (best previews), or Fly (only
  if server-side is needed).
- URL access: a public preview link, or something gated.
- Retention: how long the per-run deployment lives, and how it gets torn down.
- Large-plan performance (#140): the interactive view has to cope with big cloud
  plans (a 17MB JSON grinds the browser to a halt today), so this probably needs
  graph virtualisation, node/edge limits, or progressive rendering. This is the
  main risk in part 2 and should get its own investigation.

### Status

Part 2 is a future project. It gets its own spec and plan (and a decision on the
host plus the #140 perf work) when it's prioritised. This section is the
placeholder plan asked for during phase-3 planning.

## Related upstream issues

- Part 1: #105 (tiny SVG), #102 (genImage headless), #115/#119 (decouple plan
  generation / CI), #56 (version string).
- Part 2: #44 (hosted/SaaS), #66 (visualise state), #140 (large-plan perf),
  #34/#89 (TFC / custom backends).
