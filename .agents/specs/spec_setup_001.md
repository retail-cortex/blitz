# spec_setup_001 — Project setup: repository, layout, toolchain and conventions

| | |
|---|---|
| Status | Implemented (reverse-engineered from `53f8c53`) |
| Source | `go.mod`, `tools/go.mod`, `Makefile`, `.gitignore`, `buf.yaml`, `buf.gen.yaml`, `.agents/AGENTS.md`, `LICENSE`, `NOTICE`, `docs/HISTORY.md` |
| Next | [spec_config_002](spec_config_002.md) … [spec_release_025](spec_release_025.md) |

## 1. Purpose

Blitz is a native Go coding agent built on the Google Agent Development Kit (`google.golang.org/adk/v2`), running Gemini, Claude, OpenAI-compatible or Ollama models. The CLI is one static binary; the same engine also runs as a per-user service for a desktop app and scheduled workers. This spec fixes the repository shape everything else builds on. Product tone: terse output, no persona or mascot, plain status marks (`✓ ✗ ! ↳`).

## 2. Identity

- **SET-01** Product **Blitz**; binary `blitz` with `blz` as a symlink; module `github.com/retail-cortex/blitz` (repository `github.com/retail-cortex/blitz`); proto package `blitz.v1`; user state in `~/.blitz`; workspace state in `.blitz/`; instruction file `BLITZ.md`; environment variables `BLITZ_*`; config section `[blitz]`; default agent `blitz`; app `Blitz.app` (`dev.blitz.desktop`); login items `dev.blitz.service` / `blitz.service`.
- **SET-02** Clean break from the former name (Code Puppy): nothing under the old name is read or migrated. `NOTICE` and `docs/HISTORY.md` keep attribution to the original Python project (MIT, Mike Pfaffenberger); the last Python commit is tagged `python-final` in the former repository (`rmcguinness/code_puppy`) — this repository starts from a fresh history (`53f8c53`) — and Python must not be reintroduced.
- **SET-03** License Apache 2.0; new files need no header.

## 3. Layout (golang-standards/project-layout)

| Path | Contents |
|---|---|
| `apps/cli` | CLI (pure Go, `CGO_ENABLED=0`) |
| `apps/desktop` | Desktop app — **its own Go module** (Wails v2, cgo) |
| `pkg/engine` | UI-agnostic application layer (`engine.Open`, `Workspace`, `Backend`) |
| `pkg/engine/runtime` | Engine over the ADK runner; providers; fallback; settings; compaction; steering; usage |
| `pkg/engine/tools` | Tools and guardrails: workspace sandbox, approvals, command policy, OS sandbox, script sandbox, skills scripts, web, MCP, hooks, checkpoints |
| `apps/cli/internal/tui` | REPL |
| `pkg/config` | Configuration and in-place editing |
| `pkg/engine/session` | Transcripts, snapshots, ADK event store, search |
| `pkg/engine/skills`, `pkg/engine/agents` | Definitions; built-ins embedded |
| `pkg/i18n` | Catalogs (`en-US`, `es`, `fr-CA`) |
| `pkg/observability`, `pkg/engine/audit`, `pkg/redact` | Logs, telemetry, audit, masking |
| `pkg/images`, `pkg/engine/memory`, `pkg/engine/workers`, `pkg/engine/breaker`, `pkg/textutil` | Supporting packages |
| `apps/service/internal/server`, `pkg/client` | Service handlers; attached client |
| `proto` | Generated Go/Connect code — committed, **never edited** |
| `proto/blitz/v1` | Service protos |
| `apps/desktop/web` | Desktop page (React + TypeScript, pnpm); generated TS in `src/gen` |
| `apps/desktop/packaging` | Icons and platform packaging |
| `tools` | Separate module pinning build tools as `tool` directives |
| `.agents` | `AGENTS.md`, `ROADMAP.md`, `NEXT_STEPS.md`, `MANUAL_VERIFICATION.md`, `specs/` |

- **SET-10** New code goes in `internal/`; `pkg/` is reserved for deliberately public APIs (none yet). Slash-command logic lives in `pkg/engine`, not in a UI.
- **SET-11** Build outputs are not committed (`/bin/`, `/dist/`, `/blitz`, `/blz`, desktop `dist/*` except `.gitkeep`, `/apps/desktop/packaging/bin/`, `/apps/desktop/web/wailsjs/`); editor swap files ignored.

## 4. Toolchain

- **SET-20** Go `1.27.1` for both modules; CI uses `GOTOOLCHAIN=local`.
- **SET-21** Build tools pinned in `tools/go.mod` and run with `go tool -modfile=tools/go.mod <tool>`: `buf`, `protoc-gen-go`, `protoc-gen-connect-go`, `wails`, `staticcheck`, `govulncheck`. `protoc-gen-es` is pinned by the desktop pnpm lockfile.
- **SET-22** No Bazel (decision): Go modules, `CGO_ENABLED=0` and GoReleaser give reproducible CLI builds; Make stays a thin entry point.
- **SET-23** Key dependencies: ADK v2, `genai`, `anthropic-sdk-go`, `openai-go`, Connect, `modenv`, BurntSushi TOML, cobra/pflag, `mvdan.cc/sh` (command parsing), `go-udiff`, glamour, ergochat/readline, `robfig/cron`, OpenTelemetry, gVisor `sandboxexec` (Linux).

## 5. Make targets

| Target | Does |
|---|---|
| `build` | `CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "-s -w -X main.version=<git describe>"` → `bin/blitz` + `bin/blz` symlink |
| `install` | Copies to `$GOBIN` (or `$GOPATH/bin`) with `blz` |
| `test`, `test-race`, `vet`, `lint` (staticcheck), `vulncheck` | Checks |
| `check` | `vet lint vulncheck test-race` |
| `proto`, `proto-check` | buf lint/format/generate; fail on stale generated code |
| `desktop`, `desktop-check` | Desktop app build and checks |
| `cross-compile` | darwin/linux amd64+arm64, windows amd64 |
| `snapshot`, `release-check`, `tidy`, `clean` | GoReleaser and housekeeping |

## 6. Conventions (for people and agents changing the code)

- **SET-30** One commit per feature, with a message explaining why.
- **SET-31** A bug fix includes a test confirmed to fail without the fix.
- **SET-32** New user-facing strings go into all three catalogs (enforced by tests).
- **SET-33** Each feature updates the README, a ROADMAP item and a MANUAL_VERIFICATION section.
- **SET-34** Every package's tests fail on leaked goroutines (`goleak`, `leak_test.go`).
- **SET-35** gVisor code builds only on Linux (`//go:build linux`, stub elsewhere); check `GOOS=linux go vet ./pkg/engine/tools` from macOS; its tests need `runsc`.
- **SET-36** Real-terminal behaviour is checked with `script` against a fake provider (answer the line editor's `ESC[6n` with `ESC[1;1R`).
- **SET-37** Specs are named `spec_<name>_NNN.md`, NNN being the global order of execution/dependency.
