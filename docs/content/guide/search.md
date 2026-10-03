---
title: Search
weight: 70
---

Specs: [workspace search](../about/specs/spec_search_035.md), [web](../about/specs/spec_web_008.md).

## Workspace search

Blitz keeps an index of each workspace so you can search everything in it at once: its files (as git sees them; blocked paths such as `.env` are never indexed), the text of its PDFs and notebooks, its chats, and its notes and approved plans. Results are ranked, one per item, with the passage that matched and where it is.

- **Desktop app:** type in the search box at the top (⌘K): passages from the workspace show under the matching file names. **Search the workspace** (⇧⌘F, or the last entry in that list) is the full search. Pick where to look with the chips (Files and PDFs to start); Enter opens the file at the line, the chat or the plan; a Markdown file opens in its preview, at the passage, with your words marked.
- **Terminal:** `/search <terms>` in a session, or `/search files|documents|chats|notes|all <terms>`; `blitz search <terms>` from the shell, with `--in`, `--json`, `--status` and `--reindex`.
- **Agents:** the `search_workspace` tool, next to `grep`: ranked passages rather than every match.

Dotfiles (`.agents/`, `.github/`) and, with **Include files git ignores** on, ignored files such as data are indexed but hidden: they show in results when the Files shelf shows hidden files (`blitz search --hidden`); agents always see them. A CSV's first megabyte is indexed whatever its size, and with **Describe files** on, a model says what each of its columns holds, so "salinity" finds the table whose column is `Salnty`.

Type words or `"a phrase"`: every term must match, word forms count (`indexing` finds `indexed`), and parts of identifiers match too (`turnsMu`). The index lives in `~/.blitz/workspaces/`, never in your folder, and catches up by itself: when the workspace opens, after each turn, and a minute after the last scan while you're idle. `/search reindex` (or **Scan again**) brings it up to date now. In the desktop app, **Settings › Workspaces › Search** turns it off or on, picks where a search looks by default, and turns on the two extras below, for that workspace, at once; in the settings file, that's `search.enabled`, `search.sources`, `search.embedding_model` and `search.enrich`.

Search is local: nothing leaves your machine, unless you turn on either of these, which send your files to a model provider:

- **Semantic search** (`search.embedding_model`, e.g. `gemini/text-embedding-004`, `openai/text-embedding-3-small` or `ollama/nomic-embed-text` to stay local): finds by meaning as well as words ("automobile" finds "car"). Each indexed passage is embedded once, and each query; `--mode keyword|semantic|hybrid` (or the switch in the desktop app) picks how a search ranks.
- **Summaries and tags** (`search.enrich = true`): a model describes each file in the background, up to `search.enrich_daily_limit` a day (default 200), skipping large, generated and vendored files and redacting secrets first. Results show the summary and tags, and a search finds a file by what it's about.

A project's settings can't turn either on.

## Web search providers

Set `web.search_provider` to one of:

- **`google`**: Gemini's grounding with Google Search, using your Gemini key (`web.search_api_key`, `[llm.gemini] api_key` or `GEMINI_API_KEY`), whatever `llm.provider` is. `web.search_model` picks the Gemini model. Google bills each search query the model runs: 5,000 a month are free across Gemini 3 models, then $14 per 1,000. `/cost` doesn't include this.
- **`searxng`** (`web.search_url`): open source and self-hosted. Enable the JSON format in its `settings.yml` (`search: formats: [html, json]`); most public instances turn it off.
- **`brave`** or **`tavily`**: a key in `web.search_api_key`, or `BRAVE_API_KEY` or `TAVILY_API_KEY`.

The agent's `web_search` asks for approval (rememberable per provider), because the query leaves your machine. Results from `web.deny_domains` are dropped. `blitz doctor --online` runs a test search.

## /search

```text
/search web how do I wrap errors in Go 1.26        # find pages; the agent reads them and answers
/search session "circuit breaker"                  # what did we say about it earlier?
```

**`/search web <terms>`** searches with the configured provider and hands the agent the first five readable results. It skips non-web links, PDFs, archives, images, office documents and duplicates. The agent fetches those pages and answers from them, citing the URLs it used. You aren't asked to approve the search, since you typed it, or those five pages, since you picked them by running the command; both are recorded in the audit log.

**`/search session <terms>`** looks through this session's saved transcript for the words or `"quoted phrases"`, ignoring case, including what `/compact` summarized away. Up to 12 matching messages, with about 300 characters around each match, go to the agent, which says what was said or decided and when.

Both are read-only turns: the agent can read, fetch and search, but edits, commands and MCP tools are refused.

### Limitations

- Only those five pages are pre-approved. Any other page the agent wants asks, and a redirect to another host is refused.
- Readability is judged from the URL: a page that turns out to be a PDF, or needs JavaScript, comes back empty or as an error.
- Session search is literal: no stemming or meaning-based search (`retries` doesn't find `retry`). It covers the current session only, and the transcript holds prompts and replies, not tool output.
- With Google, results are the pages Gemini chose to cite, often fewer than ten; Google's redirect links are resolved to the real pages. Only a Gemini API key works; Vertex AI credentials aren't supported for search.

## The browser

For pages that need JavaScript or clicking through, and to check a web app you're building, the agent has a `browser` tool: it drives Chrome, Chromium, Edge or Brave (whichever is installed; `blitz doctor` says which, `browser.path` picks one) in a throwaway profile, never yours, headless unless `browser.visible = true`. It can open pages, click, type, choose options, read the text or HTML, read the console, take screenshots it then looks at, and record a walkthrough: a screenshot after each step, saved in `~/.blitz/walkthroughs/`.

It follows the same rules as `web_fetch`: each new site asks first (approving one covers both tools), `web.allow_domains` doesn't ask, and `web.deny_domains` and `deny web(…)` rules are never reached. Everything the browser loads, scripts and WebSockets included, goes through Blitz, which refuses your machine's and network's own addresses. To test an app on `localhost`, allow them:

```toml
[browser]
allow_local = true     # localhost and private addresses; cloud metadata never
# allow_scripts = true # run the agent's page scripts without asking
# visible = true       # watch it work
```

Running a script the agent wrote in a page asks first unless `allow_scripts` is set. Set `[browser] enabled = false` to turn the tool off.
