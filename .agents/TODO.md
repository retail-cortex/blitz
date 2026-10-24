# Blitz TODO

One list, in the order to work through it. Sizes: S (hours), M (a day or
two), L (a week or more). Each item says where the work is and what "done"
means. Written 2026-09-28, last updated 2026-10-23; the backlog spec
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
   (`bazel build //:deb`, then `sudo apt install ./bazel-bin/apps/electron/packaging/blitz-desktop_amd64.deb`),
   restart the service when the app offers, and go through the new
   checks in `docs/content/development/manual-verification.md`: settings
   editor, log viewer, copy buttons, full width and full screen (F11),
   `@` mentions, add to context, previews (Markdown, images, PDF, **Open
   in the system viewer**), **New worker**, the tray (Settings › Service ›
   **Show in the system tray**: every menu action, logging out and in),
   usage after a restart, chats surviving a service restart. On macOS
   too (`bazel build //:mac-app`).

3. **Recover the lost Notebook chat (XS, the owner's data, ask first).**
   `~/.blitz/sessions/session-20260929-020838-b9e4f262` lost its
   transcript to the bug fixed in `12b1e6a` (messages written to the
   "active" session, which a restarted service didn't have). Its history
   (`.events.jsonl`) has both prompts and both answers; rebuilding
   `<id>.jsonl` and the title and message count from it restores the
   chat. Only on that machine, and only if the owner still wants it.

## Queued

### v0.8.4 (branch `release-0.8.4`, planned 2026-10-23)

One branch, one PR. Each item updates its spec and its site pages in the
same commit; targeted tests per item, the full suite with `--config=race`
and `tools/coverage.sh` only before the push. Done so far: routing to
global (`~/.blitz/agents`) and workspace (`.agents/agents`) sub-agents
verified, by transfer and by `invoke_agent` (`f14f2547`); 1, drag and
drop with the computer (`454a1e73`, to be tried in the app).

Order: see **Order of work** below the list. 1 → 2 share the drop
plumbing; 3 is Go only; 4 and 5 share the Electron browser bridge, so
5's partitions land first.

1. **Drag and drop between the OS and the file tree (#70, M).**
   - *In:* `FilesShelf.tsx` `onDragOver` (~:271) also accepts
     `dataTransfer.types` `Files`; dropping on a folder (or the
     background, for the root) asks **Copy here** / **Move here**.
     Bytes go up with `WriteBinaryFile` (works in Electron, the
     `/host/` page and a plain browser, and with the planned remote
     service); folders through `webkitGetAsEntry()` + `CreateFolder`.
     **Move** only in Electron: a new bridge method
     (`webUtils.getPathForFile` in `preload.ts`) gives the sources'
     paths, and Electron's main process trashes them
     (`shell.trashItem`) after every copy succeeded. A plain browser
     offers Copy only. A name that exists asks Replace / Keep both
     (`name 2.ext`) / Skip, once for the batch.
   - *Out:* `webContents.startDrag({files, icon})` from a new bridge
     method, started on a tree row's `dragstart` when the pointer
     leaves the window (Electron only). Always a copy (owner,
     2026-10-23): the OS copies a dragged file and Electron can't tell
     where it landed, so the workspace keeps its file.
   - Check the API's request size limit for large drops (Connect's
     read max); stream or chunk if it's below ~100 MiB.
   - Specs: FIL-30/31 (spec_files_029), EL-12 (spec_electron_038).
     Tests: shelf drop tests with the fake service; Electron bridge by
     hand (manual-verification).
2. **Images in Markdown (#71, M).**
   - Drop a tree image (`application/x-blitz-path`) on the visual
     editor (`VisualEditor.tsx` `editorProps.handleDrop`) or source
     mode (CodeMirror drop handler): inserts `![name](relative path)`
     at the drop point, relative to the document (`mdlinks.ts`).
   - Drop or paste an image from the OS: saved beside the document
     (or `images/` beside it if that folder exists), named after the
     file, then inserted. This lifts VE-60's "pasted or dropped" for
     images.
   - **Image…** (`ImageEditor`, :715-760) becomes a picker: workspace
     images (FindFiles filtered to image types) with thumbnails
     (`pictureURL`), a folder tree, **From your computer…** (upload as
     above), and the address field kept for URLs. Alt text stays.
   - Specs: VE-11, VE-60 (spec_visual_editor_037), FIL-65.
3. **web_fetch falls back to the browser (S–M).** Today `web.go`
   `fetch` is a plain GET with no browser path at all. When the reply
   is a challenge (403/429/503 with cf-chl / "Just a moment" / similar
   markers) or HTML whose text is nearly empty (a JS-only page), and
   `[browser] enabled`, it loads the page in the browser (Navigate +
   text) and says so in the result (`via: "browser"`). Same approval
   key (`web:<host>`), same `allow_domains`, same size cap; a
   `web.browser_fallback` switch (default on). The registry builds the
   browser before web_fetch so it can hold it. Specs: WEB-07, §6
   "Known limits" (spec_web_008). Go tests with an httptest server and
   the existing fake browser.
4. **The agent's browser is the in-app browser (L).** When a desktop
   window is connected, the `browser` tool (and 3's fallback) drives a
   Blitz-owned `WebContentsView` the user can watch, instead of a
   headless system Chrome. Electron relays `webContents.debugger` (CDP)
   over a stream it opens to the service (a new
   `BrowserBridge` RPC through blitz-tray: streams, not polling, and
   it works for a remote service); `browser.Attach` gets a transport
   that isn't a websocket URL. No `--remote-debugging-port` on the
   app: it would let any local process drive the app's own page.
   WEB-32's proxy filtering is replaced on that path by the session's
   `webRequest` (deny rules, private addresses) plus the CDP `Fetch`
   interception already used (WEB-34). Without a window, the headless
   browser as today.
   **Watch or silent** (owner, 2026-10-23): it offers to browse
   silently. `browser.show = "ask" | "watch" | "silent"` (default
   ask): the first browse of a session asks **Watch** / **Run
   silently** (remembered for the session, or for good). Watch opens a
   tab marked "Blitz is browsing" with **Take over** / **Stop**;
   silent runs the view off screen (not attached to the window, or
   `offscreen` rendering) with a small "Browsing example.com" chip in
   the conversation that opens it as a tab. New spec
   (spec_agent_browser_050), WEB-30.
5. **Browser profiles for sign-ins (M–L).** Research below. Named
   profiles, each an Electron partition `persist:profile-<name>` (the
   in-app browser) and, for the headless browser, a persistent
   `--user-data-dir` under `~/.blitz/browser/profiles/<name>` (0700).
   The user signs in by hand in a preview tab on that profile; agents
   then browse with its cookies. Settings › Browser: list, new,
   **Sign in…** (opens a tab), clear data, delete. One profile per
   workspace (owner, 2026-10-23): `browser.profile`, set from the
   global settings only, never project settings (like WEB-38); every
   agent in the workspace browses with it. Untrusted workspaces get a
   throwaway profile. Each profile has its own
   allowed domains; a domain outside them asks. Passkeys: handle
   `select-webauthn-account` (macOS Touch ID). New spec
   (spec_browser_profiles_049), WEB-30.

6. **The status bar's branch toggles Changes (XS).** `StatusBar.tsx:129`
   sends `showView({view: "changes"})`, which only opens. A second click
   closes it: `ViewDetail.toggle`, and App's view handler closes the
   view when it's the one shown. Same for the status bar's jobs button.
   Spec: spec_desktop_024 (status bar).
7. **Git: Add all, and created files caught (S–M).** Two parts:
   - *Add all changes* (`git add --all`, .gitignore honoured) in the
     Changes view's header and menu and the tree's background menu, at
     any time (today only the commit dialog's **Include all changes**,
     when nothing is staged, via `DraftCommit(stageAll)`). A
     `StageAll` RPC in `pkg/engine/gitfiles.go`.
   - *Why files are missed:* `[git] auto_add` (`stageCreated`,
     `gitfiles.go:222`) adds only what the agent wrote with its file
     tools (`Checkpoints().Created`). Files a shell command made (`uv
     init`, a generator, a build), a job's, and dropped files never
     are. Fix: list untracked files at the turn's start (`git ls-files
     --others --exclude-standard`), again at its end, and add the new
     ones (temporary files left out, as now). Specs: spec_files_029
     FIL-59, the git settings' doc.
8. **uv projects ask which layout (S–M).** A built-in skill
   `python-uv` (`pkg/engine/skills/builtin/`, on by default, offered
   to blitz and project-setup), triggered by "new Python project", "uv
   init" and the like. Before running anything it asks (one
   `ask_user_question`): **App** (`uv init --app`, a script-style
   project), **Packaged app** (`--package`, `src/` layout, an entry
   point), **Library** (`--lib`, `src/`, `py.typed`), **Single
   script** (`uv init --script`, inline dependencies), or
   **Workspace** (several packages, `[tool.uv.workspace]`); then Python
   version, and test/lint tools (pytest, ruff). It never assumes a
   flat project. Skill scenario tests (`skillscenarios.go`) for the
   question being asked. Spec: spec_skills (built-in skills).
9. **Jobs from the chat (M).** A `create_job` tool for the blitz agent
   over `Workspace.CreateWorker` (`pkg/engine/workers.go:144`): name,
   schedule in plain text (`ParseSchedule` already reads "every day at
   8 am"), the job's prompt (with where and how it writes: the
   folder, `month-day-year.html`, the style), its permissions
   (`web:news.google.com`, `write:some/directory/**`), agent, model and
   limits; plus `list_jobs`. The agent drafts and shows the job first
   (schedule's next runs, permissions) and creates it on approval.
   Created paused, as the New Job dialog does ("review and enable"),
   with a card in the chat to **Review** / **Enable**. Open question
   (e) below. Spec: spec_workers_023.
10. **"May do without asking" as a rule builder (M).** The New Job
    dialog's free-form box (`desktop.newworker.permissions`, "one
    kind:pattern a line") becomes rows: a kind select (**Run
    commands**, **Write files**, **Delete files**, **Fetch from the
    web**, **Use MCP tools**), a pattern field with that kind's
    placeholder and examples (`reports/**`, `news.google.com`, `go
    test *`), a sentence saying what the row allows, remove, and **Add
    a rule**. Kinds the host's policy doesn't allow are disabled, with
    why. `workerForm.ts` converts to and from the lines, so the file
    format doesn't change. In `WorkerEditor.tsx` and Studio's
    `WorkersPane.tsx`.
11. **A calendar-like schedule picker (M).** Instead of the error-prone
    text field: **Repeats** (Once, Every hour, Every day, Weekdays,
    Every week with day chips, Every month on a day), a time picker,
    a start date (calendar), the time zone as a searchable select, and
    a preview of the next five runs. A new `PreviewSchedule` RPC
    (wraps `workers.ParseSchedule`, returns the next runs or the
    error), so the page and the service agree. Plain text and cron
    stay under **Custom**; an existing job's schedule opens in the
    picker when it fits, else in Custom.
12. **Job limits are defaults, not caps (S–M).** `[workers.policy]`
    ships `max_turns = 200`, `max_cost_usd = 10`, `max_timeout =
    "2h"` (`pkg/config/config.go:822`), so a job can't ask for more,
    and the editor's fields read as maximums. Change: no cap by
    default (0 = none); the defaults (50 calls, $1, 30m) fill in what a
    job leaves out, shown as the fields' placeholders, and a job may
    set more. A host that wants a cap still sets `max_*` (Settings ›
    Jobs: **The most it may ask for**, optional, "No limit"). Settings
    files that set `max_*` keep it. Labels: "Model calls a run
    (default 50)". Spec: spec_workers_023, the settings reference.
13. **A pre-commit hook against committed keys (M).** When the harness
    is set up (project-setup's `git init`) or the window makes a
    repository (`GitInit`, `pkg/engine/changes.go:186`), Blitz adds
    `.githooks/pre-commit` (versioned, so collaborators get it) and
    sets `core.hooksPath` to it in the repository's own config. The
    hook runs `blitz secrets scan --staged` when blitz is on PATH (a
    new command: `pkg/redact`'s patterns plus key formats such as
    `sk-`, `AKIA`, `ghp_`, `AIza`, private-key blocks, and high-entropy
    values in `.env`-like files), else gitleaks if installed, else a
    portable grep of the same patterns; it blocks the commit with the
    file and line (the value masked) and says how to bypass for a
    false alarm (`git commit --no-verify`, or an allow line). An
    existing `core.hooksPath` or pre-commit framework (husky,
    pre-commit) is left alone and the setup agent offers to add the
    scan to it instead. Commits Blitz makes (the commit dialog) run it
    too. For existing repositories: project-setup offers it, and
    Settings › Git has **Protect against committing keys**. New spec
    (spec_secret_hooks_051).

**Order of work:** 1 (done) → 2 → 6 and 7 (quick, and 7 feeds the hook)
→ 9–12 together (one spec, the jobs editor) → 8 → 13 → 3 → 5 → 4.

**Open questions for the owner:** (e) A job the agent creates from the
chat: paused for review like the dialog's, or enabled at once after
the approval in the chat? (f) The key-scanning hook: versioned in
`.githooks/` (collaborators get it) or only in `.git/hooks` (this
clone)?

**Research: Chromium profiles (2026-10-23).**
- What exists: the web preview's `persist:preview` partition
  (`apps/electron/src/main.ts:600`) already keeps the user's sign-ins,
  but nothing on the engine side can use it. The agent's browser
  (`pkg/engine/browser`) is the system's Chrome with a temporary
  profile deleted on close (WEB-30), so it never has a sign-in.
- Electron 44 gives what's needed: `session.fromPartition("persist:…")`
  / `session.fromPath(abs)` are separate, persistent profiles
  (cookies, localStorage, IndexedDB, cache); `ses.getStoragePath()`,
  `ses.clearData()`, `ses.cookies`; per-session permission and device
  handlers; `webContents.debugger` drives a view over CDP without
  opening a debugging port. WebAuthn/passkeys go through
  `select-webauthn-account` / `select-webauthn-authenticator`
  (macOS); without a listener, Touch ID passkeys fail with
  `NotAllowedError`.
- Limits: Google (and some other identity providers) refuse sign-in
  in embedded or automated browsers ("This browser or app may not be
  secure", since 2021); a profile won't fix that. Blitz's own OAuth
  (MCP servers, `pkg/mcpauth`) stays as RFC 8252 says: the system
  browser with a loopback redirect. Chrome refuses
  `--remote-debugging-port` on the user's default profile (Chrome 136
  on), and its cookies are encrypted with the OS keychain, so using or
  importing the user's real Chrome profile is out: profiles are
  Blitz's own.
- Security: a profile's cookies are credentials. Profiles stay in the
  global settings and `~/.blitz`, are never chosen by project
  settings, and `redact`/logs never print cookie values.


### File tree: catching up with VS Code's Explorer

`apps/desktop/web/src/files/FilesShelf.tsx` today: single selection,
keyboard moves, inline new / rename, delete, move by drag in the tree,
git letters, reveal in the file manager, refresh every 5 s. Missing, by
value:

- **Multi-select (M):** Shift/Cmd(Ctrl)-click and Shift+arrows; menu
  actions, delete, drag and **Add to context** on the whole selection.
- **Copy, cut, paste, duplicate (M):** Cmd+C/X/V and the menu, inside
  the tree and with the OS clipboard's files. Needs a `CopyFile` RPC
  (recursive, destination must not exist), shared with #70.
- **Undoable delete (S):** to the OS trash or a workspace trash, with
  **Undo** in a toast, instead of gone for good.
- **Live updates (M):** the service streams file changes (fsnotify) to
  the tree instead of the 5 s poll (FIL-33); streams over polling for
  the remote service.
- **Filter and type-to-find (S–M):** typing in the tree jumps to the
  next match; a filter box narrows the tree (find in tree), using
  `FindFiles`.
- **Reveal the active file (S):** the tree follows the open tab
  (setting), and **Reveal in Files** from a tab's menu.
- **Nested new file (S):** `a/b/c.md` creates the folders.
- **Compact folders (S):** single-child folder chains on one row
  (`src/main/java`).
- **Diagnostics in the tree (S–M):** LSP errors and warnings colour a
  file and its folders (the `lsp` tool already knows them).
- **Drag to the chat and the editor (S):** a row dropped on the
  composer adds it as context; on the editor area opens it.
- **Open to the side (M):** a second editor group (split).
- **More in the menu (S):** Open in terminal, Find in folder
  (`search_workspace` scoped), Copy name, Collapse folders below.
- **Sorting and file nesting (S):** folders first / mixed / by type,
  and nesting patterns (`*.ts` → `*.test.ts`, `package.json` → lock
  files).
- **Big folders (S):** a "show more" row at `maxListed` (5000) instead
  of a silent cut; virtualised rows for long lists.

### Error-handling follow-ups (done 2026-10-03)

All five done on the `search-index` branch, each with tests and its spec:
revoking approvals reports what stayed saved (WS-62), MCP stdio servers
always reaped (MCP-11), language servers start without holding the
manager (FS-61), dropped permission rules shown with the rules
(`ListPermissionRulesResponse.problem`, DSK-85, SVC), PDF text in a
helper process (IMG-52).

- **Playwright end-to-end tests for the desktop page (M).** Decided
  2026-10-01, deferred until the usability pass settles the UI, so the
  fake service (`dev/fake.ts`) isn't rewritten twice. Playwright, for its
  Chromium (the Electron app's engine since v0.6.6). Two tiers: `?fake` for every flow, on every push; a few smoke
  tests against a throwaway keyless `blitzd` (`BLITZ_SOCKET`, `HOME` in a
  short `/tmp` dir), which catch the fake drifting from the service. A
  `//apps/desktop/web:e2e` target (tagged `requires-network`/`manual`:
  Playwright downloads its browsers) and a CI step on Linux and macOS
  after `bazel test //...`. First flows: the file tree (deselect, drag and
  drop), the agent editor (create, problems, advanced settings, delete a
  broken file, Settings › Agents), worker editing (save pauses, stale-file
  reload, fixing an invalid worker). Out of reach: the Electron shell (native
  dialogs, notifications, deep links, the tray, `window.blitzElectron`). Done: both
  tiers green in CI on Linux and macOS.

## Waiting on something

- **Drive the user's own Chrome (L), left out of v0.8.4 (owner, 2026-10-23).**
  How others do it (2026-10-23):
  - *Antigravity:* launches Chrome with its own persistent profile
    (`--user-data-dir=~/.gemini/antigravity-browser-profile`,
    `--remote-debugging-port=9222`) plus its "Browser Connector"
    extension (`debugger`, `tabs` permissions), which gives the agent
    a higher-level API (navigate, DOM, page readiness) over
    `chrome.debugger`. Sign-ins last because the profile does; a
    blue border marks the agent's control. That's Blitz's
    `pkg/engine/browser` with a persistent profile, made visible.
  - *Extension + native messaging* (Claude in Chrome, Codex's Chrome
    control, OpenClaw, agent-browser): an MV3 extension in the user's
    everyday Chrome talks to a local native-messaging host over
    stdio and drives tabs with `chrome.debugger`. No debugging port
    (Chrome 136+ ignores it on the default profile anyway), and the
    user's real sign-ins work, Google's included, since it's real
    Chrome. Costs: a Web Store extension, a native-host manifest per
    browser and OS (registry keys on Windows), and an agent holding
    every sign-in the user has: approvals per site, a tab group of
    the agent's own.
  - For Blitz: v0.8.4's items 4 and 5 cover most of it inside the app; this is the
    path for sites that refuse embedded browsers.
- **Claude on Vertex AI, live** (backlog BL-VER-04): needs Claude Opus
  quota in `rmcguinness-lab` (requested). Then BL-ENG-22: client-side
  refusal fallback on Vertex AI.
- **Chromebooks:** if a colleague sees a blank window, turn the
  graphics card off automatically on ChromeOS (`BLITZ_GPU=off`, EL-07).
- **A signed macOS release:** the Apple Developer ID is pending.
- **Windows, by hand:** the tray and its page are built (spec_windows_036);
  run them on Windows (WIN-40, manual verification §45), then a native
  folder picker and Arm64 (WIN-42).

## Known local test failures

On the owner's Linux machine these fail on unchanged `main` too, from the
environment (sandbox, timing), and pass in CI: `apps/cli`
TestOneShotStopsAtItsTimeout; `apps/cli/internal/tui`
TestREPLTurnUndoDiffCost, TestInitWritesAndLoadsBlitzMD; `pkg/client`
TestRemoteTurnLimits; `pkg/engine/runtime`
TestWithoutPlanModeToolsRunNormally; `pkg/engine/tools` TestToolsSuite.
Anything else failing is new. Check test logs for `goleak:` too.

## Done 2026-09-29
- Background sub-agents (spec_background_agents_032): tasks, their tools,
  results at the next step or prompt, requests asked of the session's
  people (REPL prompt, desktop cards, Ctrl+J), WatchTasks, agent
  frontmatter defaults. Not yet run on Linux or checked by hand
  (manual-verification: "Background tasks"). Gap: a task's edits join the
  latest turn's checkpoint rather than their own.
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

