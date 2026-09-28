---
title: Logs and telemetry
weight: 100
---

Spec: [observability](../about/specs/spec_observability_003.md).

## Diagnostic log

`~/.blitz/logs/blitz-YYYY-MM-DD.jsonl` (owner-only, secrets masked, kept `log.retain_days`, 14 by default) records warnings, failed turns and errors, with trace IDs when telemetry is on. `log.level` (or `BLITZ_LOG_LEVEL`) is `debug`, `info`, `warn`, `error` or `off`. A background goroutine writes it, so logging never waits on the disk.

The [audit log](safety.md#audit-log) is separate: a record of what the agent did, not of Blitz's own health.

## OpenTelemetry

Telemetry is off by default, and nothing is sent unless you turn it on. With `[telemetry] enabled = true` (or `BLITZ_TELEMETRY=1`), traces and logs are exported over OTLP/HTTP to `telemetry.endpoint`, else `OTEL_EXPORTER_OTLP_ENDPOINT`, else `http://localhost:4318`. Other `OTEL_EXPORTER_OTLP_*` settings apply.

```toml
[telemetry]
enabled = true
endpoint = "http://localhost:4318"
# capture_content = false
```

- **One trace per prompt**: a `turn` span (agent, model, token counts, cost) containing the ADK's agent, model-call and tool spans, plus `approval` (time spent waiting on you), `hook` and `compact` spans.
- **Sessions are chains of turns**, not one long trace, because sessions last days and resume in new processes. Every span carries the session as `gen_ai.conversation.id`, and each `turn` has a `turn.index` and a link to the previous turn, even after `--resume`.
- **No content by default.** Prompts, replies, tool arguments and tool results aren't exported; Blitz removes the ADK's tool arguments and results from its spans before export. `capture_content = true` includes them, with secrets masked.
- Export runs on background goroutines and gives up after 3 seconds at exit if the collector is unreachable.
