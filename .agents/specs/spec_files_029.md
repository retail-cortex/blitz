# spec_files_029 — Files: explorer, editor, diff and language intelligence

| | |
|---|---|
| Status | Phase 1 implemented (2026-09-27, on `main`); phases 2 and 3 planned |
| Source | `pkg/engine/files.go`, `pkg/engine/tools/userfs.go`, `proto/blitz/v1/file.proto`, `apps/service/internal/server/files.go`, `apps/desktop/unsaved.go`; the page's `src/files/` |
| Depends on | [spec_workspace_018](spec_workspace_018.md), [spec_service_021](spec_service_021.md), [spec_desktop_024](spec_desktop_024.md), [spec_filetools_006](spec_filetools_006.md) |

## 1. Purpose

The desktop app shows only the conversation: you can't see where things are, so you can't point the agent at them, and you can't make a small change yourself. This adds the workspace's files to the window: a tree to browse, an editor to read and change them, diffs, and language intelligence.

The owner's decisions (2026-09-27):

- **The editor sits beside the conversation**, not in a view of its own, so you can talk while you look ("test and learn": revisit once used). Revisited the same day: the window is laid out like an IDE — files on the left (minimizable), the editor in the middle, the chat on the right, the workspace a dropdown in the top bar ([spec_desktop_024](spec_desktop_024.md) DSK-65).
- **Blitz installs a missing language server** when you agree (phase 3).
- **The agent is told about files you change** between its turns.
- **Hidden files** (dotfiles, ignored files, the agent's blocked paths) **are hidden by default**, with a toggle to show them.

## 2. Phases

| Phase | What |
|---|---|
| 1 | `FileService`; the Files shelf (tree, git status, hidden toggle, file operations); the editor (tabs, highlighting, word and keyword completion, saving with conflict detection); quick open; paths in the conversation open the editor; the agent told of your edits |
| 2 | Diffs in the editor (unsaved against disk, against `HEAD`, the agent's changes this session) and in the Changes view, reverting one hunk of the agent's changes; find in files; **Add to chat** (a file or lines as a reference in the composer); badges for files the agent read or changed; image and Markdown previews; reveal in Finder |
| 3 | `LanguageService` over language servers: completion, hover, go to definition, references, problems (with **Ask Blitz to fix**); installing a missing server; the agent's use of the same |

## 3. Service: `FileService` (phase 1)

Every request names its workspace (SVC-10) and a path relative to it.

- **FIL-10** Scope: the workspace directory only, through `os.Root` (`tools.Workspace`), so no path, `..` or symlink leaves it. Other roots the agent may use (`sandbox.allowed_paths`) aren't browsed.
- **FIL-11** The agent's path rules bind the agent, not you: a blocked path (`sandbox.blocked_paths`) or a read-only root is listed with the rule that applies (`blocked`, `read_only`) so the window can say so, and you may open and change it. `.git` is never listed.
- **FIL-12** `ListDir(path, show_hidden)`: the directory's entries, folders first then by name, each with its kind (file, folder, symlink), size, modification time, git status (FIL-13), why it's hidden (`dot`, `ignored`, `blocked`, or none) and the agent's rule. Hidden entries are left out unless `show_hidden`. At most 5,000 entries (`truncated` beyond).
- **FIL-13** Git status comes from `git status --porcelain=v1 -z --untracked-files=all --ignored=no` and ignored files from `git check-ignore -z --stdin`, both with the repository's filter drivers blanked and `core.fsmonitor` off, as `GitDiff` does (running git never runs the repository's configured programs). Statuses: modified, added, deleted, renamed, untracked, conflicted; a folder reports that something under it changed. Outside a repository there's no status and only dotfiles are hidden.
- **FIL-14** `ReadFile(path)`: the text, a `version` (SHA-256 of the content), size, and whether it's binary (a NUL in the first 8 kB, or not UTF-8) or too large (over 2 MB, or `tools.max_file_size_bytes` if smaller); binary and too-large files come without content.
- **FIL-15** `WriteFile(path, text, version)` replaces the file atomically (temporary file and rename, its mode kept) only if its content still has `version` (`""`: it must not exist). Otherwise `FailedPrecondition` with reason `FILE_CHANGED` and the current version. It returns the new version.
- **FIL-16** `CreateFolder`, `Rename(from, to)` (never over an existing path), `Delete(path)` (a folder with everything in it). Your writes aren't checkpoints of the agent's turns, so **Undo last turn** doesn't undo them; undoing an agent's turn over a file you've since changed asks first (CHK conflict, as before).
- **FIL-17** `FindFiles(query, limit)`: files whose path matches `query` as a fuzzy subsequence, best first (consecutive runs, matches at the start of a name or after `/`, `_`, `-`, `.`, and shorter paths rank higher), leaving out hidden files. The list comes from `git ls-files --cached --others --exclude-standard` in a repository, else a walk (at most 50,000 files), and is kept for 5 seconds.
- **FIL-18** `StatFiles(paths)`: each file's version (or that it's gone), so the window can see changes made by the agent, a shell command or a worker.

## 4. The agent and your edits (phase 1)

- **FIL-20** Your writes, renames and deletions are recorded per workspace. The next turn's prompt (not an aside) carries them, and they're forgotten:
  `<user-edits>The user changed these files in the editor since your last turn: a.go (edited), b.go (deleted). Read them again before relying on what you saw.</user-edits>`
- **FIL-21** A change you make while the agent waits for approval of an edit to the same file is caught as before (the edit is refused and the agent reads the file again).

## 5. The window (phase 1)

- **FIL-30** The **Files** shelf: a panel on the left of the window, minimized to a rail (**Show files**, **Go to file**) with its **Minimize** button (remembered per window). It lists the workspace as a tree loaded folder by folder: icons by kind, git status as a coloured letter (M, A, D, R, U, !) with the name tinted, folders with changes marked, hidden entries dimmed when shown, a lock on blocked or read-only ones with the rule in its tooltip. The header has **New file**, **New folder**, **Refresh**, **Collapse all**, **Show hidden files** (off by default, remembered per window) and **Minimize**. Under 1100 px the shelf floats over the editor and minimizes when a file opens.
- **FIL-31** Each entry's menu: **Open**, **New file**, **New folder** (in a folder), **Rename** (in place), **Delete** (confirmed, saying a folder goes with everything in it and that only git can bring files back), **Copy path**, **Copy relative path**. Keyboard: arrows move and open folders, Enter opens, F2 renames, Delete deletes.
- **FIL-32** Opening a file shows it in the editor in the middle, between the shelf and the chat (switching the middle from Changes or Workers). With no file open the chat takes the middle instead, and moves back to the side when one opens. The chat's width is dragged at its left edge (remembered); the editor takes the rest.
- **FIL-33** The window notices changes made elsewhere: after each tool call that writes, at the end of a turn, and every 5 seconds while the shelf or editor is shown, it lists the open folders again and checks the open files (`StatFiles`). A file you haven't changed reloads; one you have gets a bar: **Reload** (discard yours) or **Keep mine** (the next save asks to overwrite).
- **FIL-34** **Go to file** (⌘P, and in the command palette): `FindFiles` as you type, Enter opens.
- **FIL-35** Paths in the conversation open the editor: file paths in tool calls, and inline code in answers that names a file in the workspace (for example `internal/cart/discount.go`, with `:42` or `:42:7` going to that line). The inline code gets a link style only once the file is known to exist (FindFiles' list).

## 6. The editor (phase 1)

- **FIL-40** CodeMirror 6: tabs (a dot for unsaved changes, middle click or ⌘W closes, asking about unsaved changes), the path as breadcrumbs, line numbers, the active line, bracket matching, folding, search and replace (⌘F), go to line (⌃G), multiple cursors, indentation from the file (tabs or spaces), soft wrap off (a toggle).
- **FIL-41** Highlighting by file name (`@codemirror/language-data`: Go, TypeScript and JavaScript, Python, Rust, Java, C and C++, JSON, YAML, TOML, Markdown, HTML, CSS, SQL, shell, protobuf, Dockerfile and more), each language's parser loaded when first needed. Colours from the window's theme, light and dark, matching the conversation's code blocks.
- **FIL-42** Completion without a language server: the language's keywords and snippets where CodeMirror has them, and words from the open files. Phase 3 adds a language server's.
- **FIL-43** ⌘S saves (`WriteFile` with the version it loaded). `FILE_CHANGED` asks: **Overwrite**, **Reload** (discard yours), or cancel; phase 2 adds **Compare**.
- **FIL-44** Binary and too-large files open as a note saying so, with the size. Blocked files open with a bar: "Blitz's agent can't read this file"; read-only roots likewise.
- **FIL-45** Unsaved changes survive switching workspaces and views; closing a workspace (any way: its menu, the workspace dropdown, its details) or the window with unsaved changes asks first. The page tells the app how many files are unsaved (`SetUnsaved`, in the window's language), and the app's close handler asks with a native dialog.

## 7. Phase 2 (outline)

- **FIL-50** Diff (`@codemirror/merge`): unsaved against disk; the file against `HEAD`; the agent's changes this session (its checkpoints). Side by side or inline. The Changes view uses the same view.
- **FIL-51** Revert one hunk of the agent's changes (a write of the file without that hunk, recorded as your edit, FIL-20).
- **FIL-52** Find in files (⌘⇧F): the agent's grep over the workspace, results by file with the line, opening at the match.
- **FIL-53** **Add to chat**: a file (from the tree, or dragged onto the composer) or the selected lines, as a reference the prompt expands (`@path:10-24`); **Ask about this file**, **Explain the selection**. Blocked files can't be added.
- **FIL-54** Tree badges for files the agent read or changed in the session; image previews; Markdown preview beside its source; **Reveal in Finder** (a desktop binding).

## 8. Phase 3 (outline)

- **FIL-60** `LanguageService`: the service runs a language server per workspace and language (gopls, typescript-language-server, pyright, rust-analyzer, and others by configuration) and answers `Complete`, `Hover`, `Definition`, `References` and a `Diagnostics` stream. The page's transport can't hold a two-way stream, so these are separate calls.
- **FIL-61** Language servers run the project's code (build files, plugins), so they start only in trusted workspaces (`trust_workspace`), in the OS sandbox with the workspace writable.
- **FIL-62** A missing server: the window offers to install it. Blitz installs into `~/.blitz/tools` (never system-wide) with the language's own tool (`go install gopls@<pinned>`, npm for the TypeScript and Python servers, `rustup component add rust-analyzer`) or a release binary checked against its published checksum, in the OS sandbox with the network allowed, and says what it runs first. If the language's own tool is missing, it says how to get it.
- **FIL-63** Problems inline and in a list, with **Ask Blitz to fix**. The agent gets tools over the same servers (definitions, references, diagnostics).

## 9. Tests

- **FIL-90** The engine: confinement (paths out, `..`, symlinks out, `.git`), hidden reasons (dot, git-ignored, blocked), git status of each kind, versions and `FILE_CHANGED`, create/rename/delete, fuzzy ranking, and the user-edit note reaching the next turn once.
- **FIL-91** The service: each call over Connect, and `FILE_CHANGED` with the current version in the error's details.
- **FIL-92** The page: the tree's state (expand, refresh keeping what's open, hidden toggle), path detection in answers, language choice by file name, the unsaved-changes rules; screenshots with the fake service.
