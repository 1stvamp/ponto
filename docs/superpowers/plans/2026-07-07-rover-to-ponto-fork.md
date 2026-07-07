# Rover → Ponto Fork Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Apply the two open upstream PRs (adapted), then fully rename the Rover Terraform visualizer to Ponto while preserving Rover's original MIT license.

**Architecture:** A single Go `package main` (flat, no internal module imports) plus a Vue 2 UI under `ui/` embedded into the binary. Work proceeds in sequence: modernize Go (PR #138), rename Go code, rename UI, rework Docker/CI on current tooling (PR #69's ideas), then docs + example + attribution.

**Tech Stack:** Go 1.23+, Vue 2 (vue-cli 4), Docker buildx + bake, goreleaser, GitHub Actions, chromedp/chromium (for `-genImage`).

## Global Constraints

- Go module name: `ponto`. Go version floor in `go.mod`: `go 1.23`.
- `LICENSE` stays byte-for-byte unchanged (preserves `Copyright (c) 2021 rover`).
- Node stays `node:20-alpine`; Terraform default `TF_VERSION=1.5.5` (both overridable via bake variables). These match the current tree, which is ahead of the PR diffs.
- Docker images publish to `ghcr.io/1stvamp/ponto` (standard) and `ghcr.io/1stvamp/ponto:slim` (UPX). Standard image is alpine-based and keeps `chromium` so `-genImage` works; the slim/scratch image omits chromium and therefore does not support `-genImage` (documented, not silently dropped).
- Default `-tfPath` is `/bin/terraform`; the terraform binary must live at `/bin/terraform` in every image.
- Commit with GitButler on branch `feat/ponto-fork`: `but commit feat/ponto-fork -a -m "<msg>"`. Never use `git commit`.
- Prose (README) uses no em/en dashes.
- After edits, "grep-clean" means: `grep -rn -i rover` over tracked source finds matches only in `LICENSE`, `README.md` (attribution), and `docs/superpowers/` (spec + this plan).

### Spec deltas (reality vs the approved spec)

The spec was written from the PR diffs; the live tree is ahead of them. Two corrections, already folded into this plan:
1. Node is `20` (not 16) and TF default is `1.5.5` (not 1.1.2). No downgrade.
2. The standard image cannot be `scratch` because `-genImage` needs chromium. Only the slim image is scratch. Both variants still ship.

---

### Task 1: Apply PR #138 — modernize Go (1.23, io migration, struct-tag bugfix)

**Files:**
- Modify: `go.mod:3`
- Modify: `main.go` (imports + `getPlan`)
- Modify: `rso.go` (imports + `ModuleLocation` tag + `PopulateModuleLocations`)
- Modify: `map.go` (type grouping) + whole-repo `gofmt`

**Interfaces:**
- Consumes: nothing (first task).
- Produces: a repo that builds on Go 1.23 with no `io/ioutil` usage. Later tasks assume `os.MkdirTemp` / `io.ReadAll` are already in place.

- [ ] **Step 1: Bump the Go version in `go.mod`**

`go.mod` line 3: change `go 1.21` to `go 1.23`. (The Dockerfile Go version is handled in Task 4's full rewrite, not here.)

- [ ] **Step 2: Migrate `main.go` off `io/ioutil`**

In `main.go` imports (around line 11), replace:

```go
	"io/fs"
	"io/ioutil"
```

with:

```go
	"io"
	"io/fs"
```

In `getPlan` (line ~207) change:

```go
	tmpDir, err := ioutil.TempDir("", "rover")
```

to:

```go
	tmpDir, err := os.MkdirTemp("", "rover")
```

In `getPlan` (line ~238) change:

```go
		planJson, err := ioutil.ReadAll(planJsonFile)
```

to:

```go
		planJson, err := io.ReadAll(planJsonFile)
```

(Keep the string literal `"rover"` here for now; Task 2 renames it.)

- [ ] **Step 3: Fix the `rso.go` struct tag and migrate off `ioutil`**

In `rso.go`, fix the stray double-quote in `ModuleLocation` (line ~48):

```go
	Key    string `json:"Key,omitempty"`
```

Reorder the `rso.go` import block (lines ~3-9) so stdlib and third-party are separated and `ioutil` is gone:

```go
import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-config-inspect/tfconfig"
	tfjson "github.com/hashicorp/terraform-json"
)
```

In `PopulateModuleLocations` (line ~67) change:

```go
	byteValue, _ := ioutil.ReadAll(jsonFile)
```

to:

```go
	byteValue, _ := io.ReadAll(jsonFile)
```

- [ ] **Step 4: Group the type declarations in `map.go`**

In `map.go` (lines ~14-15) replace:

```go
type Action string
type ResourceType string
```

with:

```go
type (
	Action       string
	ResourceType string
)
```

- [ ] **Step 5: gofmt the whole repo**

Run: `gofmt -w .`
This applies PR #138's whitespace cleanups across `graph.go`, `map.go`, `server.go`, etc.

- [ ] **Step 6: Verify build, vet, and formatting**

Run: `go build ./... && go vet ./... && gofmt -l .`
Expected: build and vet succeed with no output; `gofmt -l .` prints nothing.

- [ ] **Step 7: Commit**

```bash
but commit feat/ponto-fork -a -m "chore(go): apply PR #138 - Go 1.23, io/os migration, struct-tag fix

Bumps go.mod to 1.23, replaces deprecated io/ioutil with io/os, fixes a
stray quote in the ModuleLocation JSON tag, and gofmt-cleans the tree.
Adapted from im2nguyen/rover#138."
```

---

### Task 2: Rename Rover → Ponto in Go code

**Files:**
- Modify: `go.mod:1`
- Modify: `main.go` (struct, receivers, flag defaults, log/version strings, temp prefixes)
- Modify: `map.go`, `rso.go`, `graph.go`, `zip.go`, `server.go` (receivers + user-facing strings)
- Modify: `screenshot.go` (output filename)

**Interfaces:**
- Consumes: Task 1's modernized Go.
- Produces: Go type `ponto` with receiver `(r *ponto)` / `(ro *ponto)`; output artifacts `ponto.svg` and default zip base `ponto`. The UI (Task 3) must match `ponto.svg`.

- [ ] **Step 1: Rename the module**

`go.mod` line 1: change `module rover` to `module ponto`.

- [ ] **Step 2: Rename the struct, receivers, and construction site**

Rename the type and every receiver across the Go files. Exact sites:
- `main.go:47` `type rover struct {` → `type ponto struct {`
- `main.go:120` `r := rover{` → `r := ponto{`
- `main.go:180` `func (r *rover) generateAssets()` → `func (r *ponto) generateAssets()`
- `main.go:206` `func (r *rover) getPlan()` → `func (r *ponto) getPlan()`
- `map.go:86,303,319` `func (r *rover) ...` → `func (r *ponto) ...`
- `rso.go:56,79,156,265` `func (r *rover) ...` → `func (r *ponto) ...`
- `graph.go:60,91,216,248,335` `func (r *rover) ...` → `func (r *ponto) ...`
- `zip.go:15` `func (r *rover) generateZip(...)` → `func (r *ponto) generateZip(...)`
- `server.go:15` `func (ro *rover) startServer(...)` → `func (ro *ponto) startServer(...)`

Command to do it in one pass, then eyeball the diff:

```bash
sed -i 's/\*rover)/*ponto)/g; s/type rover struct/type ponto struct/; s/r := rover{/r := ponto{/' main.go map.go rso.go graph.go zip.go server.go
```

- [ ] **Step 3: Rename user-facing and functional strings in `main.go`**

- Line 74: `flag.StringVar(&name, "name", "rover", ...)` → default `"ponto"`.
- Line 75: `flag.StringVar(&zipFileName, "zipFileName", "rover", ...)` → default `"ponto"`.
- Line 76: help text `"IP and port for Rover server"` → `"IP and port for Ponto server"`.
- Line 93: `fmt.Printf("Rover v%s\n", VERSION)` → `fmt.Printf("Ponto v%s\n", VERSION)`.
- Line 97: `log.Println("Starting Rover...")` → `log.Println("Starting Ponto...")`.
- Line 207: `os.MkdirTemp("", "rover")` → `os.MkdirTemp("", "ponto")`.
- Line 295 comment: `// If latest run is not actionable, rover will create new run` → `... ponto will create new run`.
- Line 380: `"%s/%s-%v", tmpDir, "roverplan", ...` → `"roverplan"` becomes `"pontoplan"`.

- [ ] **Step 4: Rename remaining strings in `server.go`, `map.go`, `screenshot.go`**

- `server.go:64` `log.Printf("Rover is running on %s", ipPort)` → `"Ponto is running on %s"`.
- `map.go:331` `Path: "Rover Visualization",` → `Path: "Ponto Visualization",`.
- `screenshot.go:59` `moveFile(..., "./rover.svg")` → `"./ponto.svg"`.

- [ ] **Step 5: Verify build, vet, formatting, and grep-clean of Go**

Run: `go build ./... && go vet ./... && gofmt -l .`
Expected: success, no output.
Run: `grep -rn -i rover *.go go.mod`
Expected: no matches.

- [ ] **Step 6: Commit**

```bash
but commit feat/ponto-fork -a -m "refactor: rename Rover to Ponto in Go code

Renames the module, the rover struct and all receivers, flag defaults,
log/version strings, temp-file prefixes, and the SVG output filename."
```

---

### Task 3: Rename Rover → Ponto in the Vue UI

**Files:**
- Modify: `ui/src/App.vue:61` (document title)
- Modify: `ui/src/components/MainNav.vue:4-5` (header link + heading)
- Modify: `ui/src/components/Graph/Graph.vue:487` (saveAs filename)
- Modify: `ui/src/components/ResourceDetail.vue:54,180,205` (internal sentinel)
- Modify: `ui/package.json:2` (name)
- Modify: `ui/README.md:1` (title)

**Interfaces:**
- Consumes: Task 2's `ponto.svg` output name (the UI `saveAs` must match).
- Produces: no code interface; UI branding only.

- [ ] **Step 1: App title**

`ui/src/App.vue:61`: `title: "Rover | Terraform Visualization",` → `title: "Ponto | Terraform Visualization",`.

- [ ] **Step 2: Nav header link and heading**

`ui/src/components/MainNav.vue`:
- Line 4: `href="https://github.com/im2nguyen/rover"` → `href="https://github.com/1stvamp/ponto"`.
- Line 5: `<h2>Rover - Terraform Visualizer</h2>` → `<h2>Ponto - Terraform Visualizer</h2>`.

- [ ] **Step 3: Graph SVG download filename**

`ui/src/components/Graph/Graph.vue:487`: `saveAs(blob, "rover.svg");` → `saveAs(blob, "ponto.svg");`.

- [ ] **Step 4: Rename the internal `isChild` sentinel (all three occurrences)**

This string is UI-internal (set in `getResourceConfig`, compared in the template; the Go backend never emits it). Rename consistently:

```bash
sed -i "s/rover-for-each-child-resource-true/ponto-for-each-child-resource-true/g" ui/src/components/ResourceDetail.vue
```

This covers lines 54 (compare), 180 (set), and 205 (commented). All must change together to keep the match intact.

- [ ] **Step 5: UI package name and README title**

- `ui/package.json:2`: `"name": "ui",` → `"name": "ponto-ui",`.
- `ui/README.md:1`: `# Mercator UI` → `# Ponto UI`.

- [ ] **Step 6: Verify the UI builds**

Run: `cd ui && NODE_OPTIONS='--openssl-legacy-provider' npm install && NODE_OPTIONS='--openssl-legacy-provider' npm run build && cd ..`
Expected: build completes, `ui/dist` regenerated, no errors.
Run: `grep -rn -i rover ui/src ui/package.json ui/README.md`
Expected: no matches.

- [ ] **Step 7: Commit**

```bash
but commit feat/ponto-fork -a -m "refactor(ui): rename Rover to Ponto branding

Updates the document title, nav header/link, SVG download name, the
internal for-each-child sentinel, and UI package/readme naming."
```

---

### Task 4: Rework Docker + CI (PR #69's ideas, on current tooling)

**Files:**
- Replace: `Dockerfile`
- Create: `docker-bake.hcl`
- Replace: `.github/workflows/publishDockerImage.yml`
- Create: `.github/workflows/test.yml`
- Modify: `.github/workflows/release.yml:22-24,39`
- Modify: `.dockerignore:6`
- Modify: `.gitignore:1,3` (+ add `.dist/`)

**Interfaces:**
- Consumes: Go module `ponto` (Task 2) so goreleaser's `{{ .ProjectName }}` resolves to `ponto`; UI build from Task 3.
- Produces: bake targets `image-local`, `image-slim`, `image-all`, `image-slim-all`; both images ship terraform at `/bin/terraform` and the `ponto` binary.

- [ ] **Step 1: Rewrite the `Dockerfile`**

Two final stages: `standard` (alpine + chromium, keeps `-genImage`) and `slim` (scratch + UPX, no chromium). Write `Dockerfile`:

```dockerfile
# syntax=docker/dockerfile:1

ARG NODE_VERSION=20
ARG GO_VERSION=1.23
ARG TF_VERSION=1.5.5

# Build UI
FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-alpine AS ui
WORKDIR /src
COPY ./ui/package*.json ./
COPY ./ui/babel.config.js ./
RUN npm set progress=false && npm config set depth 0 && npm install
COPY ./ui/public ./public
COPY ./ui/src ./src
RUN NODE_OPTIONS='--openssl-legacy-provider' npm run build

# Build the ponto binary
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
ENV CGO_ENABLED=0
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /src/dist ./ui/dist
ARG TARGETOS TARGETARCH
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags "-s -w" -o /out/ponto .

# UPX-compress the binary for the slim image
FROM build AS build-slim
RUN apk add --no-cache upx && upx -q --best /out/ponto

# Standard image: terraform base + chromium (supports -genImage)
FROM hashicorp/terraform:${TF_VERSION} AS standard
RUN apk add --no-cache chromium ca-certificates
COPY --from=build /out/ponto /bin/ponto
WORKDIR /src
ENTRYPOINT ["/bin/ponto"]

# Slim image: scratch + UPX'd terraform and ponto (no chromium; no -genImage)
FROM hashicorp/terraform:${TF_VERSION} AS tf-slim
RUN apk add --no-cache upx && cp /bin/terraform /terraform && upx -q --best /terraform

FROM scratch AS slim
COPY --from=standard /etc/ssl/certs/ /etc/ssl/certs/
COPY --from=tf-slim /terraform /bin/terraform
COPY --from=build-slim /out/ponto /bin/ponto
WORKDIR /src
ENTRYPOINT ["/bin/ponto"]
```

- [ ] **Step 2: Create `docker-bake.hcl`**

```hcl
variable "GO_VERSION" {
  default = "1.23"
}
variable "NODE_VERSION" {
  default = "20"
}
variable "TF_VERSION" {
  default = "1.5.5"
}
variable "REPO" {
  default = "ghcr.io/1stvamp/ponto"
}
variable "VERSION" {
  default = "edge"
}

target "_common" {
  args = {
    GO_VERSION   = GO_VERSION
    NODE_VERSION = NODE_VERSION
    TF_VERSION   = TF_VERSION
  }
  labels = {
    "org.opencontainers.image.title"       = "ponto"
    "org.opencontainers.image.description"  = "Interactive Terraform visualization. State and configuration explorer."
    "org.opencontainers.image.licenses"    = "MIT"
    "org.opencontainers.image.source"      = "https://github.com/1stvamp/ponto"
    "org.opencontainers.image.version"     = "${VERSION}"
  }
}

group "default" {
  targets = ["image-local"]
}

target "image-local" {
  inherits = ["_common"]
  target   = "standard"
  tags     = ["${REPO}:latest", "${REPO}:${VERSION}"]
  output   = ["type=docker"]
}

target "image-slim" {
  inherits = ["_common"]
  target   = "slim"
  tags     = ["${REPO}:slim", "${REPO}:slim-${VERSION}"]
  output   = ["type=docker"]
}

target "image-all" {
  inherits  = ["image-local"]
  platforms = ["linux/amd64", "linux/arm64"]
  output    = ["type=image"]
}

target "image-slim-all" {
  inherits  = ["image-slim"]
  platforms = ["linux/amd64", "linux/arm64"]
  output    = ["type=image"]
}
```

- [ ] **Step 3: Rewrite `publishDockerImage.yml` to build with bake and push to ghcr**

```yaml
name: PublishDockerImage

on:
  push:
    tags:
      - "v*"

jobs:
  push_to_registry:
    name: Build and push image to GHCR
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write
    steps:
      - name: Check out the repo
        uses: actions/checkout@v4

      - name: Set up QEMU
        uses: docker/setup-qemu-action@v3

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Log in to GHCR
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Derive version tag
        id: version
        run: echo "version=${GITHUB_REF_NAME}" >> "$GITHUB_OUTPUT"

      - name: Build and push (standard + slim, multi-platform)
        uses: docker/bake-action@v5
        env:
          VERSION: ${{ steps.version.outputs.version }}
        with:
          files: docker-bake.hcl
          targets: |
            image-all
            image-slim-all
          push: true
          set: |
            *.cache-from=type=gha
            *.cache-to=type=gha,mode=max
```

- [ ] **Step 4: Add the UI build-test workflow `test.yml`**

```yaml
name: UI Build Test

on:
  push:
    paths:
      - "ui/**"
  pull_request:
    paths:
      - "ui/**"

jobs:
  ui-build-test:
    name: Test npm build
    runs-on: ubuntu-latest
    steps:
      - name: Check out the repo
        uses: actions/checkout@v4

      - name: Set up Node
        uses: actions/setup-node@v4
        with:
          node-version: "20"

      - name: Install and build
        working-directory: ui
        env:
          NODE_OPTIONS: "--openssl-legacy-provider"
        run: |
          npm install
          npm run build
```

- [ ] **Step 5: Tidy `release.yml`**

- Replace the checkout + separate unshallow (lines ~21-24) with a full-history checkout:

```yaml
      - name: Checkout
        uses: actions/checkout@v4
        with:
          fetch-depth: 0
```

- Line ~39: `args: release --rm-dist` → `args: release --clean` (`--rm-dist` was removed in goreleaser v2).

- [ ] **Step 6: Update `.dockerignore` and `.gitignore`**

- `.dockerignore:6`: `rover` → `ponto`.
- `.gitignore`: line 1 `build/rover` → `build/ponto`; line 3 `rover.zip` → `ponto.zip`; add a line `.dist/`.

- [ ] **Step 7: Verify the standard image builds and runs**

Run: `docker buildx bake image-local`
Expected: builds `ghcr.io/1stvamp/ponto:latest` (loaded into docker) with no errors.
Run: `docker run --rm ghcr.io/1stvamp/ponto:latest -version`
Expected: prints `Ponto v...`.
Run: `grep -rn -i rover Dockerfile docker-bake.hcl .github/workflows .dockerignore .gitignore`
Expected: no matches.

- [ ] **Step 8: Commit**

```bash
but commit feat/ponto-fork -a -m "build: rework Docker and CI (buildx bake, ghcr, slim UPX image)

Multi-stage Dockerfile with an alpine standard image (keeps chromium for
-genImage) and a scratch+UPX slim image. Adds docker-bake.hcl and a UI
build-test workflow; publishes to ghcr.io/1stvamp/ponto. Ideas adapted
from im2nguyen/rover#69, updated to current Go/Node/TF and GHCR."
```

---

### Task 5: README rewrite, example source, attribution, final sweep

**Files:**
- Replace: `README.md`
- Modify: `example/nested-test/nested-module/main.tf:2`

**Interfaces:**
- Consumes: image name `ghcr.io/1stvamp/ponto` (Task 4), binary `ponto`, artifacts `ponto.zip`/`ponto.svg`.
- Produces: final grep-clean state.

- [ ] **Step 1: Repoint the example module source**

`example/nested-test/nested-module/main.tf:2`:

```hcl
  source = "git::https://github.com/1stvamp/ponto.git//example/random-test/random-name"
```

- [ ] **Step 2: Rewrite `README.md` for Ponto**

Replace the whole file. Keep the structure and commands from the original but rename Rover→Ponto, `im2nguyen/rover`→`ghcr.io/1stvamp/ponto`, `rover`→`ponto` binary, `rover.zip`→`ponto.zip`, and update the manual-build Go floor to 1.23. Add a fork-attribution line near the top. Use no em/en dashes. Skeleton (fill commands verbatim from the current README, renamed):

```markdown
# Ponto - Terraform Visualizer

Ponto is a [Terraform](http://terraform.io/) visualizer. It is a fork of the
excellent but abandoned [Rover](https://github.com/im2nguyen/rover) by Nguyen
Nguyen and contributors, released under the MIT license (see `LICENSE`).

In order to do this, Ponto:

1. generates a `plan` file and parses the configuration in the root directory or uses a provided plan.
1. parses the `plan` and configuration files to generate three items: the resource overview (`rso`), the resource map (`map`), and the resource graph (`graph`).
1. consumes the `rso`, `map`, and `graph` to generate an interactive configuration and state visualization hosted on `0.0.0.0:9000`.

## Quickstart

Run the following in any Terraform workspace to generate a visualization:

```shell
docker run --rm -it -p 9000:9000 -v "$(pwd):/src" ghcr.io/1stvamp/ponto
```

Once Ponto runs on `0.0.0.0:9000`, navigate to it to find the visualization.

<!-- ... carry over the remaining sections (plan JSON, standalone zip -> ponto.zip,
env vars, tfbackend/tfvars, genImage, installation, manual build with Go 1.23+,
basic usage) from the original README, renamed Rover -> Ponto and the image ref
-> ghcr.io/1stvamp/ponto ... -->
```

The Releases-page links (`v0.3.2` URLs) point at the upstream rover release assets and no longer apply to the fork. Replace that "Download released binary" subsection with a line pointing at the fork's own releases: `https://github.com/1stvamp/ponto/releases` (no version-pinned rover URLs).

- [ ] **Step 3: Final grep sweep**

Run: `grep -rn -i rover . --include=*.go --include=*.vue --include=*.js --include=*.json --include=*.yml --include=*.yaml --include=Dockerfile --include=*.hcl --include=*.tf --include=.dockerignore --include=.gitignore README.md | grep -v node_modules | grep -v /dist/ | grep -v package-lock.json | grep -v docs/superpowers`
Expected: only the `README.md` attribution line (the word "Rover"/"rover" inside the attribution sentence and the upstream link). No `im2nguyen/rover` image refs, no binary/artifact `rover` names.
Run: `grep -c rover LICENSE`
Expected: `1` (the preserved `Copyright (c) 2021 rover`).

- [ ] **Step 4: Full verification pass**

Run: `go build ./... && go vet ./... && gofmt -l .`
Expected: success, no output.
Run: `docker buildx bake image-local && docker run --rm -v "$(pwd)/example/random-test:/src" ghcr.io/1stvamp/ponto:latest -standalone true` then confirm a `ponto.zip` is produced in the mounted dir.
Expected: run completes, `ponto.zip` created.

- [ ] **Step 5: Commit**

```bash
but commit feat/ponto-fork -a -m "docs: rewrite README for Ponto, repoint example, add attribution

Rebrands the README to Ponto with ghcr.io/1stvamp/ponto commands, points
the nested-module example at the fork, and credits the original Rover
project. LICENSE left unchanged."
```

---

## Self-review notes

- Spec coverage: PR #138 (Task 1), PR #69 ideas (Task 4), full Go rename (Task 2), UI rename (Task 3), README + example + attribution (Task 5), LICENSE untouched (Global Constraints + Task 5 verify). All spec sections mapped.
- The `screenshot.go` deadlock rewrite from #69 is explicitly NOT applied (only its filename is renamed in Task 2) — matches the spec.
- Deviations from spec (node 20, standard image not scratch) are documented under "Spec deltas".
