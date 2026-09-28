---
title: "006 · Filetools"
weight: 6
---

*File sandbox, file tools, patches and checkpoints* (`spec_filetools_006`)

| | |
|---|---|
| Status | Implemented. Reverse-engineered from `53f8c53` (2026-09-24) and kept with the code since; its paths and names checked 2026-09-27 (`//tools/specs`). |
| Source | `pkg/engine/tools/workspace.go`, `pathpolicy.go`, `pathlock.go`, `file_ops.go`, `file_edit.go`, `patch.go`, `grep.go`, `checkpoint.go`, `diff.go` |
| Tests | `pkg/engine/tools/workspace_test.go`, `sandbox_paths_test.go`, `file_tools_test.go`, `edit_race_test.go`, `patch_test.go`, `grep_test.go`, `approvals_checkpoints_test.go` |
| Depends on | [spec_config_002](spec_config_002.md), [spec_approvals_005](spec_approvals_005.md) (approval gate) |

## 1. Purpose

Every file tool goes through one `Workspace` sandbox: paths must fall under a configured root, writes need a writable root, and blocked patterns are refused everywhere — including through symlinks. Edits show a diff for approval, are serialised per file, are refused if the file changed while waiting for approval, are written atomically, and are checkpointed so each turn can be undone.

## 2. Roots

- **FS-01** Roots: the workspace (read-write, `roots[0]`), `sandbox.allowed_paths` (read-write) and `sandbox.read_only_paths`. Extra roots are resolved relative to the workspace (after `~` expansion) and must exist — a typo must not silently widen or narrow the sandbox.
- **FS-02** Every root is canonical (absolute, symlinks resolved) and opened as an `os.Root`, which rejects `..` traversal and symlinks leaving the root at the OS level.
- **FS-03** Relative paths are relative to the workspace; a relative path that lexically escapes (`..`) is `ErrOutsideWorkspace`. Absolute paths map to the **deepest** containing root (so a read-only root nested inside the workspace wins). A non-canonical spelling of a root (`/var` vs `/private/var`) is retried after resolving its longest existing prefix.
- **FS-04** Write to a read-only root → `ErrReadOnlyPath`. A path matching a blocked pattern → `ErrBlockedPath (pattern)`. If the path is a symlink, its real target is checked too: blocked → refused "via symlink"; for writes, a read-only target → refused.
- **FS-05** Display form: workspace-relative inside the workspace, absolute elsewhere.
- **FS-06** Files over `tools.max_file_size_bytes` (default 10 MiB) are refused for reading; reads guard against growth between stat and read.

## 3. Blocked-path patterns

- **FS-10** Pattern forms (each matches the path itself and everything beneath it): no slash (`.env`, `*.pem`) — that name at any depth; absolute (`~/.ssh`, `/etc/x`) — that location, also its symlink-resolved spelling; relative with a slash (`config/prod/*.yaml`) — relative to every root.
- **FS-11** `*` matches within one segment, `**` across segments (`**/` optional), `?` one character. Patterns containing `"` or newline are invalid.
- **FS-12** Matching is case-insensitive on macOS and Windows (so `.ENV` is `.env`).
- **FS-13** The same expressions (without Go-only syntax) are emitted for the macOS Seatbelt profile ([spec_shell_007](spec_shell_007.md)).
- **FS-14** Walks (`list_files`, `grep`) skip blocked entries and do not descend into blocked directories.

## 4. Writes

- **FS-20** `WriteFileAtomic`: temp file `.<name>.tmp-<hex>` in the same directory, fsync, chmod to the existing file's mode (new files 0644), rename. A symlinked file is rewritten in place through the root instead of replacing the link. Parent directories are created (0755). Writing a directory or the root itself is refused.
- **FS-21** `CreateExclusive` fails with `fs.ErrExist` if the file exists.
- **FS-22** `RemoveFile` removes only non-directories and never a root.
- **FS-23** Every write/delete first snapshots the prior state for checkpoints; if the operation fails the snapshot entry is discarded.

## 5. Concurrency and approval races

- **FS-30** The ADK runs a model response's tool calls concurrently. Read–approve–write sequences are serialised **per file** by a lock keyed on the display path. Multi-file operations lock paths in sorted, de-duplicated order (no deadlock). Waiting honours context cancellation.
- **FS-31** After approval and under the lock, the tool re-reads the file; if it differs from what was shown (or now exists/no longer exists), nothing is written and the tool returns "<path> changed while waiting for approval (edited outside this tool); nothing was written — read it again and redo the edit".

## 6. Tools

All tools return errors in an `error` field of the result (never a Go error), so the model sees them.

| Tool | Arguments | Behaviour |
|---|---|---|
| `read_file` | `path`, `start_line?`, `end_line?` (1-based) | Lines numbered `%4d: `; output capped at 512 KiB with a truncation note suggesting paging; `total_lines`; a start beyond EOF clamps to the last line; `end < start` becomes `start` |
| `list_files` | `directory?` (`.`), `recursive?`, `max_entries?` (default 200, max 500) | Entries `path, is_dir, size_bytes`; dot-files skipped and dot-dirs not descended |
| `glob` | `pattern`, `path?`, `max_results?` (default 200, max 1000) | `**` crosses segments, `*`/`?` stay within one, `{a,b}` alternatives (nested, ≤ 64); relative patterns only (`..`, absolute and `~` refused); dot and dependency directories (`node_modules`, `vendor`, `target`, `__pycache__`) skipped unless a pattern segment names them; regular files only, symlinks not followed; newest first by modification time, then by path; `truncated` when capped; case-insensitive on macOS/Windows |
| `grep` | `query`, `path?`, `is_regex?`, `case_insensitive?`, `max_matches?` (default 100, max 500) | Skips dot-dirs, `node_modules`, `vendor`, `target`, `__pycache__`, files > 5 MiB and binaries (NUL in first 8000 bytes); lines trimmed and ellipsized to 500 bytes; parallel scan (≤ min(GOMAXPROCS, 8)) but results in walk order so the first N are deterministic; CRLF tolerated |
| `create_file` | `path`, `content`, `overwrite?` | Existing file without `overwrite` fails; approval shows a diff ("Create"/"Overwrite <path> (N bytes)") |
| `replace_in_file` (alias `edit`) | `path`, `target_content`, `replacement_content`, `allow_multiple?` | Empty target, no match, or multiple matches without `allow_multiple` fail with guidance; approval with diff |
| `delete_snippet` | `path`, `snippet` | Removes the first exact occurrence; approval with diff |
| `delete_file` | `path` | Approval with a removal diff; kind `delete` |
| `apply_patch` | `patch` | See §7 |

- **FS-40** Edit approvals: kind `write_file`, key `write:<workspace>` ("file edits in <workspace>"), targets = workspace-relative paths. Deletions: kind `delete_file`, key `delete:<workspace>`.
- **FS-41** The path is resolved as writable **before** asking, so the user is never asked to approve a write the sandbox would refuse.

## 7. `apply_patch`

- **FS-50** Accepts a unified diff (git or plain, `---/+++/@@`, `a/`/`b/` prefixes and timestamps stripped, git metadata and prose between files ignored, `\ No newline at end of file` respected, blank context lines missing their leading space tolerated) or a `*** Begin Patch … *** End Patch` block (add/delete/update/move).
- **FS-51** Validation happens before anything is written: every file resolved writable, a file may appear only once, add requires absence, delete/update require presence, move requires the destination to be absent.
- **FS-52** Hunk matching: exact, then ignoring trailing whitespace, then ignoring surrounding whitespace; nearest occurrence to the hinted line; anchors (`@@ context`) supported; pure insertions at hint/after anchor/EOF. Matching is on LF-normalised text and CRLF is restored. Failures name the hunk and preview the lines not found.
- **FS-53** One approval for the whole patch ("Apply patch to N file(s): …") with a combined diff (renames shown `rename a -> b`), targets = every touched path.
- **FS-54** Atomic: if any write fails, already-changed files are restored; the error says "(no files were changed)" or reports a rollback failure. The result lists per-file summaries.

## 8. Checkpoints and undo

- **FS-60** A checkpoint (turn) begins with each prompt ([spec_workspace_018](spec_workspace_018.md) WS-22); an empty current turn is reused rather than stacking empty ones. Changes outside any turn go into "(no prompt)".
- **FS-61** For each file, the state before the turn's **first** change is kept (content, existence, mode; symlinks record the target's mode), plus a hash of what the tool left.
- **FS-62** Memory budget `checkpoints.max_bytes` (default 64 MiB): oldest turns are dropped, always keeping the latest. Files larger than the max file size are marked too-large (not restorable).
- **FS-63** `Undo(force)` reverts the latest turn with changes, in reverse order. Without force it refuses when a file no longer matches what the tool wrote (`ErrUndoConflict`, listing files, "use /undo --force") or when a file was too large to snapshot. Restores are atomic writes with the original mode; files that did not exist are removed. The turn is removed; restored paths are sorted.
- **FS-64** Only changes made through file tools are tracked; shell side effects appear as conflicts.
- **FS-65** `SessionDiff` renders a unified diff from each file's earliest snapshot this session to its current content (`# <path>: too large to diff` otherwise); sorted by path.
- **FS-67** Checkpoints persist (`[checkpoints] dir`, default `~/.blitz/checkpoints`, one directory per workspace named after a hash of its path; `dir = ""` keeps them in memory). The directory holds `index.json` (turns with their session, the prompt's transcript index, label, time and per-file before-state by hash, after-existence and after-hash) and `blobs/<sha256>`, all owner-only, written atomically. Snapshots live only in blobs (memory holds hashes). On open, turns older than `max_age_days` (30) and past the size budget are dropped, a snapshot whose blob is missing (or doesn't match its hash) is marked unrestorable, and an unreadable index is renamed `index.json.damaged` (the store starts empty, with a warning). Unreferenced blobs are removed after turns are dropped and on open. The workspace lock (one process per workspace) is what makes one index safe. A save failure warns once; checkpoints then last only for the process.
- **FS-68** `Rewind(session, prompt, force)` restores, as one operation, every file changed by `session`'s turns from transcript index `prompt` on: each file goes back to its state before the earliest of those turns, after checking every file first against the latest turn's after-state (conflicts as FS-63, "use --force"). Other sessions' turns are left alone. `Detach(session, prompt)` moves such turns to `prompt-1` after the conversation alone is rewound, so a rewind to the next new prompt leaves them, and one to an earlier prompt still includes them. `SessionDiff(session)` diffs from each file's earliest snapshot among that session's turns (`""`: all).
- **FS-66** Diffs use `a/<path>`/`b/<path>` and `/dev/null` for creation/deletion.
