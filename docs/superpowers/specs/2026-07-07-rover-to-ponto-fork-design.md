# Rover → Ponto fork: apply PRs + rebrand

Date: 2026-07-07
Status: approved

## Context

Ponto is a fork of the abandoned [im2nguyen/rover](https://github.com/im2nguyen/rover)
Terraform visualizer. This spec covers the first two phases of taking the fork
forward: applying the two open upstream PRs, then renaming the project from Rover
to Ponto while preserving Rover's original copyright and license.

The codebase is a single Go `package main` (flat, no internal module imports) plus
a Vue UI under `ui/`. Because there are no internal imports of the module path, the
Go module rename is confined to `go.mod`.

Phase 3 (triaging upstream issues and building Trigger.dev cloud plan/apply display
use-cases) is explicitly out of scope for this spec and will get its own cycles.

## Phase 1: apply the two open upstream PRs

### PR #138 (Go 1.23) — apply verbatim

A clean, recent PR based on current `main`. Apply as-is:

- `go.mod`: `go 1.21` → `go 1.23`.
- `Dockerfile`: `golang:1.21` → `golang:1.23`.
- Migrate `io/ioutil` → `io` / `os` in `main.go` and `rso.go`
  (`ioutil.TempDir`→`os.MkdirTemp`, `ioutil.ReadAll`→`io.ReadAll`).
- gofmt whitespace cleanups in `graph.go`, `map.go`, `server.go`, and the
  `type (...)` grouping in `map.go`.
- Real bugfix in `rso.go`: `json:"Key,omitempty""` → `json:"Key,omitempty"`
  (stray trailing quote in the struct tag).

### PR #69 (Docker/CI) — re-implement its ideas on current versions

PR #69 is ~4 years old, based on a very stale base. It carries good ideas but,
applied verbatim after #138, it downgrades Go to 1.17, pins Node 16 / TF 1.1.2,
and re-introduces `io/ioutil`, undoing #138. So we take the ideas, not the diff:

- New multi-stage `Dockerfile`: node UI build → Go 1.23 build → `scratch` final
  image containing the terraform binary, CA certs, and the app binary.
- `docker-bake.hcl` with buildx targets: standard image + slim (UPX-compressed)
  image, multi-platform (`linux/amd64`, `linux/arm64`).
- Rework `.github/workflows/publishDockerImage.yml` to build with buildx-bake and
  push to `ghcr.io/1stvamp/ponto` (uses the built-in `GITHUB_TOKEN`; no extra
  registry secrets needed).
- Add a UI build-test workflow (npm build on `ui/**` changes).
- Tidy `.github/workflows/release.yml` and `.goreleaser.yml` (fetch-depth,
  formatting) consistent with the new binary name.
- Node stays at 16: the UI build relies on `NODE_OPTIONS=--openssl-legacy-provider`
  from the old vue-cli/webpack toolchain. A UI toolchain upgrade is phase 3.
- TF version: a recent stable default, overridable via a bake variable.

**Explicitly NOT adopted from #69:** its `screenshot.go` rewrite. That change
downgrades the vector SVG export to a raster screenshot and leaves a
`<-downloadComplete` receive on a channel that nothing closes (a deadlock). We keep
the existing download-based screenshot implementation, renamed only.

## Phase 2: full rename Rover → Ponto

Rename everything, including internal Go symbols.

- `go.mod`: `module rover` → `module ponto`.
- Internal Go symbols: `type rover struct` → `type ponto`, all `(r *rover)`
  receivers, and `rover{...}` construction sites.
- Functional string defaults in `main.go`:
  - `-name` flag default `"rover"` → `"ponto"`.
  - `-zipFileName` flag default `"rover"` → `"ponto"` (output becomes `ponto.zip`).
  - temp dir / plan-file prefixes (`"rover"`, `"roverplan"`) → ponto equivalents.
  - log lines ("Starting Rover..." etc.) → "Starting Ponto...".
- Output artifacts: `rover.svg` → `ponto.svg` (Go side and `Graph.vue` `saveAs`),
  `rover.zip` → `ponto.zip`.
- Binary name `rover` → `ponto` across `Dockerfile`, `docker-bake.hcl`,
  `.goreleaser.yml`, and workflows.
- Docker image reference `im2nguyen/rover` → `ghcr.io/1stvamp/ponto`.
- UI branding: `ui/src/App.vue` title, `ui/src/components/MainNav.vue`,
  `ui/src/components/ResourceDetail.vue`, `ui/src/components/Graph/Graph.vue`,
  `ui/package.json` name, UI README and `ui/public` HTML title.
- `README.md`: rewrite for Ponto, with a fork-attribution line crediting
  im2nguyen/rover.
- `example/nested-test/nested-module/main.tf`: repoint its
  `source = "git::https://github.com/im2nguyen/rover.git//..."` to
  `git::https://github.com/1stvamp/ponto.git//...` so the example uses the fork's
  own module. (Depends on the fork being pushed with that path present, which it
  is.)

### Deliberately left as-is

- **`LICENSE`**: untouched. Preserves the original MIT text and
  `Copyright (c) 2021 rover` notice. Attribution to the original Rover project
  lives in the README, not the license.

## Verification

- `go build ./...` and `go vet ./...` pass.
- `gofmt -l .` reports no files.
- `docker buildx bake` builds the standard image successfully.
- `cd ui && npm run build` succeeds.
- Smoke run against `example/random-test` renders the visualization.
- No `rover` / `im2nguyen` strings remain anywhere except: the `LICENSE` notice
  and the README attribution line.

## Out of scope (phase 3, future cycles)

- Triaging and fixing upstream rover issues.
- Trigger.dev cloud terraform plan/apply display use-cases.
- UI toolchain upgrade (would unblock a newer Node in the Docker build).
