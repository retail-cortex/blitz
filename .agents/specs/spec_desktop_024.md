# spec_desktop_024 — Desktop app

| | |
|---|---|
| Status | Implemented, second version (Material 3 redesign, 2026-09-27) |
| Source | Go side: `cmd/blitz-desktop/{main,app,proxy,prefs}.go`, `wails.json`, `go.mod` (own module). Page: `web/desktop/src/` — shell `App.tsx`, `Drawer.tsx`, `state.tsx`; views `Workspace.tsx`, `Conversation.tsx`, `RunSettings.tsx`, `Changes.tsx`, `Workers.tsx`; dialogs `SettingsDialog.tsx`, `WorkspaceDialog.tsx`; `Markdown.tsx`; logic `turns.ts`, `prefs.ts`, `theme.ts`, `palette.ts`, `options.ts`, `errors.ts`; `api.ts`, `desktop.ts`; design system `m3.css`, `app.css`, `ui/controls.tsx`; development fake `dev/fake.ts`; `gen/` (generated). Build: `web/desktop/{package.json,vite.config.ts,index.html}`, `build/desktop/{appicon.png,darwin/Info*.plist,windows/*}`, `Makefile` (`desktop`, `desktop-check`), `.github/workflows/ci.yml` (`desktop` job) |
| Tests | `cmd/blitz-desktop/{proxy,prefs}_test.go` (Go, `-race`); `web/desktop/src/{turns,prefs}.test.ts` (vitest); manual checks in MANUAL_VERIFICATION §37 |
| Depends on | [spec_service_021](spec_service_021.md) (all behaviour), [spec_workers_023](spec_workers_023.md), [spec_approvals_005](spec_approvals_005.md), [spec_sessions_017](spec_sessions_017.md) (rewind) |

## 1. Purpose and scope

`Blitz.app` is a window onto the per-user Blitz service: one tab per workspace, each with a conversation view and a workers view. It contains **no engine**. Every workspace, turn, approval, session and worker lives in the service (`blitz serve`), which keeps running — and keeps running workers — after the window closes. The app adds only what the service can't do for itself: detecting and installing the service, native dialogs, and a bridge from the web view to the service's Unix socket.

It keeps only its own settings (theme, layout, the workspaces it knows and how they are named) in `~/.blitz/desktop.json`. The design follows Material 3, with Google AI Studio as the reference for layout: a navigation drawer, a centred conversation with a large composer, and a run settings panel. Not yet (see §15): the REPL's slash commands, images, localisation.

## 2. Identity and packaging

- **DSK-01** Product name **Blitz**; macOS bundle `Blitz.app` (Wails project `name`), bundle identifier `dev.blitz.desktop`, executable `blitz-desktop` (Wails `outputfilename`), product version from `wails.json` `info.productVersion` (currently `0.1.0`, independent of the CLI's version), copyright "Ryan McGuinness, Apache License 2.0".
- **DSK-02** The app's executable must never be named `blitz`/`Blitz`: macOS file systems ignore case, and the bundled CLI (`Contents/MacOS/blitz`) would overwrite it. `make desktop` fails if the bundle's `MacOS` directory doesn't hold exactly `blitz-desktop` and `blitz` afterwards.
- **DSK-03** Minimum macOS 13.0: the build sets `-mmacosx-version-min=13.0`, and `LSMinimumSystemVersion` in `Info.plist`/`Info.dev.plist` says the same.
- **DSK-04** The CLI is bundled beside the app's executable (`Blitz.app/Contents/MacOS/blitz` on macOS; `build/desktop/bin/blitz` elsewhere) so the app can install the service without a separate CLI install. On macOS the bundle is re-signed ad hoc after adding it (adding a file breaks Wails's signature).
- **DSK-05** Windows packaging files (manifest, icon, version info, NSIS installer scripts) are Wails's templates and are not built or tested.

## 3. Architecture

```
┌──────────────── Blitz.app ────────────────┐        ┌──────── blitz serve ────────┐
│ Web view (React page, embedded dist/)     │        │ Connect API: Session,       │
│   ├─ Connect-ES clients ─► same origin ───┼─┐      │ Workspace, Worker services  │
│   └─ window.go.main.App.* (Wails binding) │ │      │ workspaces, turns, workers  │
│ Go (Wails v2):                            │ │ HTTP │                             │
│   ├─ asset server: page files             │ │ over │                             │
│   ├─ serviceProxy: /blitz.v1.* ───────────┼─┴─Unix─► ~/.blitz/run/blitz.sock    │
│   └─ App: ServiceStatus, InstallService,  │  socket└─────────────────────────────┘
│           ChooseWorkspace, Get/SavePrefs, │
│           OpenURL                         │
└───────────────────────────────────────────┘
```

- **DSK-10** Separate Go module (`cmd/blitz-desktop/go.mod`, cgo; WebKit on macOS, webkit2gtk on Linux) so the CLI module stays `CGO_ENABLED=0`. It imports `internal/server` only for the socket path, `BaseURL` and the socket HTTP client.
- **DSK-11** Window: title "Blitz", 1280×820, minimum 720×480. On macOS the title bar is hidden and inset (`mac.TitleBarHiddenInset`): the page draws the whole window in its theme's colours, leaves room for the window buttons at the top left of the drawer, and marks its top areas as drag regions (`--wails-draggable: drag`; controls in them are `no-drag`). One Go `App` value is bound to the page (`window.go.main.App`).
- **DSK-12** The page is built by Vite from `web/desktop` directly into `cmd/blitz-desktop/dist` (go:embed can only reach files under its package) and embedded with `//go:embed all:dist`. The build empties the directory and writes back the committed `.gitkeep` placeholder so the Go package compiles before any page build. Build outputs (`dist/*`, `wailsjs/`, `build/desktop/bin/`) are not committed.

## 4. The service bridge (proxy)

- **DSK-20** A web view can't open a Unix socket, so the Go asset server's handler forwards every request whose path starts with `/blitz.v1.` to the service socket (`$BLITZ_SOCKET` or `~/.blitz/run/blitz.sock`, resolved at start), rewriting the URL to the service's `BaseURL`. The page's Connect client uses `window.location.origin` as its base URL, so it talks to its own origin and needs no CORS.
- **DSK-21** Responses stream as they arrive (`FlushInterval: -1`): turn events and worker-run events must appear as they happen. (Whether WebKit streams through Wails's asset server can only be checked manually — MANUAL_VERIFICATION §37.)
- **DSK-22** If the service can't be reached, the proxy answers `503` with Connect's JSON error shape `{"code":"unavailable","message":"the Blitz service isn't answering"}`, which the page's client reports like any API error.
- **DSK-23** Any other path the handler receives is `404`; the page's own files are served by Wails from the embedded assets.
- **DSK-24** Security: the socket is owner-only ([spec_service_021](spec_service_021.md) SVC-02); the proxy adds no listener of its own, so nothing new is reachable from the network. Only the embedded page runs in the web view. Model and tool output becomes React elements (Markdown through `react-markdown`, raw HTML skipped; everything else escaped), never HTML; its links open only in the system browser through `OpenURL` (web and mail schemes only), and its images are never fetched, so output can't navigate the web view, inject script or make the app load anything.

## 5. Bound Go methods

| Method | Behaviour |
|---|---|
| `ServiceStatus() → {running, installed, socket, cli}` | `running`: the socket answers (1 s dial). `installed`: the login item file exists (`~/Library/LaunchAgents/dev.blitz.service.plist` on macOS, `~/.config/systemd/user/blitz.service` on Linux; always false elsewhere). `cli`: the `blitz` binary found beside the app's executable (`blitz.exe` on Windows), else on `PATH`, else `""`. |
| `InstallService() → output` | Runs `<cli> service install` (starts the service now and at every login) and returns its output; the error includes the command's combined output. Fails if no CLI is found. |
| `ChooseWorkspace() → dir` | Native directory dialog "Open a workspace" (may create directories); `""` when cancelled. |
| `GetPrefs() → Prefs` | The window's settings (§7), defaults when none are saved. An unreadable file is renamed `desktop.json.damaged` and reported once; the defaults are returned. |
| `SavePrefs(Prefs) → Prefs` | Normalizes (§7) and writes them atomically, owner-only (0600, directory 0700); returns what was saved. |
| `Notify(title, body, dir)` | A system notification (Wails's native notifications; macOS's needs the bundle identifier). The first call asks the user's permission; without it, or where notifications aren't available, it does nothing. A click on the notification unminimises and shows the window and emits `notification:open` with `dir`, which shows that workspace. |
| `OpenURL(url)` | Opens `http`/`https` links (with a host) and `mailto:` links in the system browser; anything else (`file:`, `javascript:`, custom schemes, relative) is refused. Never navigates the window. |

- **DSK-30** Outside the Wails window (a browser in development) the page uses stand-ins: preferences in `localStorage`, the status from a call to the service through the dev server's proxy, a prompt for the directory, links in a new tab. Calling the Go bindings themselves throws "not running inside the Blitz desktop app".

## 6. Design system

- **DSK-35** Material 3 tokens (`m3.css`): the colour roles (primary, secondary, tertiary, error, warning containers, and surface / surface-container-lowest…highest, outline, outline-variant, inverse, scrim, diff colours) in a light and a dark scheme derived from Google blue `#0B57D0`, with AI Studio-like neutral surfaces (dark surface `#131314`); shape (4–28 px, full), elevation 1–3, type scale (display, headline, title, label, body) in Google Sans/Roboto with system fallbacks and a monospace stack. Components use only tokens: buttons (filled, tonal, outlined, text, danger), icon buttons, chips, segmented buttons, outlined fields and selects, switches, sliders, lists, cards, menus, dialogs, a snackbar, progress indicators and badges, all with M3 state layers. Icons are Material Design icons as inline SVG paths (`@mdi/js`, tree-shaken), not a font.
- **DSK-36** Theme: the user chooses **System** (the default), **Light** or **Dark**; System follows the OS setting live (`prefers-color-scheme`). The choice is applied as `html[data-theme]`; density (**Comfortable** or **Compact**) as `html[data-density]`. Motion is reduced when the OS asks for it.
- **DSK-37** Narrow windows: under 980 px the drawer shows as a rail whatever the setting (its expand button is disabled with a tooltip); under 1180 px the run settings panel floats over the conversation; under 1100 px the view switcher shows icons only; under 800 px the workspace's description line is hidden.

## 7. Window settings and workspaces

- **DSK-40** `~/.blitz/desktop.json` (`Prefs`): `theme` (`system|light|dark`), `density` (`comfortable|compact`), `notifications` (`on|off`), `drawer` (`open|rail`), `run_settings` (panel shown), `show_thoughts`, `active` (a directory), and `workspaces`: every workspace the window has opened, each `{dir, name, description, color, open}`. Normalization: unknown values fall back to defaults; directories are cleaned, must be absolute, and appear once; open workspaces come first in tab order, then closed ones (at most 20, most recent first); `active` must be an open workspace, else the first open one.
- **DSK-41** The rules for the list are pure functions (`prefs.ts`), unit-tested: opening a known workspace keeps its name, description and colour and shows it (opening an open one just shows it); closing moves it to the front of the recent ones and shows the tab after it (else before); a closed workspace can be forgotten (open ones can't); open ones can be reordered. A workspace is shown by its name, else its directory's last element.
- **DSK-43** The Go side always sends `workspaces` as a list (`[]`, never `null`), and the page normalizes whatever it loads (`normalizePrefs`, unit-tested: every field of the right type, else its default; workspaces without a directory dropped). An error while drawing the page shows "Something went wrong" with the details, **Reload the window** and **Copy the details** (an error boundary), never a frozen or blank window.
- **DSK-42** Saving happens on every change, one save after another so the file ends with the latest.

## 8. Start-up, the drawer and closing

- **DSK-50** On start the page loads the preferences and asks `ServiceStatus`, showing a splash meanwhile. If the service isn't running it explains that the service holds workspaces and runs scheduled workers and keeps running after the window closes, and offers **Install and start the service** (or **Start the service** when the login item exists); it then polls the status every 2.5 s until the service answers. Without a CLI it says to install Blitz's CLI and run `blitz service install`.
- **DSK-51** When any call fails with `unavailable` (the proxy's answer when the socket doesn't answer, or the service shutting down), a banner "The Blitz service isn't answering. Reconnecting…" appears and the status is polled every 2.5 s; when the service answers again every open workspace reloads (sessions, settings) and a snackbar says "Reconnected". The open workspaces and the one shown are kept throughout.
- **DSK-52** Navigation drawer: a menu button (drawer ↔ rail), the app name, **Open workspace** (the directory chooser), the open workspaces, the recently closed ones, and **Settings**. Each open workspace shows an avatar (its initial on its colour), its name and its description (else directory), and on hover or when shown **Edit details** and **Close**. The avatar marks activity: a pulsing dot while a turn runs, a red `!` badge while an approval or question waits ("Waiting for you" replaces the description). A recent workspace reopens on click and has **Forget**. In the rail only avatars show. Rows are a button (show) beside separate action buttons, never buttons inside a button.
- **DSK-53** With no open workspace: a welcome page with **Open a workspace** and up to six recent workspaces as cards.
- **DSK-54** Open workspaces stay mounted while another is shown (hidden, not destroyed), so a running turn keeps streaming and its approval waits.
- **DSK-55** Closing a workspace: if a turn from this window runs in it, a dialog asks "Stop the turn and close?"; stopping aborts the turn and waits for it to end. The tab then closes (it moves to Recent), the service is asked to `CloseWorkspace` (a `TURN_RUNNING` refusal — another client's turn — is ignored: the tab still closes and the service keeps the workspace), and a snackbar offers **Undo** (reopen).
- **DSK-56** Workspace details dialog (from the drawer, the title, the ⋮ menu, or Settings › Workspaces): **Name** (placeholder: the directory's name), **Description**, **Colour** (8 swatches that work on both themes), the folder (read-only, with copy), **Save**/**Cancel**, and for an open workspace **Close workspace**. Changes apply to the drawer, the title bar and the welcome cards at once.

## 9. Settings

- **DSK-60** Settings dialog, in sections: **Appearance** — theme (segmented System/Light/Dark with icons), density, **Show thinking**, **Run settings panel**; **Workspaces** — every workspace the window knows with its avatar, name and directory, **Edit details**, and **Forget** for closed ones; **Service** — running or not, whether it starts at login (**Install**/**Reinstall**), the socket and the CLI path; **About** — the app's version (`wails.json` `productVersion`, set at build time) and where its settings live. The agent's settings are per workspace, in the run settings panel (§12).

## 10. Workspace view

- **DSK-65** Top bar (a drag region): the workspace's colour as a bar, its name (click: details dialog) and description (else directory), a segmented switch **Chat | Changes | Workers**, **Run settings** (toggles the panel; the choice is saved), and ⋮ (**Edit details**, **Close workspace**). Settings (agent, model, mode, effort) are loaded on open and after any change, turn or reconnection.

## 11. Conversation

API used: `SessionService.GetActiveSession`, `NewSession`, `ListSessions`, `LoadSession`, `RenameSession`, `RunTurn`, `Steer`, `Approve`, `Answer`, `GetUsage`, `Rewind`; `WorkspaceService.SetPermissionMode`, `SetSetting`.

- **DSK-70** On opening, show the workspace's active session, or start one. A session bar shows its title (click to rename; Enter saves, Escape cancels; "New chat" until named), **History** (a menu of this workspace's sessions with message count and last update; snapshots marked 📸) and **New chat**; both are disabled while a turn runs.
- **DSK-71** The conversation is a centred column (max 860 px). Prompts are right-aligned bubbles; messages sent while the agent worked are dashed bubbles "Sent while working"; a stop hook's request and an approved plan's go-ahead show as small notices, not bubbles (message `kind`). Model text renders as Markdown (§11a). A sub-agent's text is labelled with its name.
- **DSK-72** Hovering a prompt shows **Copy**, **Edit** (rewind code and conversation to before it and put the prompt back in the composer) and a rewind menu: **Code and conversation**, **Conversation only**, **Code only**, **Summarize from here**, **Summarize up to here**, **Copy into the composer**. Rewinds need the prompt's transcript index, known for saved prompts and learnt for new ones after each turn (the n-th prompt shown is the n-th saved; `assignPromptIndices`). A conflict (`UNDO_CONFLICT`) asks "Files changed since" — **Keep them** or **Overwrite them** (force). Rewinds are unavailable while a turn runs.
- **DSK-73** Turn rendering (`turns.ts`, unit-tested): streamed text grows one open entry and its final repeat closes it; final text that wasn't streamed is its own entry; a whitespace-only final text (the separator between runs of one turn) is added to the previous answer; thinking becomes thought entries by the same rules (shown only with **Show thinking**, folded: "Thoughts", or "Thinking…" while streaming); streamed copies of tool calls are left out; a tool result completes its call (by ID, else by name); `finished` closes open text and adds an error notice if the turn failed.
- **DSK-74** Tool calls show as rows with an icon for their kind (read, search, edit, shell, web, agent, task list, question, plan), the name, the argument summary (first of `path`, `command`, `url`, `query`, `question`, `pattern`, `agent`, `reason`, cut to 80), and a pending/✓/✗ status with the error; clicking expands the arguments and result as JSON (cut at 20 000 characters). Two or more consecutive calls fold into **Used N tools** (with their icons and the number that failed), open while one runs or one failed, folded otherwise.
- **DSK-75** "Working…" shows while a turn runs and nothing waits for the user. The view follows new output unless the user has scrolled up more than 80 px.
- **DSK-76** Task list (`TurnEvent.tasks`): a card above the composer, **Tasks** with "n of m done", a progress bar and the items (done struck through and ticked, in progress pulsing); it folds itself when all are done and can be hidden after the turn. A new session clears it.
- **DSK-77** Composer: a rounded field that grows up to 280 px; Enter sends, Shift+Enter adds a line (IME composition respected); chips for the **permission mode** (a menu of the five modes with their meaning; `bypass` in red; a refusal because the sandbox is off is explained), **reasoning effort** (Auto, Minimal … Max), and **Plan first** (the next prompt is sent as `/plan`: the agent plans and asks for approval before changing anything); a hint line; **Send** (disabled when empty), and while a turn runs **Stop** — the text then **steers** the agent (`Steer`), shown as a dashed bubble. An empty session shows "What are we working on in <workspace>?" with four suggestions that fill the composer.
- **DSK-78** After a turn: "This turn: <in> in · <out> out · context <ctx> · $<turn>" plus the session total ("session <tokens> tokens · $<total>", from `GetUsage` and each turn's `after`). **Stop** aborts the turn ("Stopped."); otherwise leftover steer messages are sent at once as the next turn, marked accepted. Errors show as a dismissable card with the service's message; `unavailable` errors also start reconnecting (DSK-51).

### 11a. Markdown

- **DSK-79** Model text, questions and summaries render with `react-markdown` and GitHub-flavoured Markdown (tables, task lists, strikethrough) to React elements; raw HTML is skipped, never rendered. Links show their target on hover and open with `OpenURL` after a click (never in the window). Images are never loaded: they show as links. Fenced code blocks show their language and a **Copy** button and are syntax-highlighted (§11b); inline code is tinted; wide tables scroll.

### 11b. Syntax highlighting

- **DSK-79a** Code blocks and diff lines are highlighted with highlight.js grammars through `lowlight` (the common set, about 35 languages), which returns an element tree rendered as React elements — never an HTML string. A block's grammar comes from its fence's language (with aliases: `sh`→bash, `ts`→typescript, `yml`→yaml, `golang`→go, `proto`→protobuf, …); a diff's from the file name (extension, or `Makefile`). Unknown languages, and code over 100 000 characters, stay plain text. Diff lines are highlighted one at a time after their `+`/`−`/space sign (shown in its own column), over the added/removed tint. Token colours are theme tokens (`--hl-*`) with a light and a dark set.

### 11d. Images

- **DSK-78b** Images can be pasted into the composer, dropped anywhere on the conversation (an overlay "Drop images to attach them" shows while dragging files), or chosen with the attach button. Only images up to 20 MB are taken (others are refused in a snackbar); each uploads at once (`AddImage`) and shows as a chip with its thumbnail (a local object URL), name, and the service's dimensions and size ("scaled down" when the service resized it) or its error; chips can be removed. **Send** waits until uploads finish; the turn carries the uploaded images' IDs (`Turn.image_ids`), and the prompt's bubble shows them. With images turned off for the workspace (`imagesEnabled` false) the button is hidden and pasting or dropping explains why. Files dropped elsewhere in the window are ignored, so the web view never navigates to a dropped file. Rules in `attachments.ts`, unit-tested.

### 11c. Notifications

- **DSK-78a** With **Notifications** on (the default; Settings › Appearance; `notifications` in `desktop.json`), the window notifies when an approval or a question waits ("<workspace>: approval needed" with the request, "<workspace>: the agent asks" with the question), or when a turn that took at least 10 s finishes ("<workspace>: done" with the start of the answer, or "the turn failed" with the error) — but only while the user isn't looking at that conversation: the window isn't focused, or another workspace or view is shown (`shouldNotify`, unit-tested). Bodies are cut to 180 characters. In a browser (development) the web's Notification API stands in.

## 12. Approvals, questions and plans

- **DSK-80** An approval request shows as a card: "Allow this?", `<tool>` wants to: <detail>, the diff coloured per file (§14), and **Allow once** (focused, filled), **Allow <scope> this session**, **Always allow <scope>** (when the request has a scope), **Deny** (outlined, red).
- **DSK-81** A question shows as a card "The agent asks" with the question as Markdown (up to half the window, scrolling), one button per option (the first filled), and a field for another answer ("feedback on a plan, say"). A plan review (`exit_plan_mode`) is such a question: the plan renders as Markdown and typed feedback makes the agent revise it.
- **DSK-82** One request shows at a time (the latest); it disappears once answered or when the turn ends. Late answers fail with the service's `UNKNOWN_REQUEST`.

## 13. Run settings panel

API used: `WorkspaceService.ListAgents`, `SetAgent`, `SetModel`, `GetModelSettings`, `UpdateModelSettings`, `SetSetting`, `SetPermissionMode`, `ListLocales`, `SetLocale`, `ListPermissionRules`, `AddPermissionRule`, `RemovePermissionRule`, `ListApprovals`, `RevokeApprovals`; `SessionService.GetUsage`, `Compact`.

- **DSK-85** A 340 px panel beside the conversation (§6 for narrow windows) with collapsible sections: **Agent and model** — agent (with pinned models), model (a field with suggestions: pinned models and models with settings; applied on Enter or leaving the field); **Thinking** — reasoning effort for the session, thinking budget for the model; **Generation · <model>** — temperature and top P (slider with a number, "default" when unset), max output tokens (placeholder: the global value), **Reset to defaults** (settings the provider ignores are reported); **Behaviour** — permission mode (with its meaning), agency, and the language the model replies in; **Permission rules** — the rules with their effect (coloured), source, and remove; a form to add one (effect, rule, "Save to the configuration"); **Standing approvals** — each with its kind, subject and scope, revoke one or all; **Context** — context size, tokens sent and received, cost, and **Compact the context**. Every change reports failure in a snackbar and reloads the panel.

## 14. Changes and workers

- **DSK-90** Changes view: **This session** (the agent's changes, `GetDiff`) or **Git** (`GetDiff git`); a count of files and lines added and removed; **Refresh**; **Undo last turn** (a conflict asks **Keep them** / **Undo anyway**). A side column lists the files (name, path, +/−) and the turns that changed files (a timeline from `ListCheckpoints`); the main pane shows the agent's latest summary of its work (its last answer, as Markdown, foldable — the walkthrough) above the selected file's diff. Diffs are parsed per file (`parseDiff`, unit-tested: file headers, `/dev/null` for created and deleted files, hunks, "too large" notes) and coloured: added and removed lines on tinted backgrounds, hunk headers in the secondary colour.
- **DSK-91** Workers view (behaviour as before): a list with state and schedule; the selected worker's description, state chip, schedule (with next run when enabled), agent, model, permissions as chips, limits, content hash and problems; **Enable as shown** (enables the displayed hash; `HASH_MISMATCH` if edited since), **Run now** (with the run's events live), **Disable**; the latest 20 runs with an icon per status, cost, time, session, refusals and error.

## 15. Build, test and CI

- **DSK-95** `make desktop` (needs pnpm; Linux also webkit2gtk): installs page dependencies from the lockfile, runs the pinned Wails CLI (`go tool -modfile=../../tools/go.mod wails build -clean`, which builds the page with `pnpm run build` = `tsc --noEmit && vite build`), then bundles the CLI (DSK-04) and re-signs on macOS. Output: `build/desktop/bin/Blitz.app` on macOS, `build/desktop/bin/blitz-desktop` plus `blitz` elsewhere.
- **DSK-96** `make desktop-check`: `pnpm test` (vitest), the page build, then `go vet` and `go test -race` in the desktop module. CI's `desktop` job (macOS) runs `make desktop-check` and then `make desktop`.
- **DSK-97** Tests: the proxy against a real `internal/server` service (API calls, a streamed turn and its approval, the `unavailable` error); the preferences file (defaults, normalization, owner-only atomic writes, a damaged file set aside) and `OpenURL`'s scheme check; the page's pure logic (turn events, message kinds, prompt indices, task lists, diff parsing, the workspace list rules, theme resolution, colours).
- **DSK-98** Development: `pnpm dev` serves the page and proxies `/blitz.v1.*` to the service socket (`$BLITZ_SOCKET` or `~/.blitz/run/blitz.sock`). `?fake` in a development build uses an in-page fake service (`dev/fake.ts`, left out of production builds) whose turns script every state — streamed Markdown, thinking, tool calls, a task list, an approval, a plan review — for working on the page and taking screenshots without a service or a model.
- **DSK-99** Dependencies are pinned: React 19, Connect-ES 2, protobuf-es 2 (`protoc-gen-es` generates `src/gen` via `make proto`), react-markdown 10 with remark-gfm 4, lowlight 3 with hast-util-to-jsx-runtime 2, `@mdi/js` 7, Vite 8, TypeScript 5.9, vitest 5; pnpm 10.24 via `packageManager`. The page is one bundle (about 610 kB, 190 kB compressed), loaded from the app's binary.

## 16. Known gaps

Not implemented; each is tracked in [spec_backlog_026](spec_backlog_026.md):

- **Slash commands** in the composer and a command palette (BL-DSK-10/11); `/btw`, `/search`, snapshots by name, sessions from other workspaces.
- **Localisation**: the page's strings are English (BL-DSK-40).
- **Workers**: no refresh on file changes; a run's session doesn't open in the conversation (BL-DSK-50).
- **Release**: signing, notarisation, a release job, Linux and Windows packages (BL-DSK-60/61).
