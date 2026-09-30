---
title: "008 · Web"
weight: 8
---

*Web fetch, web search and the browser* (`spec_web_008`)

| | |
|---|---|
| Status | Implemented. Reverse-engineered from `53f8c53` (2026-09-24) and kept with the code since; its paths and names checked 2026-09-27 (`//tools/specs`). |
| Source | `pkg/engine/tools/web.go`, `websearch.go`, `usersearch.go`, `browser.go`; `pkg/engine/browser`; `/search` in `pkg/engine/context.go` and `apps/cli/internal/tui` |
| Tests | `pkg/engine/tools/web_test.go`, `websearch_test.go`, `usersearch_test.go`, `browser_test.go`; `pkg/engine/browser/browser_test.go` (a real browser with `BLITZ_TEST_CHROME=1`), `fake_test.go` (against `browsertest`, a fake DevTools page); `apps/cli/internal/tui/search_test.go` |
| Depends on | [spec_approvals_005](spec_approvals_005.md) |

## 1. Purpose

`web_fetch` reads public pages as text; `web_search` queries a configured provider. Both are gated by approval and SSRF protection. The user's own `/search web` runs a search without approval and pre-approves exactly the pages it hands to the agent for that turn.

## 2. `web_fetch`

Args `url`. Result `url, final_url, status, content_type, content, truncated, error`. Registered only when `web.enabled`.

- **WEB-01** Refused when `sandbox.allow_network = false`. Only `http`/`https`, a host is required, embedded credentials are refused, `web.deny_domains` are refused.
- **WEB-02** Domain globs use `path.Match` on the lower-cased host without a trailing dot; `*.example.com` also matches `example.com`.
- **WEB-03** Authorisation order: host in `web.allow_domains` → no approval; URL in the turn's fetch grants (compared without fragment) → no approval, audited as approval with decision `user-selected`; otherwise approval (kind `network`, key `web:<host>`, target = host).
- **WEB-04** SSRF: the dialer's `Control` hook checks the **actual IP dialled after DNS resolution** — on every connection and redirect hop — and refuses loopback, private, link-local (incl. cloud metadata `169.254.169.254`), multicast, unspecified, CGNAT `100.64.0.0/10` and `0.0.0.0/8`, unless `web.allow_private`. Through a proxy from the environment (`HTTPS_PROXY`, `NO_PROXY`), the proxy connects instead: the host is resolved first and refused unless every address is public (with `allow_private`, allowed), and only the proxy's own address is dialled without the check ([spec_models_015](spec_models_015.md) MDL-54).
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
- **WEB-15** Google bills each search query Gemini runs for a grounded answer: the count (the grounding metadata's `webSearchQueries`) goes into the session's usage for `web_search` (the tool call's session, or its owner's) and `/search web` (the active session), priced by `[search_pricing."<provider>"] per_1k_queries` (default `google` $14 per 1,000, the published rate past the free tier) and saved with the session. `/cost` shows it on a line of its own ("Web searches: 3 queries, estimated $0.0420"); it is never part of the token cost (`Usage.SearchQueries`, `SearchCostUSD`; the service's `Usage.search_queries`, `search_cost_usd`). The Gemini tokens the grounded call itself uses aren't counted (BL-WEB-01).

## 4. User searches

- **WEB-20** `/search web <terms>` (user-initiated, [spec_workspace_018](spec_workspace_018.md) WS-54): no approval; audited `user_search` "<provider>: <query>". The top ≤5 viable links are handed to the agent as a **read-only** turn with fetch grants for exactly those URLs; every other fetch still asks. The transcript records the `/search` command, not the generated prompt.
- **WEB-21** `/search session <terms>`: literal, case-insensitive search of the transcript (words or `"quoted phrases"`), up to 12 messages with ~300 chars of context; run as a read-only turn ([spec_sessions_017](spec_sessions_017.md)).

## 5. `browser`

The agent drives a real browser, for pages that need JavaScript or interaction and to check a web app the user is building (spec_parity_027 PAR-TOOL-10..12).

- **WEB-30** Offered (to the `blitz` agent) with `web.enabled` and `[browser] enabled` (both default on); refused without network access. The browser is Chrome, Chromium, Edge or Brave (`browser.path`, else the first installed; `blitz doctor` says which), started on first use with `--remote-debugging-port=0` in a new temporary profile (never the user's; removed on close), headless unless `browser.visible`, viewport `browser.width`×`height` (1280×800). One browser per workspace, one action at a time; it closes after 15 minutes unused, on `close`, and with the workspace.
- **WEB-31** One tool, `browser`, with `action`: `navigate` (url), `back`, `click` (selector; a real mouse click at the element's centre), `type` (selector, text; replaces the field unless `append`; `submit` presses Enter), `select` (selector, value or option text), `read` (the page's or element's text), `html` (outerHTML), `screenshot` (`full_page`), `console`, `script`, `record`, `stop_recording`, `close`. A selector is CSS, or `text=<visible text>` (exact, else contained, among visible elements). Every result carries the page's URL and title, the console messages and uncaught errors since the last action (up to 200), and what was refused. Text, HTML and script results are capped at 100 KiB. Actions that may navigate wait for the load (the timeout is `web.timeout_seconds`, at least 30s), including pages restored from the back/forward cache.
- **WEB-32** All the browser's traffic goes through an in-process HTTP proxy on loopback (`--proxy-server`, with `<-loopback>` so loopback is proxied too): plain requests are forwarded, https and WebSockets tunnelled. It refuses hosts in `web.deny_domains` or under a `deny web(…)` rule, and addresses that aren't public as dialled (after DNS, so a name resolving or rebinding to an internal address is refused): loopback and private ranges are allowed only with `browser.allow_local`, link-local (cloud metadata) never.
- **WEB-33** `navigate` is decided like `web_fetch` (WEB-04; the same approval key `web:<host>`, so approving a host covers both): http(s) only, no credentials in the URL, `deny_domains` and deny rules refuse, `localhost`, `*.localhost` and literal non-public addresses need `browser.allow_local`, link-local and unspecified addresses are refused outright, `web.allow_domains` go ahead, anything else asks. Nothing starts a browser before the decision.
- **WEB-34** Top-level page loads the page starts itself (links, redirects, forms, scripts) are intercepted (the DevTools `Fetch` domain, documents of the main frame): the current page's host and `allow_domains` go ahead; another host is asked about when an action is running, and refused otherwise (the tool reports it and says to navigate there to ask). Frames and subresources are left to the proxy.
- **WEB-35** `script` runs a JavaScript expression in the page (awaited; the value returned as JSON unless a string); it needs approval (kind `run_command`, key `browser:script`) unless `browser.allow_scripts`. The tool isn't allowed in plan mode.
- **WEB-36** `screenshot` is a PNG put in the image store and shown to the model after the result, like `view_image` ([spec_images_011](spec_images_011.md)).
- **WEB-37** `record` starts a walkthrough: after each action that may change the page (navigate, back, click, type, select, script), a screenshot is saved as `NNN-<action>.png` in `~/.blitz/walkthroughs/<yyyymmdd-hhmmss>/` (owner-only); `stop_recording` returns the folder and its files.
- **WEB-38** `[browser]` is never taken from project settings ([spec_project_config_031](spec_project_config_031.md)).

## 6. Known limitations
- Readability is judged from the URL extension; PDFs without an extension or JS-only pages come back empty or as errors.
- Search charges (Google grounding) are not included in `/cost`.
- No Anthropic server-side web search (decision).
- The browser records screenshot series, not video. One page (tab) only; no file uploads or downloads, and no dialogs (`alert`, `confirm`) handling.
- A browser can't run in the Bazel test sandbox on macOS: the real-browser tests need `--strategy=TestRunner=local` too.
