---
title: "redact"
weight: 10
---

Masking secrets before they're written. Source: [`pkg/redact`](https://github.com/retail-cortex/blitz/tree/main/pkg/redact).

`pkg/redact` masks secret values in text before it reaches the audit log, the diagnostic log or telemetry. `pkg/engine` builds one from the configured API keys, the web search key, every MCP server's `env` values, and the values of scrubbed environment variables.

## API

| Name | |
|---|---|
| `Redactor`, `New(values…)` | Mask these values |
| `FromEnv(patterns, extra…)` | Mask the values of matching environment variables |

## Used by

pkg/engine, pkg/engine/audit, pkg/observability.

Spec: [observability](../about/specs/spec_observability_003.md).
