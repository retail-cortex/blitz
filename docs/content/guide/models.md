---
title: Models
weight: 30
---

Spec: [models](../about/specs/spec_models_015.md).

## Resilience

Model requests are retried on rate limits, overload, 5xx responses and dropped connections, with exponential backoff that honours `Retry-After` (`llm.max_retries`, default 3; `0` turns retries off). A request that sends nothing for `llm.stall_timeout_seconds` (default 600) fails instead of hanging the turn. A stream that fails partway through isn't retried, because its text has already been shown. At most `tools.max_parallel` (default 8) tool calls from one model response run at once.

## Models on your cloud

Claude runs on Amazon Bedrock and Google Cloud's Vertex AI, and OpenAI's models and Claude on Azure, each with the cloud's own sign-in:

```toml
[llm]
provider = "bedrock"            # AWS credentials as the AWS CLI finds them, or AWS_BEARER_TOKEN_BEDROCK
[llm.bedrock]
region = "us-east-1"
model  = "us.anthropic.claude-sonnet-4-5-20250929-v1:0"

# provider = "azure"            # api_key, or auth = "entra" for az login and managed identities
# [llm.azure]
# resource = "my-foundry"
# model    = "gpt-5"            # the deployment; one named claude-… goes through Foundry's Anthropic API

# provider = "vertex-anthropic" # Claude on Vertex AI: [llm.anthropic] project_id and location, gcloud's login
```

A model reference can name them too, for fallbacks or pinned agents: `bedrock/us.anthropic.claude-haiku-4-5-20251001-v1:0`, `azure/gpt-5-mini`. The desktop app's provider settings don't offer them yet: set them in the settings file.

## Listing models

`blitz models` asks each provider you've set up which models it offers, and shows the price Blitz knows for each; `blitz models anthropic` asks one.

## Keys from a password manager

Instead of a key in the settings, a command can print it: Blitz runs it when it needs the key and again after `api_key_ttl`, so a key your gateway rotates keeps working.

```toml
[llm.anthropic]
api_key_command = "op read op://work/anthropic/api-key"
api_key_ttl     = "15m"   # default 5m
```

## Proxies and company certificates

Blitz uses `HTTPS_PROXY`, `HTTP_PROXY` and `NO_PROXY` for model APIs, MCP servers, web search and `web_fetch` (which still refuses private addresses: it checks where a host resolves before the proxy connects). If a proxy inspects TLS with the company's own certificate authority, trust it:

```toml
[network]
ca_file = "~/certs/company-root.pem"
```

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

A pin wins over an agent's own `default_model`, and both win over the configured model. Pinned agents keep the fallback chain. `/pin_model` and `/unpin` change pins and save them in the workspace's own settings (`~/.blitz/workspaces/<name>-<hash>/.env.toml`), so each workspace keeps its own: a pin there wins over one in the global settings, and `/unpin` of an agent the global settings pin saves `agent = ""`, which masks the global pin in that workspace only. Pins in the global `.env.toml` apply to every workspace that doesn't set its own.

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

Current Claude models (Opus 4.7 and later, Sonnet 5, Fable) decide how much to think themselves and take no budget: on them `thinking_budget` turns thinking on at the effort it stands for (up to 4096 `low`, 16384 `medium`, 65536 `high`, beyond that `max`), unless `reasoning_effort` is set. Opus 5.5 and Fable always think, so `0` can't turn it off there; `/model_settings` says so.

The key is the model name. Every model uses its own settings: the main model, pinned agents, and each fallback. `/model_settings gpt-5 temperature=0.3` changes them from the next model call and saves them. A setting the provider doesn't accept is left out of the request, and `/model_settings` warns.

`/effort high` (or `--effort high`) sets the reasoning effort for every model call in the session, over each model's own; `/effort auto` goes back to them.

## Context and cost

History is compacted automatically once a prompt reaches `context.token_threshold` tokens, or on demand with `/compact [focus]`. List prices for the default models are built in; override or add them under `[pricing."model-name"]` (`input_per_mtok`, `output_per_mtok`, `cached_input_per_mtok`, `cache_write_per_mtok`). `blitz doctor` warns when the active model has no price. `/cost` shows the session's tokens, including cache reads and writes, and its estimated cost. Web searches through Google are billed per query and shown on their own line, priced by `[search_pricing.google] per_1k_queries` (default $14).
