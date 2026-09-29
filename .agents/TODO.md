# Blitz TODO

One list, in the order to work through it. Sizes: S (hours), M (a day or
two), L (a week or more). Each item says where the work is and what "done"
means. Written 2026-09-28; the backlog spec
(`docs/content/about/specs/spec_backlog_026.md`) keeps the long tail.

## P0: broken, misleading, or unverified now

1. ✅ *Done 2026-09-28: run 36493663203, green on macOS and Linux (`db7e5fa` added a manual trigger).* **Run CI on everything pushed today (S).** Every commit since the
   release candidate went up with `[skip ci]` or had its run cancelled:
   the ADC/OAuth sign-in, Claude on Vertex AI, the permission rules, the
   stream keepalive, the thought-signature fix. Only local tests (Linux)
   have run them; the macOS build and tests haven't run at all, and the
   macOS disk cache was evicted (the first run builds cold). Done: a green
   run on macOS and Linux, and the docs site rebuilt (the safety and
   configuration guides changed).

2. ✅ *Done (`74b759f`).* **An unavailable model silently answers "Done." (S).** When the
   configured model can't be built (no credentials, a bad setting), the
   workspace runs turns on a placeholder model that replies "Done."
   instead of failing (`pkg/engine/workspace.go`, `runtime.NewMockLLM`).
   Seen 2026-09-28 with a Gemini setting the API refused. Done: a turn
   with no working model fails with the model's error (after
   `RetryModel`), in the desktop, the CLI and worker runs.

3. ✅ *Done (`62fbb78`); to confirm in the app.* **Settings › Service hangs after reinstalling the service (S).** The
   status doesn't refresh after **Reinstall**; it stays as it was until
   the dialog is reopened. Likely the check runs once, before the new
   service is up (`apps/desktop/web/src/SettingsDialog.tsx`, `Service`;
   `desktop.ts` `installService`). Done: after install, restart or stop,
   the status polls until it settles (with a timeout and an error if it
   doesn't).

4. ✅ *Done: `TestWorkspacesKeepTheirOwnSettings` (daemon). Agent model pins (`/pin_model`, `/unpin`) are now the workspace's, over the global ones (`/unpin` masks a global pin with `agent = ""`); `/locale` and `/model_settings` stay global, which suits them.* **Prove each workspace gets its own configuration (S).** Already the
   design: the service opens each workspace as its own engine with
   `config.LoadWorkspace` (the global settings plus
   `~/.blitz/workspaces/<name>-<sha256 of the path>/.env.toml`), keyed by
   the canonical path; sessions share one folder, tagged by workspace;
   remembered approvals share one file, keyed by workspace and command.
   What's missing is a test that proves it end to end, and a decision on
   anything still shared. Done: a service test opening two workspaces with
   different providers, models and permission rules, switching between
   them, and checking each keeps its own; any leak found is fixed or
   written down as intended.

## P1: small, high-value improvements

5. ✅ *Done; to confirm in the app.* **Copy an agent's response, as text or Markdown (S).** A copy button
   on each answer in the conversation (`Conversation.tsx`, `EntryView`):
   **Copy** (the rendered text) and **Copy as Markdown** (the source), as
   other AI tools do; also for a whole turn. Done: both copy exactly what
   the user expects, with a snackbar; keyboard reachable.

6. ✅ *Done.* **The settings file editor: highlighting, a Validate button, and a
   reference (M).** Settings › Settings file is a plain text area. Use the
   editor the Files view already has (CodeMirror, `files/codemirror.ts`)
   with TOML highlighting; add **Validate**, which checks without saving
   (a new `ConfigService.CheckConfigFile`: TOML errors with their line,
   unknown settings, invalid permission rules, plain-text keys); and a
   reference of every setting, with its type, default and meaning, as a
   tooltip on hover and a searchable panel. The reference should be
   generated from the config structs' doc comments, so it can't drift.
   Done: errors are shown at their line before saving; every setting in
   `pkg/config` is in the reference.

7. ✅ *Done: the service reports a replaced program (any build, stamped or not), and the app offers to restart it; stamped dirty builds carry a hash of the changes. The `.deb` doesn't restart the service itself: a package upgrade would end turns in progress without asking.* **Local builds that say they're local, and installs that restart the
   service (S).** A build with uncommitted changes reports the same
   version as its commit, so the app's "service version differs" check
   can't tell an old service from a new one (the cause of today's
   "still broken after installing" confusion). Stamp `<commit>-dirty`
   (`build/` stamping); have the `.deb` restart a running user service
   after install, or have the app offer to. Done: installing a new local
   build and reopening the app never talks to the old service silently.

8. **Vertex AI quota errors say "quota" (S).** Backlog BL-ENG-20.
   `429 RESOURCE_EXHAUSTED` reads as "rate limited"; show Google's
   message and the quota to raise, in `doctor --online` (in full, not its
   first line), the "model isn't available" note and turn errors.

9. **`thinking_budget` on current Claude models (S).** Backlog BL-ENG-21.
   A budget above 0 sends `budget_tokens`, which Opus 4.7 and later,
   Sonnet 5 and Fable refuse (400); `0` sends thinking disabled, which
   Opus 5.5 and Fable refuse. Map it to effort (or report it as
   unsupported) on those models.

10. **A log viewer in Settings (M).** Blitz's logs are JSON lines in
    `~/.blitz/logs/blitz-<date>.jsonl`; today the only way to read them is
    a terminal. A Settings section (or a Service section tab): today's
    log, newest first, with level filters, search, the day to show, and
    **Open the folder**. Needs a way to read the service's logs (a new
    `ServiceService.ReadLog`, or reading the files from the desktop side,
    which shares the user's home). Done: an error the user saw can be
    found there in a few seconds.

11. **`doctor --online` still says "use --online to confirm" (S).** The
    credentials line says it even when `--online` is given.

## P2: features

12. **The Blitz service in the system tray, on every OS (M–L).** An icon
    with the service's state and, on right click, **Start**, **Stop**,
    **Restart**, **Open Blitz** and **Logs** (item 10). Wails v2 has no
    tray API: it needs a small separate tray process (e.g. `fyne.io/systray`,
    which covers macOS, Windows and Linux) started at login beside the
    service, talking to it over the socket, or moving to Wails v3, which
    has one. Linux needs a StatusNotifier/AppIndicator host (GNOME only
    with the AppIndicator extension; say so, and degrade to no icon).
    Done: the icon shows the service's state within seconds of a change
    and every menu action works on macOS and Ubuntu 24.04.

13. **Usage that survives a restart (M).** Backlog BL-ENG-10, BL-ENG-11.
    Token counts, context size and cost live only in memory, so
    `--resume`, a service restart or a worker run across restarts start
    at zero, and `--max-cost-usd` and compaction thresholds are wrong
    after resuming. Save the session's cumulative usage in its metadata
    after each model call (`session.Store`), restore it when a session
    opens, and record it in worker runs.

14. **The attached CLI's background processes and `!cmd` audit (M).**
    Backlog BL-SVC-01, BL-SVC-02. Attached to the service, the CLI can't
    list or stop background processes (`Processes()` is nil) and doesn't
    audit `!cmd` (`AuditShell` does nothing). Add `ListProcesses`,
    `GetProcessOutput`, `KillProcess` and `AuditShell` to
    `WorkspaceService`, scoped to the session.

15. **Faster Linux sandbox masking (M).** Backlog BL-SH-01. bubblewrap's
    masking walks the writable roots before every command (about 100 ms
    on a 10,000-file workspace, 3 s under `-race`), so CI runs parallel
    tests without the sandbox. Cache the mask per root (by mtime, or
    inotify), skip known build and cache directories when the rules
    allow, and turn the sandbox back on in CI's parallel tests.

## P3: large, needing a design first

16. **Project configuration with a trust boundary (L).** Teams can't
    commit MCP servers, commands, permission presets or hooks to a
    repository. Add `.blitz/settings.toml` (and `.local.toml`) per the
    decisions in `spec_parity_027` §12 and §2.4: settings that only
    tighten apply at once; anything that runs code (hooks, command MCP
    servers, allow rules) needs a one-time confirmation recorded in
    `~/.blitz/trust.json` by path and the file's SHA-256, asked again
    when it changes. Builds on the per-workspace settings and permission
    scopes that exist now.

17. **Background sub-agents (L).** `invoke_agent` blocks the turn. Add
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
