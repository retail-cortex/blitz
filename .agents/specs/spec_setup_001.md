# spec_setup_001 — Project setup: repository, layout, toolchain and conventions

| | |
|---|---|
| Status | Implemented (reverse-engineered from `53f8c53`) |
| Source | `go.mod`, `MODULE.bazel`, `.bazelrc`, `.gitignore`, `buf.yaml`, `.agents/AGENTS.md`, `LICENSE`, `NOTICE`, `docs/HISTORY.md` |
| Next | [spec_config_002](spec_config_002.md) … [spec_release_025](spec_release_025.md) |

## 1. Purpose

Blitz is a native Go coding agent built on the Google Agent Development Kit (`google.golang.org/adk/v2`), running Gemini, Claude, OpenAI-compatible or Ollama models. The CLI is one static binary; the same engine also runs as a per-user service for a desktop app and scheduled workers. This spec fixes the repository shape everything else builds on. Product tone: terse output, no persona or mascot, plain status marks (`✓ ✗ ! ↳`).

## 2. Identity

- **SET-01** Product **Blitz**; binary `blitz` with `blz` as a symlink; module `github.com/retail-cortex/blitz` (repository `github.com/retail-cortex/blitz`); proto package `blitz.v1`; user state in `~/.blitz`; workspace state in `.blitz/`; instruction file `BLITZ.md`; environment variables `BLITZ_*`; config section `[blitz]`; default agent `blitz`; app `Blitz.app` (`dev.blitz.desktop`); login items `dev.blitz.service` / `blitz.service`.
- **SET-02** Clean break from the former name (Code Puppy): nothing under the old name is read or migrated. `NOTICE` and `docs/HISTORY.md` keep attribution to the original Python project (MIT, Mike Pfaffenberger); the last Python commit is tagged `python-final` in the former repository (`rmcguinness/code_puppy`) — this repository starts from a fresh history (`53f8c53`) — and Python must not be reintroduced.
- **SET-03** License Apache 2.0; new files need no header.

## 3. Layout

A monorepo of three apps (`apps/cli`, `apps/service`, `apps/desktop`) over shared packages (`pkg/`, the engine in `pkg/engine/`), with the protos in `proto/` the build's rules in `build/`, commands for working on it in `tools/`, changes to outside code in `third_party/`, and the release archives in `release/`: [spec_monorepo_028](spec_monorepo_028.md) §2 has the table and the dependency rules.

- **SET-10** Code private to one app goes in `apps/<app>/internal/`; code more than one app uses goes in `pkg/` (the engine's in `pkg/engine/`). Slash-command logic lives in `pkg/engine`, not in a UI.
- **SET-11** Build outputs are not committed (`/bazel-*`, `/bin/`, `/dist/`, `/blitz`, `/blz`, `/apps/desktop/web/dist/`, `/apps/desktop/packaging/{bin,dist}/`, `/user.bazelrc`); neither is generated code (MR-12). Editor swap files are ignored.

## 4. Toolchain

- **SET-20** Go `1.27.1` (`go.mod`), downloaded by rules_go; Node 22 by rules_nodejs; Bazel 9.2 (`.bazelversion`).
- **SET-21** Tools come through Bazel: protoc (protobuf's toolchain), `protoc-gen-go` and `protoc-gen-connect-go` (Go dependencies), `protoc-gen-es` (the page's lockfile), buf (rules_buf), staticcheck and govulncheck (`tool` directives in `go.mod`). There is no separate tools module.
- **SET-22** Bazel builds everything (decision 2026-09-27, reversing "No Bazel" of 2026-09-26): [spec_monorepo_028](spec_monorepo_028.md). There is no Makefile.
- **SET-23** Key dependencies: ADK v2, `genai`, `anthropic-sdk-go`, `openai-go`, Connect, `modenv`, BurntSushi TOML, cobra/pflag, `mvdan.cc/sh` (command parsing), `go-udiff`, glamour, ergochat/readline, `robfig/cron`, OpenTelemetry, gVisor `sandboxexec` (Linux), Wails v2 (the desktop app).

## 5. Commands

| Command | Does |
|---|---|
| `bazel build //...` | Everything for this machine |
| `bazel test //...` (`--config=race`) | Every test (with the race detector) |
| `bazel run //apps/cli:blitz -- …` | Run the CLI |
| `bazel build //release:archives` | The CLI release archives, every platform |
| `bazel build //apps/desktop/packaging:Blitz.app` / `:deb` | The desktop app's package (macOS / Linux) |
| `bazel run //:gazelle` | Update the BUILD files after adding a package or import |
| `bazel run //tools:tidy` | `go mod tidy`, `bazel mod tidy`, Gazelle |
| `bazel run //tools:check_deps`, `tools/check_format.sh` | The dependency rules; formatting |

## 6. Conventions (for people and agents changing the code)

- **SET-30** One commit per feature, with a message explaining why.
- **SET-31** A bug fix includes a test confirmed to fail without the fix.
- **SET-32** New user-facing strings go into all three catalogs (enforced by tests).
- **SET-33** Each feature updates the README, a ROADMAP item and a MANUAL_VERIFICATION section.
- **SET-34** Every package's tests fail on leaked goroutines (`goleak`, `leak_test.go`).
- **SET-35** gVisor code builds only on Linux (`//go:build linux`, stub elsewhere); check it from macOS with `bazel build --platforms=@rules_go//go/toolchain:linux_arm64 //pkg/engine/tools`; its tests need `runsc`.
- **SET-36** Real-terminal behaviour is checked with `script` against a fake provider (answer the line editor's `ESC[6n` with `ESC[1;1R`).
- **SET-37** Specs are named `spec_<name>_NNN.md`, NNN being the global order of execution/dependency.
