---
title: "035 · Workspace search"
weight: 35
---

*Search everything in a workspace* (`spec_search_035`)

| | |
|---|---|
| Status | **Implemented** (2026-10-03): keyword search (the index, its sources, the API, the CLI, the agent's tool, the desktop dialog), semantic search (opt-in embeddings) and LLM metadata (opt-in summaries and tags). |
| Source | `pkg/engine/search` (`index.go`, `scan.go`, `query.go`, `vectors.go`, `enrich.go`), `pkg/engine/search.go` (sources, lifecycle, the describer), `pkg/engine/runtime/embed.go`, `pkg/api/search.go`, `pkg/engine/tools/search.go`, `WorkspaceService.SearchWorkspace`/`GetSearchStatus`/`Reindex` in `proto/blitz/v1/workspace.proto`, `apps/cli/search.go`, `apps/cli/internal/tui/searchws.go`, `apps/desktop/web/src/{SearchDialog.tsx,search.ts}` |
| Tests | `pkg/engine/search/{search,vectors,enrich}_test.go`, `pkg/engine/runtime/embed_test.go`, `pkg/engine/search_test.go`, `apps/service/internal/server/handlers_test.go` (`TestSearch*`), `pkg/client/remote_test.go`, `apps/cli/search_test.go`, `apps/cli/internal/tui/searchws_test.go`, `apps/desktop/web/src/search.test.ts` |

## 1. Purpose

One search over what a workspace holds: its files, the text of its PDFs and notebooks, its chats, and its notes and plans; ranked, with the passage that matched and where it is. For people (the desktop app, the CLI) and for agents (`search_workspace`), next to `grep`, which finds every exact match of a pattern but doesn't rank.

## 2. Words

A **workspace** is a folder opened in Blitz and what Blitz keeps for it; the index is part of that, kept in `~/.blitz`, never in the folder. See [the guide](../../guide/_index.md#words).

## 3. The index

- **SRCH-01** One index per workspace, in `config.WorkspaceSettingsDir(cfg.Dir, workspace)/search/index.db` (`~/.blitz/workspaces/<name>-<hash>/search`, mode 0700): SQLite through `modernc.org/sqlite` (pure Go, so the `pure = "on"` builds keep it). An index of another schema version is rebuilt.
- **SRCH-02** Items (`docs`: source, reference, title, size, time, a hash of the text) are split into chunks of about 60 lines or 2 KB, never across a section's start (a PDF's page, a notebook's cell); a line over 4 KB is cut. Each chunk is in two FTS5 tables: `words` (`porter unicode61`: title and text) and `grams` (`trigram`: text).
- **SRCH-03** `search.enabled = false` keeps no index: search, the status and `Reindex` say so (`ErrSearchDisabled`, `SEARCH_DISABLED`). A workspace whose index can't be opened opens with search off and a warning.

## 4. Sources

- **SRCH-10** `files`: the workspace's files as git lists them (`ls-files --cached --others --exclude-standard`), or every file outside a repository, dotfiles and dot folders included (`.agents`, `.github`, `.gitignore`; *2026-10-03*, they were left out as Go to file leaves them) except `.git` and the top `.blitz` (plans are §4's notes; worktrees are whole checkouts); blocked paths (`AgentRule` "blocked": deny read rules, `.env*`, keys), binaries (`isBinary`), files over `tools.max_file_size_bytes`, PDFs and notebooks left out. Files are read through an `os.Root` of the workspace, so a link can't lead out of it. The list isn't the 5-second one `FindFiles` keeps (`listFiles`), so a scan doesn't make Go to file miss a new file.
- **SRCH-11** `documents`: PDFs (text through `pdftext.Text`, which in `blitz` and `blitzd` runs in a helper process killed at the timeout; IMG-52), up to 2,000,000 characters, sections "page N"; notebooks (`tools.NotebookText`, as `read_file` shows them), sections "cell N".
- **SRCH-12** `chats`: the workspace's sessions (`ListWorkspace`), what was asked and answered (tool output left out), titled by the chat's title or its ID.
- **SRCH-13** `notes`: the remember tool's notes (`~/.blitz/memory/…`, by name) and approved plans (`.blitz/plans/*.md`, by path).
- **SRCH-15** *2026-10-03.* Hidden items: an item a source marks hidden (the files source: dotfiles and what's in dot folders; files git ignores) is found only when the search asks for hidden items (`SearchQuery.Hidden`, the API's `hidden`). The desktop app asks when its Files shelf shows hidden files; `blitz search --hidden`; the agent's `search_workspace` always does. A change of hidden only (a `.gitignore` edited) is taken at the next scan without reading the file.
- **SRCH-16** `search.include_ignored` (off by default; a switch in the Search panel): the files git ignores are indexed too (`git ls-files --others --ignored`), hidden, without dependency, build and cache folders (`node_modules`, `vendor`, `target`, `build`, `dist`, `out`, `__pycache__`, virtual environments, `.cache` and the like, `bazel-*`), `.git`, the top `.blitz` or blocked paths.
- **SRCH-17** Tables (`.csv`, `.tsv`, `.tab`): their first 1 MB is indexed, to the last whole row, whatever their size, Latin-1 decoded, so their columns and leading values are found.
- **SRCH-14** `search.sources` (default `files`, `documents`) is what a search uses when it names none. An unknown source is `ErrUnknownSearchSource` (`UNKNOWN_SEARCH_SOURCE`).

## 5. Keeping up

- **SRCH-20** No file watcher: scans. One when the workspace opens; one after each turn ends; one on `Reindex`; and one a minute (`searchInterval`) after the last ended while no turn runs. One runs at a time; closing the workspace stops it.
- **SRCH-21** A scan reads only what changed: an item with the same size and time isn't read; one read again whose text hashes the same only has its size and time updated. Gone items, and items now blocked or unsearchable, are dropped.
- **SRCH-22** An item that can't be read is remembered as unreadable (no text) and not read again until it changes, so a PDF that takes the whole extraction timeout costs it once. The status counts these apart.
- **SRCH-23** Blocked paths are checked again at query time, so a rule added since the last scan hides its files at once.
- **SRCH-24** A write the database refuses fails the scan with its reason (the status's error) and leaves the index as it was.
- **SRCH-25** `[search]` settings apply while the workspace is open (`Workspace.ReloadSearch`): when the settings files change (the settings watcher, within 2 s) or the service saves one (`ConfigService` changes, at once), the index is closed and opened again with them; the same settings leave it alone.

## 6. Searching

- **SRCH-30** A query is words and "quoted phrases" (`EMPTY_SEARCH` with none); every term must match. The last word also matches as a prefix (typing). The `words` table ranks by BM25 (the title weighs 4×); for a query that looks like code (an identifier, dots, slashes, brackets, camelCase) the `grams` table adds substring matches. The two lists are fused by reciprocal rank (k = 60).
- **SRCH-31** One hit per item, its best chunk: source, reference, title, the line of the passage (1-based), its section, the passage (the matching line and one either side, each cut to 240 characters), the score. 20 hits unless asked for more.

## 7. Surfaces

- **SRCH-40** API: `WorkspaceService.SearchWorkspace` (query, sources, limit), `GetSearchStatus` (enabled, items per source, scanning, last scan, unreadable, error), `Reindex` (starts a scan and returns); `api.Backend` `Search`, `SearchStatus`, `Reindex`, local and remote alike.
- **SRCH-41** The agent's `search_workspace` (query, sources, limit): hits as `where` (`path:line`, a chat's ID), title, section, snippet; failures in its `error`. Read-only, so allowed in plan mode; built-in agents with `grep` have it.
- **SRCH-42** CLI: `/search <terms>` (the default sources), `/search files|documents|chats|notes|all <terms>`, `/search status`, `/search reindex`; none leads to a turn. `blitz search <terms> [--in …] [-n N] [--json] [--status] [--reindex]` waits up to two minutes for a scan that ended after it started (a workspace it opened itself; the service's index is kept current), and prints without colors when its output isn't a terminal. The last scan's time is kept in the index.
- **SRCH-45** The top bar's search (the command palette, Cmd/Ctrl+K) shows, under the files whose names match, up to six passages workspace search finds for what's typed ("In the workspace": where, and the matching line), then **Search the workspace for "…"**, which opens the dialog with the query in it.
- **SRCH-44** Desktop: **Settings › Workspaces › Search** (`SearchSettings.tsx`), saved in the workspace's own settings (`ConfigService.SetConfigValue`, which writes `search.enabled`, `search.enrich` and `search.enrich_daily_limit` with their types and `search.sources` as a list, refusing other values) and applied at once: **Search this workspace**; **Search by default in** (chips); **Search by meaning** (an embedding model, suggested ones listed, with what's sent where; empty is off); **Describe files** (on, its model and files a day); the index's counts, its problem if a model couldn't be built, and **Scan the workspace again**. Folded, the section says On or Off and which extras are on. `GetSearchStatus` carries the settings followed (`SearchSettings`, even off) and `problem`.
- **SRCH-43** Desktop: **Search the workspace** (Cmd/Ctrl+Shift+F, or the palette): the query, chips for the sources (Files and PDFs on; the last can't be turned off; clicking one keeps the focus in the query), hits with their passage, the words marked; Enter or a click opens a file at its line (a PDF at its start), a chat, or a plan; a note kept outside the workspace opens nothing. A Markdown file (a passage from the palette, too) opens in its preview, even if its tab showed the source: scrolled to the block its line is in, the query's words marked (a cut word, as the stemmer cuts it, to the end of the word), the hit's own the strongest, by the CSS Custom Highlight API so the document stays as rendered; the marks go when the preview closes or its text changes, and the tab stays in the preview. The foot shows the index's counts, "Indexing…" while it scans, and **Scan again**.

## 8. Semantic search

- **SRCH-50** *Opt-in* (`search.embedding_model`, "provider/model": Gemini through the Gemini API or Vertex AI, OpenAI, or an OpenAI-compatible server such as Ollama; `runtime.NewEmbedder`). Every chunk (its item's title and its text, cut to 8,000 characters) is sent to that provider once, and each semantic or hybrid query; empty, search stays local. A project's settings can't set it (`neverFromProject`). A model that can't be built leaves keyword search, with a warning.
- **SRCH-51** Vectors live beside the index in `search/vectors`: a chromem-go database (MPL-2.0, pure Go), one collection per source, one compressed file per chunk. After each scan, chunks without a vector are embedded 64 at a time; the vectors of dropped or changed chunks are removed at the next pass (`stale`). Changing the model throws the vectors away and embeds everything again. An embedding failure stops the pass (the status's `embed_error`) without failing the scan; keyword search goes on, and the next scan carries on where it stopped.
- **SRCH-52** Modes: `keyword` (§6), `semantic` (the query embedded, the nearest chunks by cosine similarity across the sources searched) and `hybrid` (both, fused by reciprocal rank), the default with a model. `semantic` or `hybrid` without one is `ErrNoEmbeddings` (`NO_EMBEDDINGS`); another mode `ErrUnknownSearchMode` (`UNKNOWN_SEARCH_MODE`). `blitz search --mode`, the tool's `mode`, and in the desktop a **Words and meaning / Words / Meaning** switch shown only with a model.

## 9. LLM metadata

- **SRCH-60** *Opt-in* (`search.enrich`): after each scan (and embedding), a model (`search.enrich_model`, else `suggestions.model`, else the auto mode's, else the default model) writes a summary (one or two sentences, up to 300 characters) and tags (3 to 10 lowercase keywords, synonyms welcome) for each file and document whose text changed since, newest first. The text sent (up to 24,000 characters) has its secrets redacted (`SecretRedactor`), and the model is told never to repeat any.
- **SRCH-61** Within `search.enrich_daily_limit` items a day per workspace (default 200; the day's count is kept in the index, so a restart doesn't reset it), pausing while a turn runs. Items over 64 KB, unreadable, empty, vendored or generated (`vendor/`, `node_modules/`, `third_party/`, `dist/`, `build/`, `gen/`, `generated/`, `.pb.go`, `.min.js`, lock and sum files, a first line with "Code generated … DO NOT EDIT") are marked done without a call. A failure stops the pass (the status's `enrich_error`); the next scan tries again. A project's settings can't turn it on.
- **SRCH-63** A table is described from a profile of its columns, not its rows (each column's name, kind, numeric range, empty count and sample values, from its first 2,000 rows; `table.Describe`), whatever its size; the model also says what each column holds and its unit. Those are kept (columns the table doesn't have are dropped), searched (a search for "salinity" finds the table whose column is `Salnty`), and shown by the table viewer (FIL-71).
- **SRCH-62** Summaries and tags are searched too (the `about` FTS5 table: an item found by them comes in as its first chunk, a third list in the fusion), and every hit carries its item's summary and tags: under the hit in the desktop app and the CLI, in the tool's result and `--json`. The status counts the items described.
