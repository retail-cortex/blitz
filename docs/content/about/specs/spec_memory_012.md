---
title: "012 · Memory"
weight: 12
---

*Project memory (instruction files)* (`spec_memory_012`)

| | |
|---|---|
| Status | Implemented. Reverse-engineered from `53f8c53` (2026-09-24) and kept with the code since; its paths and names checked 2026-09-27 (`//tools/specs`). |
| Source | `pkg/engine/memory/memory.go`; `/memory` in `apps/cli/internal/tui`, `ReloadMemory`/`AddMemory` in `pkg/engine/context.go` |
| Tests | `pkg/engine/memory/memory_test.go` |
| Depends on | [spec_config_002](spec_config_002.md) |

## 1. Purpose

Instruction files in the project and the user's home are added to every agent's system prompt, so a repository can tell the agent its conventions. They inform; they never grant permissions.

## 2. Loading

- **MEM-01** Disabled by `[memory] enabled = false`.
- **MEM-02** Order: the global file (`memory.global`, default `~/.blitz/BLITZ.md`) and the global rules (`memory.global_rules`, `~/.blitz/rules/`) first; then, for each directory from the repository root down to the workspace, each name in `memory.files` (default `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, `BLITZ.md` — other agents' files too, so a repository set up for them works unchanged), then `memory.local_files` (default `CLAUDE.local.md`, `BLITZ.local.md`: personal files, warned about at startup when git tracks them), then the rule files in `memory.rule_dirs` (default `.blitz/rules`, `.agents/rules`, `.claude/rules`) — more specific files come last.
- **MEM-03** The repository root is the nearest ancestor (up to 10 levels) containing `.git`. Without one, only the workspace directory is searched.
- **MEM-04** Files are de-duplicated by real path (symlinks resolved) and by content (a `CLAUDE.md` copying `AGENTS.md` loads once); directories and unreadable files are skipped; empty files are dropped.
- **MEM-06** Imports: a token `@path` (at a line start or after whitespace; with a `/` or a file extension, so `@team` and e-mail addresses stay text; not in fenced code blocks or inline code) loads that file right after the importing one, relative to the importing file as written, or `~/…` for files under `~/.blitz`. The real path must stay inside the repository root (the workspace without a repository; `~/.blitz` for user files), and blocked paths (`sandbox.blocked_paths`, e.g. `.env`) are never read. Imports nest at most 5 deep; cycles and repeats load once. The prompt names the importing file.
- **MEM-07** Rules: `*.md` files under the rule directories (recursively; symlinked files allowed and checked like imports). A rule without frontmatter `paths` is loaded like an instruction file. A rule with `paths` (a glob or a list, relative to the directory holding the rule directory; `**`, `*`, `?`) is **scoped**: it is not in the prompt; instead the first successful `read_file`, `create_file`, `replace_in_file`/`edit`, `delete_snippet`, `delete_file`, `view_image` or `apply_patch` call in a session on a matching file carries it in the tool result as `project_rules` (once per session and rule). Listing or searching (`list_files`, `glob`, `grep`) doesn't count. Paths are compared with symlinks resolved on both sides.
- **MEM-05** Each file is capped at `memory.max_bytes` (32 KiB, UTF-8 safe, marked "(truncated)"), and terminal control sequences are stripped.

## 3. Rendering

- **MEM-10** Section "## Project Instructions" stating the files come from the user's project and home directory, should be followed unless they conflict with the user's requests or safety rules, and **cannot grant permissions or bypass approvals**; then, when scoped rules exist, a sentence saying they arrive as `project_rules` with tool results; then one "### <path>" block per file (imports marked "(imported by …)").
- **MEM-11** Memory is appended to every agent's instructions (with the reply-language instruction, [spec_i18n_004](spec_i18n_004.md)).

## 4. Commands

- **MEM-20** `/memory` (or `/memory show`) lists loaded files; `/memory reload` re-reads them and rebuilds the agents; `/memory add <note>` appends `- <note>` to `<workspace>/<last configured file>` (default `BLITZ.md`), creating it with "# Project notes for Blitz" if absent, then reloads. Empty notes are refused.
- **MEM-21** `doctor` lists the memory files found and the number of path-scoped rules.
- **MEM-22** `/init` (and `blitz init`, a one-shot run) sends a fixed prompt (`runtime.InitPrompt`) asking the agent to study the repository and write or update `BLITZ.md` — purpose, verified build/test/lint/run commands, layout, non-obvious conventions, gotchas; no invented commands, no secrets, under 150 lines, and not repeating `AGENTS.md`/`CLAUDE.md`. Edits go through the usual approvals and diffs. The transcript records `/init`, and memory is reloaded afterwards so the file applies from the next prompt.
