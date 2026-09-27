# Blitz Go — Where to Pick Up

Written 2026-09-24 at commit `7e211697` on `main`; updated 2026-09-25 and 2026-09-26. On 2026-09-26 the code moved to `github.com/retail-cortex/blitz` with a fresh history (initial commit `53f8c53`): the commit hashes below, and the `python-final` and `v0.1.0` tags, exist only in the former repository, `rmcguinness/code_puppy`. Read this first when resuming. [ROADMAP.md](ROADMAP.md) has the detail behind each finished item, and [MANUAL_VERIFICATION.md](MANUAL_VERIFICATION.md) has the checks that need a person.

## State

Roadmap items 1–22 are done and committed, and item 23 (the desktop app) is in progress; each has its own commit:

| Commit | Change |
|---|---|
| `383ab847` | Per-file locks for parallel edits; refuse to write a file that changed during approval |
| `ae509e39` | Earlier work: Anthropic provider, workspace-scoped sessions, `/compact`, MCP prefixes/agents, saved forged tools, web search, pricing, i18n, images |
| `bd0a0bb5` | `slog` diagnostic log + opt-in OpenTelemetry (content stripped), chained turn traces |
| `04bd68dc` | `post_tool` hooks on an ordered background worker |
| `b13ba38d` | Retries for every provider, stall timeouts, MCP stdio restart + circuit breaker, parallel tool cap |
| `72acd1c4` | Steering a running turn (type or Ctrl+T) |
| `4d1f31c5` | `!cmd`, enforced `/plan` and `--plan`, `/tools`, `/show` |
| `042263dd` | Ordered model fallback across providers; Ollama base-URL fix |
| `7a06b7a6` | Default Gemini model → `gemini-3.8-flash` |
| `7e211697` | Per-agent models (`[agent_models]`, `/pin_model`, `/unpin`) |
| `b652f263` | Per-model settings (`[model_settings]`, `/model_settings`) |
| `36c2758b` | Removed the committed editor swap file; `*.swp` ignored |
| `957c08b9` | Named session snapshots (`/session save`, `/session load <name>`, `--resume=<name>`) |
| `9eeab5e6` | Google search via Gemini grounding; `/search web`, `/search session` |
| `155d595b`, `a5f34180`, `e2f02959` | Item 22: Castor skill definitions and `[skills.policy]`; the gVisor/OS script sandbox; environments, `run_skill_script`, `/envs` |
| (the commit removing `python/`) | Go-only repository: Python removed (tag `python-final`), Go at the root, workflows `ci.yml` and `release.yml`, Apache 2.0 with `NOTICE`, planning docs in `.agents/` |
| `1370b80f` | Side questions (`/btw`) |
| `56a870a4`, `dce9baa9`, `c239dfb8`, `f318498d` | Dependabot and a macOS release note; pinned release tools, `GOTOOLCHAIN=local`, a cross-host reproducibility check; project-layout with code in `internal/`; actions upgraded (checkout v7, setup-go v7, Node 24) |
| `8ce5c642` | ROADMAP item 23, phase 2: `engine.Open` and `engine.Workspace` replace `cmd`'s `buildEnv` |
| `75630f18` | Phase 3: `Workspace.Run` and `Steer`, one turn lifecycle for the REPL and one-shot runs |
| `e657a7bd` | Phase 4, agents and models: typed operations for `/agents`, `/agent`, `/model`, `/pin_model`, `/unpin`, `/model_settings`, `/set` |
| `ef91966a` | Phase 4, sessions: `/session list/new/load/save`, `/resume`, `/rename` |
| `8bbe0b94` | Phase 4, checkpoints and approvals: `/undo`, `/checkpoints`, `/diff`, `/approvals` |
| `5bf56f4c` | Phase 4, skills, envs, MCP and tools: `/skills`, `/envs`, `/mcp`, `/tools` |
| `4ddbb783` | Phase 4, the rest: `/cost`, `/context`, `/compact`, `/memory`, `/locale`, `/sandbox`, `/attach`, `/paste`, `/search`; `tui.App` reduced to the workspace and terminal state |
| `900c1b96` | ROADMAP item 23 replanned: one service per user, CLI attaches, Connect + buf |
| `3fb181cd`, `af7cad97` | Phase 5: `api.Event` instead of ADK events for every client; the prompt is recorded before steering can start |
| `42aaf41b` | Phase 5b: per-workspace state, no dependence on the working directory |
| `f9a6e7bf` | ROADMAP item 24 planned: workers |
| `d8c08d91` | Phase 6: the service API as protos, buf, generated Go and Connect code |
| `2e4cb2c0`, `a6ed23d6`, `a926faa6` | Phase 7: `apps/service/internal/server`, approvals over the stream, `blitz serve`, the workspace lock |
| `26e261cd`, `d969c75f`, `d5562753` | Phase 8: `api.Backend`, `pkg/client`, the CLI attaching to the service, `--local` |
| `31531764`, (the commit adding `pkg/engine/workers.go`) | Item 24a–b: worker definitions, schedules, permissions (`ApprovalRequest.Targets`); enabling pinned to the hash, `[workers.policy]` |
| (the commit adding `pkg/engine/session/title_test.go`) | Session names from the first prompt, `/rename`, terminal title, resume hint on exit |

`bazel test --config=race //...` passes (vet and staticcheck run in every compile), and so does `bazel run //bazel:check_deps`.

## Dated reminders

- **Gemini 3.8 Flash price (handled).** The introductory $0.75 / $3.75 / $0.075 per 1M tokens ends on 2026-12-31. `config.PriceChanges` switches `gemini-3.8-flash` to $1.50 / $7.50 / $0.15 from 2027-01-01 (UTC) when the configuration loads, so no code change is due then; a long-running `blitz serve` picks it up at its next restart. Before a release in 2027, check https://ai.google.dev/gemini-api/docs/pricing and fold the change into `DefaultPricing`.

## Open work, in suggested order

**Priority (2026-09-26, owner's decision): close feature gaps before adding new features.** The gaps are specified as requirements in [specs/spec_backlog_026.md](specs/spec_backlog_026.md) (Blitz's own) and [specs/spec_parity_027.md](specs/spec_parity_027.md) (against Claude Code, Antigravity CLI and Antigravity; §14 gives the order; §12 records the five conflicts with earlier decisions, decided below). Specs 001–025 describe current behaviour; move requirements into them as gaps close.

1. **ROADMAP item 23 is done in a first version**: the per-user service (`blitzd`, `service install`), the proto API, the CLI attaching to it, workers (item 24), and the desktop app (`bazel build //apps/desktop/packaging:Blitz.app`). **Next, in order:** run MANUAL_VERIFICATION sections 36–37 (the service, workers and the app need a person, and some checks are 💲); then the known gaps: workers' edits joining `/undo`, an attached CLI's process list and `!cmd` audit (ROADMAP 8b, 24c). The app's slash commands, Markdown, images, notifications, translation and release job are done (2026-09-27, branch `desktop-polish`); its signing is item 3.
2. **Manual verification (needs a person).** Nothing in `MANUAL_VERIFICATION.md` has been run yet. It covers real providers (💲 = paid calls), terminal behavior, the macOS and Linux sandboxes, MCP, steering, fallback and pinning. Record results in the file; any failure becomes the next task.
3. **Name the release archives' macOS binaries** (spec_monorepo_028 §5): they're signed `a.out` by Go's linker, which macOS takes for one program; build them on the macOS runner and `codesign --identifier`, or sign with rcodesign on Linux, keeping the archives reproducible.
4. **Sign and notarize the desktop app** (needs an Apple Developer Program membership). The release job (`release.yml` `desktop`, [spec_release_025](specs/spec_release_025.md) REL-20–25) signs ad hoc and warns until these repository secrets exist (Settings › Secrets and variables › Actions):
   - `MACOS_CERTIFICATE`: a **Developer ID Application** certificate with its private key, exported from Keychain Access as `.p12`, then `base64 -i cert.p12 | pbcopy`;
   - `MACOS_CERTIFICATE_PASSWORD`: the `.p12`'s export password;
   - `DESKTOP_SIGN_IDENTITY`: the certificate's name, `Developer ID Application: <name> (<TEAMID>)` (`security find-identity -v -p codesigning`);
   - `NOTARY_KEY`: an App Store Connect API key (Users and Access › Integrations › Team Keys, role Developer), the `.p8` file's text;
   - `NOTARY_KEY_ID` and `NOTARY_ISSUER`: that key's ID and the issuer ID shown on the same page.
   
   To try it locally first: with the certificate in your keychain and the `.p8` on disk, `bazel build --config=release //apps/desktop/packaging:Blitz.app`, then `DESKTOP_SIGN_IDENTITY=… NOTARY_KEY_PATH=… NOTARY_KEY_ID=… NOTARY_ISSUER=… apps/desktop/packaging/sign_macos.sh "$(bazel cquery --config=release --output=files //apps/desktop/packaging:Blitz.app)" <version> Blitz.dmg`. Then tag a release and run MANUAL_VERIFICATION §18's desktop checks. Neither the signed path nor the release job has run yet; if `gh release upload` can't find the draft release by its tag, pass the release's ID instead.
   
   The CLI archives stay unnotarized (the release job could sign their macOS binaries with the same certificate and key, and notarize them in a zip); until then the release notes (`.github/release-footer.md`) and the README explain clearing the quarantine flag. Dependabot (`.github/dependabot.yml`) keeps the pinned action SHAs current. `~/.gnupg/gpg-agent.conf` now points at GPG Suite's `pinentry-mac`; the next tag will show whether tag signing works.
5. **Linux sandbox startup cost.** Before every sandboxed command, `expandBlocked` (`pkg/engine/tools/bwrap.go`) scans the writable roots, including the temp and cache directories (e.g. the Go build cache), for blocked names, up to 50,000 entries. Measured in a Linux container: about 0.1 s per command normally, 3.4 s under `-race`. Worth reducing (e.g. skip cache directories for name patterns, or reuse a recent scan), keeping in mind that a file created between scans could then escape masking. CI's parallel-cap test runs with the sandbox off because of this.
6. **Skill scripts, follow-ups** (ROADMAP item 22 is done: Castor definitions, `[skills.policy]`, the gVisor/OS `ScriptBox`, environments, `run_skill_script`, `/envs`):
   - TypeScript scripts;
   - scripts that write the workspace directly, with snapshots;
   - `requires-python` with uv-managed interpreters;
   - `storage_uri` and resources;
   - running the opt-in real-install tests in CI (`BLITZ_PYENV_TESTS=1`);
   - adding a network field to Castor's proto, instead of `custom_hints.network`.
7. **Optional: reasoning settings per model.** Python's `/model_settings` also sets `reasoning_effort`, extended thinking and budgets. The Go wrapper (`pkg/engine/runtime/settings.go`) is where they'd go, mapped to genai `ThinkingConfig`, which each adapter translates differently.
8. **Optional, from the Antigravity review (ROADMAP, "Antigravity CLI review"):** `/copy` (last reply to the clipboard, with OSC 52 over SSH), `--add-dir <path>` at startup, `/grill-me` (the agent interviews you before coding), and `/fork [n]` (branch a new session from an earlier turn).

## Decisions already made (don't redo without a reason)

- **The product is Blitz** (2026-09-26), renamed from Code Puppy: binary `blitz` (with `blz` as a symlink), module `github.com/retail-cortex/blitz`, proto package `blitz.v1`, `~/.blitz`, `.blitz/` in workspaces, `BLITZ.md`, `BLITZ_*` variables, the `[blitz]` config section, the default agent `blitz`, `Blitz.app` (`dev.blitz.desktop`), the login item `dev.blitz.service` / `blitz.service`. **A clean break:** nothing under the old name is read or migrated. `NOTICE` and `docs/HISTORY.md` keep the attribution to Mike Pfaffenberger's Code Puppy. The repository moved to `retail-cortex/blitz` (2026-09-26); the remote, the README's and `.goreleaser.yaml`'s cosign identity now name it, and releases made before the move verify against the former repository's identity. The tone pass is done (no mascot; status marks ✓ ✗ ! ↳ only; a one-line banner and `agent ›` prompt; persona-free agent prompts; `qa-kitten` is `qa`; `puppy_name`/`owner_name` and the legacy `puppy.cfg` loader removed). A deliberate proto break is declared with a `Breaking-API: <why>` line in the commit message, which CI's `buf breaking` step honours. `blitz exec <prompt>` is the explicit one-shot form (flags shared with the root command; no prompt is a usage error), and the README opens with the Blitz lore, with claims kept to what exists. Not done: a `blz refactor` command (to be designed), and the Sam Walton line, left out pending a source (his famous bird dog was Ol' Roy).

- **No shared event bus.** Considered for steering and dropped: audit, hooks and traces are fed from engine callbacks, and steering didn't need a bus.
- **Steering rides on tool results** (`message_from_user`), because ADK model callbacks can't add session events (`Session()` returns nil there).
- **Each turn is its own trace root.** Turns are linked to the previous turn and tagged `gen_ai.conversation.id`; the previous turn's traceparent is kept in session metadata as `last_turn`. A session is never one long trace.
- **Content is never exported by default.** The ADK puts tool arguments and results on every `execute_tool` span, so `pkg/observability` filters them before export. OTel providers are built here, not with `adk/telemetry.New`, which adds its own unfiltered exporter.
- **The Python implementation is gone** (2026-09-26): the Go code is the repository root, and `python-final` tags the last commit with Python (in the former repository's history). Don't reintroduce a second implementation.
- **Not ported from Python:** `/truncate` (use `/compact`) and `/tutorial`. Reasons are in ROADMAP item 14. (`/cd` was also left out then; it is now planned, see below.)
- **Parity decisions (2026-09-26, owner), replacing earlier rejections; details in [specs/spec_parity_027.md](specs/spec_parity_027.md) §12:**
  - **`bypass` permission mode instead of `--dangerously-skip-permissions`:** entered only with `--permission-mode bypass`, and it refuses to run unless the OS sandbox is active; deny rules and blocked paths still apply. `blitz.auto_approve` becomes this mode. Prompt fatigue is mainly addressed by `accept-edits` and a reviewing-model `auto` mode.
  - **`/cd` is adopted:** it closes the workspace and reopens the target through `engine.Open` (lock, roots, sandbox, memory, MCP, project config) and moves the session; nothing is patched mid-session.
  - **Project configuration is adopted in layers:** `.blitz/settings.toml` may tighten without asking; anything that runs code or loosens policy loads only after trust pinned to the workspace path and a content hash; credentials, endpoints, telemetry targets and sandbox loosening are never read from a project. This replaces "configuration only from trusted locations" as the rule for project files; user configuration still loads only from trusted locations.
  - **The REPL stays line-based,** with inline arrow-key pickers; full-screen views go in the desktop app.
  - **`/boost` and agent teams are deferred** until background sub-agents and worktrees exist.
- **`fallback_models` switches only before any output** and never on cancellation. Breakers are asked just before each attempt (regression tests guard this).
- **Model settings are applied per built model, not per agent.** `newProviderModel` wraps every model, so each member of a fallback chain uses its own `[model_settings]`. Applying them in `newLLMAgent` would give fallbacks the primary's settings.
- **Snapshots are never continued in place.** Loading one (by name or ID, `/session load`, `/resume`, `--resume=`) copies it to a new session, as Python's `/load_context` does, and `--continue` skips snapshots.
- **No Anthropic server-side web search** (dropped by the user). **No Google Custom Search JSON API**: it shuts down on 2027-01-01, so `google` means Gemini grounding.
- **`/search web` pre-approves exactly the URLs it hands over, for that turn only** (`tools.WithFetchGrants`); everything else the agent fetches still asks.
- **Telemetry and OTel need one provider per process:** the ADK binds its tracer to the first global provider.
- **Bazel, in a monorepo** (2026-09-27, owner's decision; reverses "No Bazel" of 2026-09-26). The engine had bled into the front ends; the repository is now three apps over shared packages (`apps/`, `pkg/`, `pkg/engine/`), and Bazel 9.2 builds and tests everything: Go, the desktop page, the protos (generated, never committed) and the packages. The service is its own program, `blitzd`. Make, GoReleaser and the tools module are gone; tools come through Bazel, and host tools are a last resort (`bazel run //bazel:tidy` for dependencies). The Linux desktop build uses the system's GTK through pkg-config, deliberately not hermetic. [specs/spec_monorepo_028.md](specs/spec_monorepo_028.md).
- **Layout follows golang-standards/project-layout** (2026-09-26): `apps/cli` (CLI/TUI, pure Go) and `apps/desktop` (Wails, cgo, built on each OS); code in `internal/`, `pkg/` only for deliberately public APIs; frontend in `apps/desktop/web`, packaging in `build/`. Slash-command logic moves from `tui` to a UI-agnostic `pkg/engine` shared by both UIs.
- **One engine service per user** (2026-09-26, replacing "a process per tab"): the service (`blitz serve`; since 2026-09-27 its own program, `blitzd`) hosts every workspace over Connect on a Unix socket; the CLI attaches when it's running, else runs in-process. Scheduled workers (item 24) run in it. See ROADMAP item 23.
- **The module path is `github.com/retail-cortex/blitz`**, matching the repository.

Conventions, layout and commands: [AGENTS.md](AGENTS.md).
