# Blitz TODO

One list, in the order to work through it. Sizes: S (hours), M (a day or
two), L (a week or more). Each item says where the work is and what "done"
means. Written 2026-09-28, last updated 2026-09-29; the backlog spec
(`docs/content/about/specs/spec_backlog_026.md`) keeps the long tail.

## Picking up

Everything through 2026-09-29 is pushed to `main`, most of it with
`[skip ci]`, so CI hasn't run on it. Before new work:

1. **Run CI (S).** Start the CI workflow by hand (workflow_dispatch) on
   `main`. It's the first run with the tray (`apps/tray`), whose macOS
   build uses cgo (Cocoa) and has never been built; and the first to
   check coverage since the features below (the floor is 78%,
   `tools/coverage/floor.txt`; locally the script stops at the known
   test failures below, so only CI measures it). Done: green on Linux and
   macOS, or the failures fixed.

2. **Manual test of the desktop app (S).** Install a fresh build
   (`bazel build //:deb`, then `sudo apt install ./bazel-bin/apps/desktop/packaging/blitz-desktop_amd64.deb`),
   restart the service when the app offers, and go through the new
   checks in `docs/content/development/manual-verification.md`: settings
   editor, log viewer, copy buttons, full width and full screen (F11),
   `@` mentions, add to context, previews (Markdown, images, PDF, **Open
   in the system viewer**), **New worker**, the tray (Settings › Service ›
   **Show in the system tray**: every menu action, logging out and in),
   usage after a restart, chats surviving a service restart. On macOS
   too, once CI builds `Blitz.app`.

3. **Recover the lost Notebook chat (XS, the owner's data, ask first).**
   `~/.blitz/sessions/session-20260929-020838-b9e4f262` lost its
   transcript to the bug fixed in `12b1e6a` (messages written to the
   "active" session, which a restarted service didn't have). Its history
   (`.events.jsonl`) has both prompts and both answers; rebuilding
   `<id>.jsonl` and the title and message count from it restores the
   chat. Only on that machine, and only if the owner still wants it.

## P3: large, needing a design first

4. **Background sub-agents (L).** `invoke_agent` blocks the turn. Add
   `background: true` returning a task ID (`spec_parity_027` §8.1),
   `list_tasks`, `task_output` and `stop_task`, and deliver completions
   as steer messages at the next tool cycle or prompt.
   Design drafted for review: `docs/content/about/specs/spec_background_agents_032.md`
   (not committed yet; the owner is reviewing it).

## Waiting on something

- **Claude on Vertex AI, live** (backlog BL-VER-04): needs Claude Opus
  quota in `rmcguinness-lab` (requested). Then BL-ENG-22: client-side
  refusal fallback on Vertex AI.
- **Chromebooks:** if a colleague sees a blank window, set
  `WEBKIT_DISABLE_DMABUF_RENDERER=1` automatically on ChromeOS.
- **A signed macOS release:** the Apple Developer ID is pending.
- **The tray on Windows:** it needs what Windows lacks first: the service
  installing there (a login item; REL-25 says it doesn't) and a Windows
  package of the desktop app. The tray itself also needs a lock and
  detaching that aren't Unix-only (`flock`, `Setsid`).
- **Wails v3** (decided 2026-09-29: stay on v2). v3 was at `v3.0.0-beta.26`
  and the tray, its main draw, is done separately. Revisit when v3 has a
  stable release that has been out a while, or we need several windows or
  native menus, or a v2 bug blocks us; start with a short branch trial
  (window, bindings, the asset handler, `.deb` and `Blitz.app`).

## Known local test failures

On the owner's Linux machine these fail on unchanged `main` too, from the
environment (sandbox, timing), and pass in CI: `apps/cli`
TestOneShotStopsAtItsTimeout; `apps/cli/internal/tui`
TestREPLTurnUndoDiffCost, TestInitWritesAndLoadsBlitzMD; `pkg/client`
TestRemoteTurnLimits; `pkg/engine/runtime`
TestWithoutPlanModeToolsRunNormally; `pkg/engine/tools` TestToolsSuite.
Anything else failing is new. Check test logs for `goleak:` too.

## Done 2026-09-29
- Project configuration with a trust boundary (spec_project_config_031):
  `.blitz/settings.toml` in three tiers, hash-pinned trust in
  `~/.blitz/trust.json`, the REPL's question, `/trust`, `blitz trust`,
  doctor, and the desktop dialog. Not yet run on Linux or checked by hand
  (manual-verification: "Project settings").
- Faster Linux sandbox masking (BL-SH-01–03): the blocked-path scan is kept
  between commands and re-reads only changed directories (~1 ms for
  50,000 entries, from ~220 ms on macOS); a CI benchmark step; the
  parallel-cap test runs sandboxed. First Linux run is CI's.
- The attached CLI's background processes and `!cmd` audit (BL-SVC-01–03):
  `ListProcesses`, `GetProcessOutput`, `KillProcess`, `AuditShell`,
  scoped to the sessions the client ran turns in.
- Fallback-model notices show in the conversation (turn `notice` events),
  not only in the service's log.
- The tray's menu in the desktop app's language (`tray.*` keys).
- The tray's **Open the logs** opens the running service's log folder
  (`ListLogDays`), not just the settings file's `log.dir`. Settings › Logs
  already read the service's: one log per service, from the global
  settings, is by design.
- Empty chats from before `5059c9d` are deleted when a process opens its
  first workspace (`session.Storage.RemoveEmpty`).

- Full-width conversation and full screen (F11).
- `@` mentions in the chat, with completion; the content goes with the prompt.
- Add to context and new conversation about a file, from the Files view.
- Previews: Markdown, images, PDF (pdf.js).
- New worker dialog; workers in `.agents/workers/`.
- The service in the system tray (`blitz-tray`), with start at login.
- A session's usage (tokens, cost, context size) survives a restart.
- Chats kept their messages after a service restart (the transcript was
  written to the "active" session); a new chat is saved with its first
  message; a failed save shows in the conversation.
- The settings file editor (highlighting, Validate, a reference of every
  setting); stale-service detection after an install; quota errors;
  `thinking_budget` on adaptive-thinking Claude models; the log viewer;
  `doctor --online` wording.

## Done 2026-09-28

- Run CI on everything pushed today (S).
- An unavailable model silently answers "Done." (S).
- Settings › Service hangs after reinstalling the service (S).
- Prove each workspace gets its own configuration (S).
- Copy an agent's response, as text or Markdown (S).
- The settings file editor: highlighting, a Validate button, and a reference (M).
- Local builds that say they're local, and installs that restart the service (S).
- Vertex AI quota errors say "quota" (S).
- `thinking_budget` on current Claude models (S).
- A log viewer in Settings (M).
- `doctor --online` still says "use --online to confirm" (S).

