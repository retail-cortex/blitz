# Blitz TODO

One list, in the order to work through it. Sizes: S (hours), M (a day or
two), L (a week or more). Each item says where the work is and what "done"
means. Written 2026-09-28, reordered 2026-09-29; the backlog spec
(`docs/content/about/specs/spec_backlog_026.md`) keeps the long tail.

## P1: the desktop app, next

1. ✅ *Done.* **Full-width window (XS).** The conversation is capped by
   `.chat-column { max-width: var(--content-width) }`. Let it fill the
   window (full width by default, or an Appearance option). Done: a
   maximised or full-screen window uses its width.

2. ✅ *Done (WS-21a, DSK-77a).* **`@` mentions in chat (M).** Typing `@` in the composer completes
   workspace files and folders (fuzzy, as Go to file does); a mentioned
   file goes to the model with the prompt (its content, up to a cap; above
   it, the path and a note to read it), a folder as its listing, an image
   as an image. The same in the desktop and the CLI (which today only
   takes `@image` mentions). Done: `@` completes, and the model answers
   from the file without reading it first.

3. ✅ *Done (FIL-53).* **Add to context from the Files view (S).** The file bar: **Add to
   context** and **New conversation about this file** (icon buttons). The
   tree's right-click menu: **Add file to context**, **Add folder to
   context**, **Start a conversation about this file**. Adding puts a chip
   in the composer, as `@path` would (item 2); a new conversation opens a
   new session with the file attached and the cursor in the prompt.

4. ✅ *Done (FIL-54; pdf.js, which also renders in WebKitGTK).* **Preview pane: Markdown, images, PDF (M).** Markdown rendered (a
   Source / Preview toggle, the chat's renderer); images shown; PDFs
   viewed (WebKitGTK has no PDF viewer: pdf.js, a new dependency and
   notice). Done: each opens from the tree as a preview.

5. **Creating workers (M).** A **New worker** dialog with a field for
   each `WORKER.md` frontmatter setting and the prompt, checked before
   saving (a new `WorkerService.CreateWorker`). New workers go in
   `.agents/workers/<name>/WORKER.md`; that path is searched by default
   beside `workers/`, which still works. Done: a worker made in the
   dialog lists, runs and validates like a hand-written one.

## P2: features and the rest

6. **The Blitz service in the system tray, on every OS (M–L).** An icon
    with the service's state and, on right click, **Start**, **Stop**,
    **Restart**, **Open Blitz** and **Logs** (Settings › Logs, done). Wails v2 has no
    tray API: it needs a small separate tray process (e.g. `fyne.io/systray`,
    which covers macOS, Windows and Linux) started at login beside the
    service, talking to it over the socket, or moving to Wails v3, which
    has one. Linux needs a StatusNotifier/AppIndicator host (GNOME only
    with the AppIndicator extension; say so, and degrade to no icon).
    Done: the icon shows the service's state within seconds of a change
    and every menu action works on macOS and Ubuntu 24.04.

7. **Usage that survives a restart (M).** Backlog BL-ENG-10, BL-ENG-11.
    Token counts, context size and cost live only in memory, so
    `--resume`, a service restart or a worker run across restarts start
    at zero, and `--max-cost-usd` and compaction thresholds are wrong
    after resuming. Save the session's cumulative usage in its metadata
    after each model call (`session.Store`), restore it when a session
    opens, and record it in worker runs.

8. **The attached CLI's background processes and `!cmd` audit (M).**
    Backlog BL-SVC-01, BL-SVC-02. Attached to the service, the CLI can't
    list or stop background processes (`Processes()` is nil) and doesn't
    audit `!cmd` (`AuditShell` does nothing). Add `ListProcesses`,
    `GetProcessOutput`, `KillProcess` and `AuditShell` to
    `WorkspaceService`, scoped to the session.

9. **Faster Linux sandbox masking (M).** Backlog BL-SH-01. bubblewrap's
    masking walks the writable roots before every command (about 100 ms
    on a 10,000-file workspace, 3 s under `-race`), so CI runs parallel
    tests without the sandbox. Cache the mask per root (by mtime, or
    inotify), skip known build and cache directories when the rules
    allow, and turn the sandbox back on in CI's parallel tests.

## P3: large, needing a design first

10. **Project configuration with a trust boundary (L).** Teams can't
    commit MCP servers, commands, permission presets or hooks to a
    repository. Add `.blitz/settings.toml` (and `.local.toml`) per the
    decisions in `spec_parity_027` §12 and §2.4: settings that only
    tighten apply at once; anything that runs code (hooks, command MCP
    servers, allow rules) needs a one-time confirmation recorded in
    `~/.blitz/trust.json` by path and the file's SHA-256, asked again
    when it changes. Builds on the per-workspace settings and permission
    scopes that exist now.

11. **Background sub-agents (L).** `invoke_agent` blocks the turn. Add
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

