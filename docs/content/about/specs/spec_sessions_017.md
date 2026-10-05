---
title: "017 · Sessions"
weight: 17
---

*Sessions, transcripts, snapshots and session search* (`spec_sessions_017`)

| | |
|---|---|
| Status | Implemented. Reverse-engineered from `53f8c53` (2026-09-24) and kept with the code since; its paths and names checked 2026-09-27 (`//tools/specs`). |
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
- **SES-15** `usage` keeps the session's usage so far (`api.Usage`: calls, input, cached and cache-write tokens, output, the last prompt's size, cost, whether every call was priced): the engine saves it after every model call and compaction (`runtime.WithUsageStore`, `Storage.SetUsage`) and restores it the first time a session's usage is asked for in a process (`Storage.Usage`, `UsageTracker.Seed`), so `/cost`, `/context` and the desktop's usage footer go on after a restart or `--resume`, and new calls add to it. A session the store doesn't have (a worker run's, kept in its own store) is left alone. A snapshot, or a session started from one, gets the source's context size only: what was spent is the source's.
- **SES-16** A turn's transcript messages go to the session the turn runs in (`AppendTo(id, msg)`), not to whichever session the storage has active: a client may run a turn in a session another client, or the service before a restart, opened. A non-aside turn also makes its session the active one first (`Load`), so rewind points, renaming and snapshots act on it. *Fixed 2026-09-29:* after the service restarted, a conversation continued in the chat the desktop still showed was kept in the model's history but not in its transcript ("no active session"), so it reopened empty.
- **SES-18** `/cd` moves a session to another workspace (`Storage.Move`, `Workspace.MoveSession`, `SessionService.MoveSession`): it becomes that workspace's (listed and continued there, no longer in the old one) and records where it came from and how many messages it had (`moved_from`, `moved_at`). Prompts before the move aren't rewind points there (a rewind to one says to `/cd` back), and the agent's next prompt says the workspace changed (`workspace-changed` context); the new workspace's instructions replace the old in its system instructions rather than being added as a message (PAR-SES-42 as built). The conversation history carries on unchanged.
- **SES-17** A new session is saved with its first message: `CreateSession` keeps it in memory (active, not on disk), and the first message written to it (or a rename, or a snapshot of a session with history) writes its metadata. So chats opened and never used (each time a workspace opens, every "new chat") leave nothing behind, and one replaced unused is dropped. A message for a session the store has no metadata for (one a process since restarted opened and never saved) creates it under that ID. Listing leaves out sessions with no messages and no history (empty chats saved by earlier versions), and the first workspace a process opens deletes them (`RemoveEmpty`: no title, no messages, no transcript, no history; never the active session or a named chat); a snapshot of a new, unused chat has "nothing to save yet".

- **SES-19** Goals (PAR-SES-30): `SetGoal(condition)` gives the active session a goal (`Goal`, `ClearGoal`; `SessionService.SetGoal`, `GetGoal`, `ClearGoal`; `NO_GOAL`), kept while the workspace is open. After each successful non-aside turn (after plans carried out and stop hooks), a judge model (the auto mode's reviewer, else the session's; its tokens count in `/cost`) reads the goal and the last ten messages and answers `met`, `unmet` (with what to do next) or `impossible`. Met or impossible: the goal is removed and a notice says why. Unmet: the agent is sent on in the same turn with the reason, the goal and the next step, recorded as `(goal) …` (kind `hook`), until `blitz.goal_max_continues` (20) continuations, when the goal is removed with a notice. Each verdict is a notice ("Goal not met yet (2/20): …"). An answer that isn't a verdict stops the turn with an error notice and keeps the goal. The turn's `max_turns`, cost and time limits apply across the continuations; Ctrl+C stops them.

- **SES-20** Fork (PAR-SES-10, BL-SES-01/02): `ForkSession(turn)` (`/fork [n]`; `SessionService.ForkSession`) copies the active session (`Storage.Fork`: transcript, event log, metadata with `from`) and cuts the copy where prompt turn `n+1` starts (its recorded event count): the transcript to that prompt, the event log to that many events (`TruncateSession`), so a compaction past the cut goes too. `n` 0 or past the last turn copies it all; a negative one is `ErrNotRewindPoint`, and cutting at a prompt recorded without an event count is `ErrCantRewindConversation`; a running turn is `ErrSessionBusy`. The copy becomes active (session_end and session_start hooks with reason `fork`); the source is unchanged and listed as before. `--fork` with `--resume` or `--continue` continues in a copy of the session picked.
- **SES-21** Export (PAR-SES-11): `ExportSession(id)` (the active session when empty; `SessionService.ExportSession`; `/export [file]`, written relative to the workspace, default `<title>.md`; `blitz sessions export <id> [-o file]`, from the files, with no workspace opened) is Markdown: the title, the ID, workspace, start and source; then from the event log each prompt (`## You · <time>`), each agent's answer (`## <agent>`, thoughts left out), each tool call as `` - `tool` {arguments} `` (JSON, 200 characters) with its result below (`→ error: …` or the result's JSON, 200 characters), and a line where earlier conversation was summarised. Without an event log, the transcript's messages. Configured secrets are masked (`SecretRedactor`).
- **SES-23** Deleting (`Storage.Delete`, `Workspace.DeleteSession`; `SessionService.DeleteSession`): a saved session or snapshot, by ID, any workspace's, loses its metadata (first, so it stops being listed), transcript and event log, and the engine forgets its in-memory history (`Engine.ForgetSession`). The session open in this storage, or active in another in the process (`owners`: another workspace's, a background run's), is refused (`api.ErrSessionOpen`); an unknown or invalid ID is `api.ErrSessionNotFound`. Sessions copied from it keep their files (their `from` names a session that's gone), and file checkpoints, which are the workspace's undo history, stay. The desktop's History deletes from a row (DSK-70).
- **SES-22** `--name <title>` names the new session (with `--fork`, the copy); on a resumed one it's a usage error. `/resume` without an argument is the picker (TUI), filtered as you type over titles and details.
- **SES-24** *2026-10-04 (the owner's request).* A session records what started it (`origin`, in its metadata and `SessionInfo`): `worker` for a worker's run (`RunWorker`, scheduled or not), `background` for a detached run (`RunDetached`, `blitz --bg`), none for a chat. A worker's run saved before origins were kept is still known by the title `RunWorker` gives it (`⏰ <worker> <date>`); a renamed one keeps its origin. Background tasks never become saved sessions.

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
- **SES-49** One storage writes a session's metadata: the one that has it active in the process (`owners`, set as a storage creates, loads or branches a session). Another storage asked to record its last turn or usage, or to read them (the workspace's, for a worker run's session kept by the run's own storage), hands the call to the owner, so their read-modify-writes of the file can't drop each other's fields (BL-WK-20).

## 7. Session search

- **SES-50** `SearchTerms` splits a query into words and `"quoted phrases"`. `Search` matches case-insensitively and literally (no stemming), skipping earlier `/search` commands; it keeps at most `limit` (12) messages — most distinct terms first, then most recent — presented oldest first, each as an excerpt of ~300 chars around the hit cut at rune boundaries with "…". It returns the total match count.
- **SES-51** The transcript keeps what compaction removed from the model's context, but not tool calls or their output; other sessions and snapshots are not searched.

## 8. Commands

`/session list [--all]`, `/session new`, `/session load <id|name>`, `/resume <id|name>`, `/session save <name> [--force]`, `/rename <name>`. On exit the REPL prints `blitz --resume=<id>` for the session. Usage totals are not persisted with the session ([spec_engine_016](spec_engine_016.md) ENG-82).
