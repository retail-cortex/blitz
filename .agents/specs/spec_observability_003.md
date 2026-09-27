# spec_observability_003 — Audit log, diagnostic log, telemetry and secret redaction

| | |
|---|---|
| Status | Implemented (reverse-engineered from `53f8c53`) |
| Source | `internal/audit/audit.go`, `internal/redact/redact.go`, `internal/observability/{logfile,telemetry,span}.go`; wiring in `cmd/blitz/setup.go`, `internal/app/workspace.go` |
| Tests | `internal/audit/audit_test.go`, `internal/redact/redact_test.go`, `internal/observability/observability_test.go`, `internal/runtime/telemetry_test.go` |
| Depends on | [spec_config_002](spec_config_002.md) |

## 1. Purpose

Three records, each with secrets masked and none able to break a session:
- the **audit log** — what the agent and the user did (for accountability);
- the **diagnostic log** — warnings and errors (for debugging);
- **OpenTelemetry** traces and logs — opt-in, content-free by default.

## 2. Redaction

- **RED-01** Built-in patterns are masked as `[REDACTED]`: PEM private keys; AWS access key IDs (`AKIA…`); OpenAI/Anthropic keys (`sk-`, `sk-ant-`, `sk-proj-`); Google API keys (`AIza…`); GitHub tokens (`ghp_ gho_ ghu_ ghs_ ghr_`, `github_pat_`); Slack tokens (`xox[abprs]-`); JWTs.
- **RED-02** Assignment-style secrets (`password=`, `api_key:`, `token=`, `secret`, `access_key`…, case-insensitive, value ≥ 6 chars): only the value is masked.
- **RED-03** Exact values are masked too: configured API keys (Gemini, OpenAI, Anthropic, web search), every MCP `env` value, and the values of environment variables matching `sandbox.scrub_env` globs. Values shorter than 8 chars are ignored (false positives); longest first.
- **RED-04** `Value` masks recursively inside maps and slices (tool arguments) and returns a copy. A nil redactor still applies the built-in patterns.

## 3. Audit log

- **AUD-01** `~/.blitz/audit/audit-YYYY-MM-DD.jsonl` (UTC day rotation), directory 0700, files 0600, append-only. Disabled with `[audit] enabled = false` (then a nil logger no-ops everywhere).
- **AUD-02** Entry: `time` (UTC), `session`, `kind`, `tool`, `args`, `detail`, `decision`, `error`, `workspace`. Session and workspace default to the context set when the session opened.
- **AUD-03** Kinds: `session_start`, `prompt`, `tool_call` (name, args), `tool_result` (ok/error), `approval` (decision: `once|session|always|auto-policy|session-rule|saved-rule|user-selected|unattended-permitted`), `denial` (`deny|no-approver|error|unattended-refused`), `hook` (`block` or error), `undo` (files), `attachment` (path and hash only), `user_shell` (`!cmd`, `exit N`, start error), `user_search` (`/search web` provider and query).
- **AUD-04** Detail, error and args are redacted before writing. Write failures are reported once to stderr and otherwise ignored.

## 4. Diagnostic log

- **LOG-01** `~/.blitz/logs/blitz-YYYY-MM-DD.jsonl` (`log.dir`), owner-only, slog JSON records. Level `log.level` or `BLITZ_LOG_LEVEL`: `debug|info|warn|error|off`. Files older than `log.retain_days` (14) are deleted (0 = keep).
- **LOG-02** Writes never block callers: records go to a 1024-entry queue drained by one writer goroutine with group commit; when full, lines are dropped and counted, and the count is written once there is room.
- **LOG-03** String and error attributes are redacted before encoding. With telemetry on, lines carry trace and span IDs.
- **LOG-04** With level `off`, only extra handlers (the OTel log bridge) receive records, at info level. A failure to open the log disables it with a warning.
- **LOG-05** The CLI logs `start` (version, provider, model, telemetry), failed turns, and `exit` with code and error when non-zero. The service logs workspace opens and model problems per workspace.

## 5. Telemetry (OpenTelemetry)

- **OTEL-01** Off by default; nothing is sent unless `[telemetry] enabled = true` or `BLITZ_TELEMETRY=1`. Exports traces and logs over OTLP/HTTP to `telemetry.endpoint`, else `OTEL_EXPORTER_OTLP_ENDPOINT`, else `http://localhost:4318`. Other `OTEL_EXPORTER_OTLP_*` settings apply. Service name `blitz`.
- **OTEL-02** Content is never exported by default: prompts, replies, tool arguments and tool results are removed from spans (the ADK sets tool args/results on every `execute_tool` span regardless, so Blitz filters them in its own exporter). `capture_content = true` includes them with secrets masked (and sets `OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT` for the ADK).
- **OTEL-03** Providers are built by Blitz, not `adk/telemetry.New` (which adds its own unfiltered exporter). One provider per process: the ADK binds its tracer to the first global provider.
- **OTEL-04** Spans: `turn` (root per prompt; see [spec_engine_016](spec_engine_016.md) ENG-90/91), the ADK's agent/model/tool spans, `approval` (time waiting on the user), `hook <event>`, `compact`, `btw`. Every span carries `gen_ai.conversation.id`. Span attributes must not carry content — sizes, names and outcomes only. Cancellation is not recorded as an error.
- **OTEL-05** Export runs on SDK batch goroutines; shutdown gives up after 3 s if the collector is unreachable.
- **OTEL-06** Telemetry start failure warns and disables telemetry only.
