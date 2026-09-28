---
title: "observability"
weight: 9
---

The diagnostic log and OpenTelemetry. Source: [`pkg/observability`](https://github.com/retail-cortex/blitz/tree/main/pkg/observability).

`pkg/observability` provides the diagnostic log (`slog`, JSON lines, secrets masked, daily files with retention) and OpenTelemetry export of traces and logs over OTLP/HTTP. Both are fed from the goroutines doing the work and written by dedicated background goroutines, so logging and tracing never wait on disk or network. Spans are stripped of prompt and tool content unless `capture_content` is on. See [logs and telemetry](../guide/telemetry.md).

## API

| Name | |
|---|---|
| `OpenLog` | The diagnostic log |
| `Telemetry`, `StartTelemetry`, `NewTelemetry` | The OpenTelemetry providers and exporters |
| `Start`, `StartWith`, `End` | Spans |
| `ConversationID` | The `gen_ai.conversation.id` attribute every span carries |
| `Traceparent`, `ParseTraceparent` | Linking a turn to the previous one across processes |

## Used by

apps/cli, pkg/engine, pkg/engine/runtime, pkg/engine/tools.

Spec: [observability](../about/specs/spec_observability_003.md).
