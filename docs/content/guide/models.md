---
title: Models
weight: 30
---

Spec: [models](../about/specs/spec_models_015.md).

With no model set, Blitz uses Gemini's `gemini-4-argon` (for chat and for Google web searches). Choose another with `/model provider/model` in a session, `--model`, or `model` under `[llm.<provider>]` in the settings; a model you've set is kept across releases.

## Resilience

Model requests are retried on rate limits, overload, 5xx responses and dropped connections, with exponential backoff that honours `Retry-After` (`llm.max_retries`, default 3; `0` turns retries off). A request that sends nothing for `llm.stall_timeout_seconds` (default 600) fails instead of hanging the turn. A connection that stops answering altogether, as when a VPN or a network change drops it, is noticed within a minute and the request is retried on a new one. A stream that fails partway through isn't retried, because its text has already been shown. At most `tools.max_parallel` (default 8) tool calls from one model response run at once.

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

## A gateway with its own sign-in

The `api` provider sends requests to your company's model gateway, with the headers you give it, in the API each model speaks: a `claude-…` model gets Anthropic's Messages API (streaming, prompt caching and thinking included), any other model OpenAI's Responses API. Both go to `<base_url>/responses`, as gateways that pass each vendor's request through expect. Where a header (or the URL's path or query) says `{{TOKEN_SCRIPT}}`, Blitz puts the token your script prints:

```toml
[llm]
provider = "api"

[llm.api]
base_url = "https://llm-gateway.example.com/v1"   # requests go to <base_url>/responses
model    = "gpt-5"
token_script          = "gateway-cli token --audience llm"   # prints one line: the token
token_script_language = "shell"                                # or python, typescript (Node 22.6+)

[llm.api.headers]
Authorization = "Bearer {{TOKEN_SCRIPT}}"
X-Team        = "platform"
```

The script runs when a session first needs the token, with the service's environment and outside the sandbox, and must finish within 30 seconds. Each session keeps its token in memory; a `401` or `403` runs the script once more and retries, and a second one fails with the message to check the script. No `OPENAI_API_KEY` or `ANTHROPIC_API_KEY` from the environment reaches the gateway. Responses API requests are never stored (`store: false`) or streamed. Requests follow the model: `temperature` and `top_p` aren't sent to models that refuse them (current Claude models, OpenAI's reasoning models), and a setting the gateway refuses for a model is left out of that model's requests from then on. Errors start `[llm.api]` and quote the gateway's reason. A project's settings can't set `[llm.api]`.

In the desktop app, choose **API** in **Settings › Providers & keys**: the endpoint, a row per header (**Use the token** puts `{{TOKEN_SCRIPT}}` in it) and the script. Header values you've saved aren't shown again; leave one empty to keep it. `blitz doctor` shows the endpoint and the header names, and `blitz doctor --online` runs the script and sends a request.

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
