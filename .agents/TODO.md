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

## P1: follow-ups from 2026-09-29

4. **Small follow-ups (S each).**
   - Fallback-model notices ("switched to the fallback model") only reach
     the service's log: send them as turn `notice` events (api.Notice,
     `TurnEvent.notice`, added for failed saves) so the desktop shows them.
   - The tray's menu is English only: give it the i18n catalogs.
   - The tray isn't packaged for Windows (the library supports it; the
     start-at-login entry and a package are missing).
   - `Settings › Logs` and the tray's **Open the logs** only read
     `log.dir` of the global settings.
   - Empty chats saved by versions before `5059c9d` stay on disk (hidden
     from the list): a one-off cleanup could delete metadata files with no
     messages and no history.

## P2: features and the rest

5. **The attached CLI's background processes and `!cmd` audit (M).**
   Backlog BL-SVC-01, BL-SVC-02. Attached to the service, the CLI can't
   list or stop background processes (`Processes()` is nil) and doesn't
   audit `!cmd` (`AuditShell` does nothing). Add `ListProcesses`,
   `GetProcessOutput`, `KillProcess` and `AuditShell` to
   `WorkspaceService`, scoped to the session.

6. **Faster Linux sandbox masking (M).** Backlog BL-SH-01. bubblewrap's
   masking walks the writable roots before every command (about 100 ms
   on a 10,000-file workspace, 3 s under `-race`), so CI runs parallel
   tests without the sandbox. Cache the mask per root (by mtime, or
   inotify), skip known build and cache directories when the rules
   allow, and turn the sandbox back on in CI's parallel tests.

## P3: large, needing a design first

7. **Project configuration with a trust boundary (L).** Teams can't
   commit MCP servers, commands, permission presets or hooks to a
   repository. Add `.blitz/settings.toml` (and `.local.toml`) per the
   decisions in `spec_parity_027` §12 and §2.4: settings that only
   tighten apply at once; anything that runs code (hooks, command MCP
   servers, allow rules) needs a one-time confirmation recorded in
   `~/.blitz/trust.json` by path and the file's SHA-256, asked again
   when it changes. Builds on the per-workspace settings and permission
   scopes that exist now.

8. **Background sub-agents (L).** `invoke_agent` blocks the turn. Add
   `background: true` returning a task ID (`spec_parity_027` §8.1),
   `list_tasks`, `task_output` and `stop_task`, and deliver completions
   as steer messages at the next tool cycle or prompt.

## Waiting on something

- **Claude on Vertex AI, live** (backlog BL-VER-04): needs Claude Opus
  quota in `rmcguinness-lab` (requested). Then BL-ENG-22: client-side
  refusal fallback on Vertex AI.
- **Chromebooks:** if a colleague sees a blank window, set
  `WEBKIT_DISABLE_DMABUF_RENDERER=1` automatically on ChromeOS.
- **A signed macOS release:** the Apple Developer ID is pending.
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

