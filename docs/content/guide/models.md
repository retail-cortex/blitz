---
title: Models
weight: 30
---

Spec: [models](../about/specs/spec_models_015.md).

## Resilience

Model requests are retried on rate limits, overload, 5xx responses and dropped connections, with exponential backoff that honours `Retry-After` (`llm.max_retries`, default 3; `0` turns retries off). A request that sends nothing for `llm.stall_timeout_seconds` (default 600) fails instead of hanging the turn. A stream that fails partway through isn't retried, because its text has already been shown. At most `tools.max_parallel` (default 8) tool calls from one model response run at once.

## Fallback models

If the model fails before answering (after its retries: an outage, a rate limit, bad credentials), the next one in `llm.fallback_models` answers instead:

```toml
[llm]
provider = "gemini"
fallback_models = ["anthropic/claude-sonnet-5", "gemini-3.5-flash-lite"]
```

Each provider uses its own credentials. A failed model is skipped for 15 seconds, doubling up to 5 minutes, then tried again. You see one notice when a fallback takes over and one when the primary is back, and cost is priced by the model that answered. For OpenRouter names that contain a slash, write the provider first: `openai/anthropic/claude-sonnet-5`.

## Per-agent models

Agents can run on different models, for example a cheaper one for reviews:

```toml
[agent_models]
qa = "anthropic/claude-haiku-4-5"
```

A pin wins over an agent's own `default_model`, and both win over the configured model. Pinned agents keep the fallback chain. `/pin_model` and `/unpin` change pins and save them.

## Per-model settings

```toml
[model_settings."gpt-5"]
temperature = 0.3
top_p = 0.9
max_tokens = 4096
seed = 7

[model_settings."claude-opus-5-5"]
reasoning_effort = "high"   # minimal, low, medium, high or max
thinking_budget = 16000     # 0 turns thinking off
```

The key is the model name. Every model uses its own settings: the main model, pinned agents, and each fallback. `/model_settings gpt-5 temperature=0.3` changes them from the next model call and saves them. A setting the provider doesn't accept is left out of the request, and `/model_settings` warns.

`/effort high` (or `--effort high`) sets the reasoning effort for every model call in the session, over each model's own; `/effort auto` goes back to them.

## Context and cost

History is compacted automatically once a prompt reaches `context.token_threshold` tokens, or on demand with `/compact [focus]`. List prices for the default models are built in; override or add them under `[pricing."model-name"]` (`input_per_mtok`, `output_per_mtok`, `cached_input_per_mtok`, `cache_write_per_mtok`). `blitz doctor` warns when the active model has no price. `/cost` shows the session's tokens, including cache reads and writes, and its estimated cost.
