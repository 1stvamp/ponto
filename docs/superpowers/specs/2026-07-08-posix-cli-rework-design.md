# POSIX CLI rework (cobra)

Date: 2026-07-08
Status: draft (approach approved, pending spec review)

## Why

Ponto's flags are Go `flag`-package style: single dash, camelCase, `-tfPath`,
`-planJSONPath`, `-genImage`. That's neither POSIX nor GNU, there are no short
flags, bools are awkward (`-genImage true` doesn't even do what the README
implies), and the `--help` is the bare `flag` dump. This reworks the whole CLI
onto cobra + pflag so the flags are standard (short `-x`, long `--long`,
kebab-case), the help is grouped and readable, and the tool is pleasant both
run by hand and wired into an automated pipeline.

It's a clean break. Ponto is a fork with no external users to keep happy, so the
old camelCase names go away rather than lingering as aliases. This is a breaking
change to the CLI and that's fine.

This is sub-project 1 of two. Sub-project 2 (the bubbletea TUI, `-i/--tui`) sits
on top of this. The mode handling here is built so that adding the `--tui` mode
later is a one-line addition, but the `--tui` flag itself lands with sub-project
2, not here.

## Stack

- **cobra** for the command, help, and `--version`.
- **pflag** (comes with cobra) for POSIX/GNU flags.
- **viper** for env-var binding.

Single root command `ponto`, no subcommands. `RunE` does the work so errors
return instead of calling `log.Fatal` all over.

## The flags

Default with no mode flag: serve the web UI (today's behaviour).

### Modes (mutually exclusive)

| flag | was |
| --- | --- |
| `-s, --standalone` | `-standalone` |
| `-g, --gen-image` | `-genImage` |

`--tui` (`-i`, alias `--interactive`) is reserved for sub-project 2. cobra's
`MarkFlagsMutuallyExclusive` enforces one-mode-at-a-time with a clear error
instead of silently letting one win.

### Input

| flag | was |
| --- | --- |
| `-C, --working-dir` | `-workingDir` |
| `-p, --plan-path` | `-planPath` |
| `-j, --plan-json-path` | `-planJSONPath` |
| `-f, --tf-vars-file` (repeatable) | `-tfVarsFile` |
| `--tf-var` (repeatable) | `-tfVar` |
| `--tf-backend-config` (repeatable) | `-tfBackendConfig` |
| `-t, --tf-path` | `-tfPath` |
| `-w, --workspace` | `-workspaceName` |
| `--tfc-org` | `-tfcOrg` |
| `--tfc-workspace` | `-tfcWorkspace` |
| `--tfc-new-run` | `-tfcNewRun` |

### Output / server

| flag | was |
| --- | --- |
| `-a, --address` | `-ipPort` |
| `-o, --output` | `-zipFileName` and `-imageFileName`, unified |
| `--image-format` | `-imageFormat` |
| `--name` | `-name` |
| `--show-sensitive` | `-showSensitive` |

`-o, --output` (default `ponto`) is the base name for whatever the run
produces: `<output>.zip` in standalone mode, `<output>.svg` / `<output>.png`
for an image. One name flag instead of two that never applied at the same time.

### Meta

- `-v, --verbose`: turn on the verbose log lines. New, and it doubles as the fix
  for the upstream complaint that a long `terraform init` looks like a freeze
  (rover #64): with `-v` you see what it's doing.
- `--version`: cobra's, wired to the build version.
- `-h, --help`: cobra's, with the flags grouped (modes / input / output / meta)
  rather than one flat list.

## Terraform binary lookup

`--tf-path` keeps working as an explicit override, but when it isn't set the
default is no longer a hard-coded `/bin/terraform`. Ponto looks for `terraform`
on `$PATH`, then `tofu`, and only falls back to `/bin/terraform` if neither is
found. That fixes the Windows / Homebrew / tfenv pain (rover #49, #109) and
gives OpenTofu users a working default (rover #133), without Ponto having to
know anything about OpenTofu beyond the binary name.

The Docker image still ships terraform at `/bin/terraform`, and that's on
`$PATH` in the image, so the container behaviour is unchanged.

## Env-var binding

viper binds every flag to a `PONTO_`-prefixed env var: `--tf-path` reads
`PONTO_TF_PATH`, `--address` reads `PONTO_ADDRESS`, and so on (kebab to
SCREAMING_SNAKE). Precedence is the usual one: an explicit flag beats the env
var beats the default. No config file, just flags and env. This is for the
automated case, where setting `PONTO_TF_PATH` once in the job env is tidier than
threading a flag through every invocation.

## Scope

Go only.

- `main.go`: replace the `flag` block with a cobra root command, its flags, the
  viper binding, the mutually-exclusive marking, and the tf-path lookup. Move
  the body into `RunE`.
- Thread the unified `--output` name into standalone (`zip.go`) and image
  (`screenshot.go`) in place of the two old names.
- `--version` via `rootCmd.Version` fed by the existing build version var.
- `README.md`: rewrite the usage examples onto the new flags (the current ones
  show `-genImage true`, `-planJSONPath=`, `-standalone true`, `-tfVarsFile`,
  `-tfBackendConfig`, all of which change).

No UI changes, no Docker/workflow changes (the image entrypoint passes no flags;
CI passes none either).

## Verification

- `go build`, `go vet`, `gofmt` clean.
- `ponto --help` shows grouped POSIX flags; `ponto --version` prints the version.
- Serve (no mode), `--standalone`, and `--gen-image` each still work end to end
  in the Docker image against `example/random-test`, producing the web UI, a
  `<output>.zip`, and an image respectively.
- `--gen-image --image-format png -j plan.json` still writes `<output>.png`.
- Mutually-exclusive modes error clearly (e.g. `--standalone --gen-image`).
- `PONTO_ADDRESS=0.0.0.0:9010 ponto` picks up the env var; an explicit
  `--address` overrides it.
- With no `--tf-path` and `terraform` on `$PATH`, Ponto finds it; with neither
  `terraform` nor `tofu` present it falls back to `/bin/terraform`.

## Out of scope

The TUI (`-i/--tui`) and its behaviour: sub-project 2. Any change to the web UI,
the graph, or the image content. A config file (viper supports one, but there's
no need yet).
