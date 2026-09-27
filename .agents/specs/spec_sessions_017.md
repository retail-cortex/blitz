# spec_sessions_017 — Sessions, transcripts, snapshots and session search

| | |
|---|---|
| Status | Implemented (reverse-engineered from `53f8c53`) |
| Source | `pkg/engine/session/{session,snapshot,adkstore,search}.go`; `pkg/engine/sessions.go` |
| Tests | `pkg/engine/session/*_test.go`, `pkg/engine/sessions_test.go`, `apps/cli/internal/tui/snapshot_test.go`, `rename_test.go` |
| Depends on | [spec_config_002](spec_config_002.md) |
| Used by | [spec_engine_016](spec_engine_016.md), [spec_workspace_018](spec_workspace_018.md) |

## 1. Purpose

A session is one conversation. Two stores persist it, both owner-only under `session.storage_dir` (`~/.blitz/sessions`, dir 0700, files 0600 — transcripts may contain secrets):
- the **transcript** (what the user typed and the model's final replies), for listing, recaps, titles and `/search session`;
- the **ADK event log** (every final event, including tool calls and compaction summaries), so the model's context resumes exactly in a later process.

## 2. Identity

- **SES-01** IDs: `session-YYYYMMDD-HHMMSS-<8 hex>` (UTC). Any ID used as a file name must match `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$` (`ErrInvalidID`), so it can't escape the directory.
- **SES-02** Sessions are scoped to a canonical workspace directory. Listing is per workspace unless `--all`. Sessions saved before scoping (no workspace) are adopted by the workspace that resumes them.

## 3. Transcript store

- **SES-10** Per session: `<id>.meta.json` (metadata, rewritten atomically via temp + rename) and `<id>.jsonl` (messages appended, so adding a message costs O(message)). A torn last line (crash mid-append) is skipped. Legacy single-file sessions are still read.
- **SES-11** Metadata: `id, title, agent, created_at, updated_at, message_count, workspace, last_turn{traceparent,index}, name (snapshot), from`. Messages: `role (user|model|tool), content, timestamp`.
- **SES-12** Title: a new session is named after its first user prompt — first non-empty line, whitespace collapsed, max 60 runes ending "…". `/rename <name>` sets it (empty refused). Titles show in `/session list` and the terminal window title.
- **SES-13** `List` returns metadata sorted by update time, newest first, without reading message bodies.
- **SES-14** `last_turn` links traces across processes ([spec_engine_016](spec_engine_016.md) ENG-91).

## 4. ADK event log

- **SES-20** `PersistentService` implements the ADK session service in memory and appends every **final** event as JSON to `<id>.events.jsonl`. On `Create`/`Get` of a known ID after restart, stored events are replayed. `Delete` removes the log.

## 5. Selecting a session (`OpenSession`)

- **SES-30** No `--resume`/`--continue`: a new session (titled by its first prompt).
- **SES-31** `--continue`, or `--resume` with no value (`latest`): the most recent **non-snapshot** session of this workspace; none → `ResumeError` "no saved sessions for <dir> (use --resume <id> for a session from another directory)".
- **SES-32** `--resume <id>` resumes that session in place (any workspace; the REPL warns if it belongs to another). `--resume <snapshot-name>` starts a new session copied from the snapshot ("branched"). Unknown → `ResumeError`.
- **SES-33** The audit log context is set to the chosen session and workspace.

## 6. Snapshots

- **SES-40** `/session save <name> [--force]` saves a named copy of the active session — transcript, metadata and event log — under a new ID with `name` and `from = <source>`. The source stays active and unchanged. An empty session can't be saved. Names are unique across workspaces; an existing name is `ErrNameTaken` unless `--force`, which replaces it after the new one is written.
- **SES-41** Names follow the ID rules, may not start with `session-` (never mistaken for an ID) and may not be `latest`.
- **SES-42** Snapshots are **never continued in place**: opening one by name or ID (`/session load`, `/resume`, `--resume=`) copies it to a new session with `from = <snapshot>`. `--continue` skips snapshots. Snapshots are marked 📸 in `/session list`.

## 6a. Rewind

- **SES-45** Transcript messages carry `kind` (`""` prompt, `steer`, `hook` for a stop hook's request) and, on prompts, `events`: how many events the session's event log held before the prompt. Only prompts (`role user`, no kind) are rewind points (PAR-SES-03); prompts recorded before this have no `events` and can only have their files rewound (`ErrCantRewindConversation`).
- **SES-46** `Rewind(index, mode, force)` on the active session, refused while a turn runs in it (`ErrSessionBusy`, counted per session in `Run`) and for an index that isn't a prompt (`ErrNotRewindPoint`); modes `both`, `conversation`, `code`, `summarize_from`, `summarize_up_to` (`ErrUnknownRewindMode`). Files first (checkpoints' `Rewind`, FS-68; a conflict refuses the whole rewind unless `force`; nothing to restore is fine), then the conversation: the event log is cut to the prompt's `events` (`PersistentService.Truncate` rewrites the file atomically and rebuilds the in-memory session; other services by delete, create and re-append), the transcript to the prompt's index (atomically), and the dropped prompts' checkpoints are detached (FS-68). The prompt's text is returned. Summarize modes run `Engine.CompactAt` on the events from the prompt on, or before it, and change neither files nor transcript.
- **SES-48** The API's `Message` carries `kind`, so clients tell prompts (rewind points, at their index in `messages`) from steer, hook and plan messages.
- **SES-47** `RewindPoints` lists the prompts oldest first with time, whether the conversation can be rewound, and the files changed by the checkpoints between each prompt and the next. API: `SessionService.ListRewindPoints` and `Rewind` (reasons `SESSION_BUSY`, `NOT_REWIND_POINT`, `CANT_REWIND_CONVERSATION`, `UNKNOWN_REWIND_MODE`, `UNDO_CONFLICT`).

## 7. Session search

- **SES-50** `SearchTerms` splits a query into words and `"quoted phrases"`. `Search` matches case-insensitively and literally (no stemming), skipping earlier `/search` commands; it keeps at most `limit` (12) messages — most distinct terms first, then most recent — presented oldest first, each as an excerpt of ~300 chars around the hit cut at rune boundaries with "…". It returns the total match count.
- **SES-51** The transcript keeps what compaction removed from the model's context, but not tool calls or their output; other sessions and snapshots are not searched.

## 8. Commands

`/session list [--all]`, `/session new`, `/session load <id|name>`, `/resume <id|name>`, `/session save <name> [--force]`, `/rename <name>`. On exit the REPL prints `blitz --resume=<id>` for the session. Usage totals are not persisted with the session ([spec_engine_016](spec_engine_016.md) ENG-82).
