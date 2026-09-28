---
title: "008 · Web"
weight: 8
---

*Web fetch and web search* (`spec_web_008`)

| | |
|---|---|
| Status | Implemented. Reverse-engineered from `53f8c53` (2026-09-24) and kept with the code since; its paths and names checked 2026-09-27 (`//tools/specs`). |
| Source | `pkg/engine/tools/web.go`, `websearch.go`, `usersearch.go`; `/search` in `pkg/engine/context.go` and `apps/cli/internal/tui` |
| Tests | `pkg/engine/tools/web_test.go`, `websearch_test.go`, `usersearch_test.go`; `apps/cli/internal/tui/search_test.go` |
| Depends on | [spec_approvals_005](spec_approvals_005.md) |

## 1. Purpose

`web_fetch` reads public pages as text; `web_search` queries a configured provider. Both are gated by approval and SSRF protection. The user's own `/search web` runs a search without approval and pre-approves exactly the pages it hands to the agent for that turn.

## 2. `web_fetch`

Args `url`. Result `url, final_url, status, content_type, content, truncated, error`. Registered only when `web.enabled`.

- **WEB-01** Refused when `sandbox.allow_network = false`. Only `http`/`https`, a host is required, embedded credentials are refused, `web.deny_domains` are refused.
- **WEB-02** Domain globs use `path.Match` on the lower-cased host without a trailing dot; `*.example.com` also matches `example.com`.
- **WEB-03** Authorisation order: host in `web.allow_domains` → no approval; URL in the turn's fetch grants (compared without fragment) → no approval, audited as approval with decision `user-selected`; otherwise approval (kind `network`, key `web:<host>`, target = host).
- **WEB-04** SSRF: the dialer's `Control` hook checks the **actual IP dialled after DNS resolution** — on every connection and redirect hop — and refuses loopback, private, link-local (incl. cloud metadata `169.254.169.254`), multicast, unspecified, CGNAT `100.64.0.0/10` and `0.0.0.0/8`, unless `web.allow_private`. Proxies are disabled (a proxy would bypass the check).
- **WEB-05** At most 5 redirects; each hop is re-checked; a redirect to a different host is refused unless that host is in `allow_domains` ("fetch that URL directly if needed").
- **WEB-06** Timeouts: dial 10 s, TLS 10 s, overall/headers `web.timeout_seconds` (20). Body capped at `web.max_bytes` (2 MiB) with `truncated`.
- **WEB-07** Content: HTML/XHTML converted to text (scripts, styles, noscript, svg, template, iframe dropped; block elements become line breaks; headings `#`…; list items `- `; absolute link targets appended `(url)`; blank lines collapsed). Text, JSON, XML, JavaScript and missing types pass through. Other types → "unsupported content type". Output capped at 100 KiB chars (UTF-8 safe), invalid UTF-8 replaced. Status ≥ 400 sets `error: "HTTP n"` but still returns content.
- **WEB-08** User-Agent `blitz/2 (+web_fetch)`.

## 3. `web_search`

Args `query`, `max_results?` (default `web.search_max_results` or 5, max 20). Result `query, results[{title,url,snippet}], answer?, error`. Registered only when web is enabled and `web.search_provider` is set; a misconfigured provider fails tool creation (and `doctor` reports it).

- **WEB-10** Providers: `brave` (key from `search_api_key` or `BRAVE_API_KEY`), `tavily` (`TAVILY_API_KEY`), `searxng` (`search_url` required; the instance must enable JSON output), `google` (Gemini grounding with Google Search; key from `search_api_key`, `[llm.gemini] api_key` or `GEMINI_API_KEY`; model `web.search_model` else `llm.gemini.model`; Vertex credentials unsupported). Unknown provider or missing key/URL are configuration errors with guidance.
- **WEB-11** Google: results are the pages Gemini cited; its grounding redirect links are resolved to their targets in parallel (unresolvable ones are kept and later dropped by `/search web`); `answer` is Gemini's summary; chunk snippets are the answer text they support. Google's Custom Search JSON API is deliberately unsupported (shuts down 2027-01-01).
- **WEB-12** The agent's query leaves the machine, so it needs approval (kind `network`, key `search:<provider>`, target = provider).
- **WEB-13** Results: non-http(s) and denied domains dropped; titles ellipsized to 200; snippets HTML-stripped and ellipsized to 400; response body capped at 2 MiB. Errors: 401/403 "authentication failed", 429 "rate limited", other "HTTP n: <body…>".
- **WEB-14** Refused without network access; empty query is an error.

## 4. User searches

- **WEB-20** `/search web <terms>` (user-initiated, [spec_workspace_018](spec_workspace_018.md) WS-54): no approval; audited `user_search` "<provider>: <query>". The top ≤5 viable links are handed to the agent as a **read-only** turn with fetch grants for exactly those URLs; every other fetch still asks. The transcript records the `/search` command, not the generated prompt.
- **WEB-21** `/search session <terms>`: literal, case-insensitive search of the transcript (words or `"quoted phrases"`), up to 12 messages with ~300 chars of context; run as a read-only turn ([spec_sessions_017](spec_sessions_017.md)).

## 5. Known limitations
- Readability is judged from the URL extension; PDFs without an extension or JS-only pages come back empty or as errors.
- Search charges (Google grounding) are not included in `/cost`.
- No Anthropic server-side web search (decision).
