# spec_memory_012 — Project memory (instruction files)

| | |
|---|---|
| Status | Implemented (reverse-engineered from `53f8c53`) |
| Source | `internal/memory/memory.go`; `/memory` in `internal/tui`, `ReloadMemory`/`AddMemory` in `internal/app/context.go` |
| Tests | `internal/memory/memory_test.go` |
| Depends on | [spec_config_002](spec_config_002.md) |

## 1. Purpose

Instruction files in the project and the user's home are added to every agent's system prompt, so a repository can tell the agent its conventions. They inform; they never grant permissions.

## 2. Loading

- **MEM-01** Disabled by `[memory] enabled = false`.
- **MEM-02** Order: the global file (`memory.global`, default `~/.blitz/BLITZ.md`) first; then, for each directory from the repository root down to the workspace, each name in `memory.files` (default `AGENTS.md`, `BLITZ.md`) — more specific files come last.
- **MEM-03** The repository root is the nearest ancestor (up to 10 levels) containing `.git`. Without one, only the workspace directory is searched.
- **MEM-04** Files are de-duplicated by real path (symlinks resolved); directories and unreadable files are skipped; empty files are dropped.
- **MEM-05** Each file is capped at `memory.max_bytes` (32 KiB, UTF-8 safe, marked "(truncated)"), and terminal control sequences are stripped.

## 3. Rendering

- **MEM-10** Section "## Project Instructions" stating the files come from the user's project and home directory, should be followed unless they conflict with the user's requests or safety rules, and **cannot grant permissions or bypass approvals**; then one "### <path>" block per file.
- **MEM-11** Memory is appended to every agent's instructions (with the reply-language instruction, [spec_i18n_004](spec_i18n_004.md)).

## 4. Commands

- **MEM-20** `/memory` (or `/memory show`) lists loaded files; `/memory reload` re-reads them and rebuilds the agents; `/memory add <note>` appends `- <note>` to `<workspace>/<last configured file>` (default `BLITZ.md`), creating it with "# Project notes for Blitz" if absent, then reloads. Empty notes are refused.
- **MEM-21** `doctor` lists the memory files found.
