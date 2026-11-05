---
title: "guardrails"
weight: 10
---

Blitz's guardrails on a model's request and reply. Source: [`pkg/guardrails`](https://github.com/retail-cortex/blitz/tree/main/pkg/guardrails).

`pkg/guardrails` runs the guardrails for a program that has a model's request and reply rather than an engine turn, such as a gateway between Blitz and the models ([spec_053_enterprise](../about/specs/spec_053_enterprise.md) EN-23). The rules and their checks are the engine's own (`pkg/engine/gates`), so a guardrail means the same there as on the desktop. The engine's packages stay private: this package names their types for other programs.

## API

| Name | |
|---|---|
| `Guardrail`, `Limits`, `Asker` | The engine's guardrail, the limits it holds a turn to, and the judge |
| `Defaults()`, `Load(dirs…)`, `Parse(name, data)` | The built-in guardrails at their defaults; guardrail files |
| `Set` | The guardrails one request runs under: for an agent, with a judge and a redactor |
| `Set.Input(text)` | The secrets guardrail on what the client sends: masked or refused |
| `Set.ToolResult(ctx, tool, external, text)` | A tool's result going back to the model: injection taken out, custom checks judged |
| `Set.ToolCall(ctx, tool, args, external)` | A tool call the model makes: the limits guardrail's domains and commands, custom checks judged |
| `Set.Output()`, `Output.Write`, `Output.Flush` | The secrets guardrail on the model's text as it streams, over a window held back so a secret split between pieces is still caught; a private key is held until it's whole |
| `Hold`, `Hold.Write`, `Hold.Flush` | The hold-back `Output` uses, with any check: the engine holds the agent's streamed answer with it, checked by the workspace's guardrails |
| `Finding` | What a guardrail did, for a log |

Custom output guardrails judge a whole answer, which a gateway has only once it's been sent, so they aren't run here.

## Used by

The enterprise service's model gateway, and the engine (`Hold`).

Spec: [enterprise](../about/specs/spec_053_enterprise.md).
