---
title: Search
weight: 70
---

Spec: [web](../about/specs/spec_web_008.md).

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
