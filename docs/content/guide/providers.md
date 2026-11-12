---
title: Providers
weight: 5
---

Spec: [models](../about/specs/spec_015_models.md).

A provider is where Blitz's models come from, and setting one up is the first thing Blitz needs: until a provider has a key or a sign-in, no chat, job or eval can run. Pick the provider you already have access to; you can add more later for [fallbacks](models.md#fallback-models) and [per-agent models](models.md#per-agent-models).

| Provider | `provider =` | Signs in with | Models |
|---|---|---|---|
| Google Gemini (the default) | `gemini` | API key, Google Cloud (ADC) or a Google account | Gemini, on the Gemini API or Vertex AI |
| Anthropic | `anthropic` | API key or an Anthropic account | Claude |
| Claude on Vertex AI | `vertex-anthropic` | Google Cloud (ADC) | Claude, in your Google Cloud project |
| OpenAI and compatible APIs | `openai` | API key | OpenAI's, or any OpenAI-compatible API's (OpenRouter, vLLM, …) |
| Ollama | `ollama` | nothing | Models running on your machine |
| Amazon Bedrock | `bedrock` | AWS credentials or AWS IAM Identity Center | Claude, in your AWS account |
| Azure | `azure` | API key or Entra ID | OpenAI's models, and Claude through Microsoft Foundry |
| Your company's gateway | `api` | Headers and a token a script prints | Whatever it serves: each model in its own API (OpenAI's, Anthropic's, Gemini's, Vertex AI's or an OpenAI-compatible server's) |

Keys go into your system keychain (the macOS Keychain, the Secret Service on Linux), never into a file you could commit. Settings are read only from `~/.blitz`, never from a project, so a cloned repository can't redirect your key.

## Set up in the desktop app

Until a provider is ready, the welcome page says so with **Set up a provider**. That opens **Settings › Providers & keys**, which lists every provider with where it stands:

- **Key in the keychain**, **Key found in the environment** or **Signed in**: it's ready. For Gemini, Anthropic, OpenAI and Ollama, **Activate** makes it available: its models are listed in the status bar's model menu and `/model`, beside the other active providers'. **Deactivate** takes it back (not while the default model is its). For a cloud's deployment or a gateway, **Use** makes it the one Blitz uses.
- **Google Cloud sign-in found** or **Anthropic Console sign-in found**: you're signed in on this machine (with `gcloud` or `ant`) but Blitz doesn't use it yet. **Set up** opens its form on that sign-in.
- **Running on this machine** (Ollama): it answered on its usual address, with the models it has. **Activate** lists them.
- **Not set up**: pick the row to fill in its form. **Get a key** opens the provider's page for API keys (for Ollama, **Get Ollama**).

To set one up by hand:

1. Pick the provider in the list.
2. Paste its API key. Blitz tries it as you paste, before anything is saved, and says whether it works and how many models it reaches. Or, with advanced settings, choose another way under **Sign in with** and press **Sign in**.
3. For Gemini, Anthropic, OpenAI and Ollama, choose its **Models in the menu**: the ones the model menus list for it. None chosen, they list its latest: the newest of each family (Flash, Pro, Opus, Sonnet, …), without older generations, previews or dated snapshots. For the others, set the model it uses.
4. Press **Save**: the provider is saved and active. (A cloud's deployment or a gateway: **Save and use**.) If the model still isn't available, the form says why.

**Default model**, at the top of the page, is the model new chats start on: one choice of every active provider's models. Any chat can switch to another from the status bar's model menu.

The same, in the settings file:

```toml
[llm]
provider  = "openai"                 # the default model's provider
providers = ["gemini", "openai"]     # the active ones; empty: every one set up

[llm.openai]
model  = "gpt-6.1-sol"               # the default model, with provider above
models = ["gpt-6.1-sol", "gpt-6.1-sol-mini"]   # the menus' models; empty: the latest
```

The form edits the settings every workspace shares. A workspace can use its own provider or key: open its workspace settings and set them there (its **Use the global setting** row follows the global ones).

## Set up on the command line

```bash
blitz config init                 # writes a commented ~/.blitz/.env.toml
blitz config set-key anthropic    # reads the key from stdin, stores it in the keychain
blitz doctor                      # checks the provider, the key and the model
```

Then choose the provider in `~/.blitz/.env.toml`:

```toml
[llm]
provider = "anthropic"

[llm.anthropic]
model = "claude-opus-5"            # optional: else the provider's default
```

`--model anthropic/claude-sonnet-5`, or `/model` in a session, switches to any provider you've set up. With `--workspace` (`-w`), the `blitz config` commands below change the current workspace's own settings.

## API keys

```bash
blitz config set-key gemini          # gemini, anthropic or openai
blitz config keys                    # where each provider's key comes from, and how it signs in
blitz config secure-key anthropic    # move a key written in the settings file into the keychain
blitz config remove-key openai
```

Where there's no keychain, keys go to an owner-only `~/.blitz/secrets.toml`. The settings file only refers to the key (`api_key = "keychain:…"`).

A key in the environment (`GEMINI_API_KEY`, `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`) works too, but the Blitz service started at login doesn't see your shell's environment: use the keychain for it.

**Keys from a password manager.** Instead of a key, a command can print it. Blitz runs it when it needs the key and again after `api_key_ttl`, so a key that's rotated keeps working:

```toml
[llm.anthropic]
api_key_command = "op read op://work/anthropic/api-key"
api_key_ttl     = "15m"   # default 5m
```

## Signing in without a key

Gemini, Claude, Amazon Bedrock and Azure can use an account instead of an API key. A key keeps working; an account is another way. OpenAI has no account sign-in for other apps: it takes an API key.

The sign-in belongs to the machine the Blitz service runs on, and Blitz can run it for you there: **Sign in** in **Settings › Providers & keys**, or on the command line:

```bash
blitz auth login google|anthropic|aws|azure   # runs gcloud, ant, aws or az, which opens your browser
blitz auth status                             # where each sign-in stands
blitz auth logout google|anthropic|aws|azure  # signs that provider's tool out
blitz config set-auth <provider> <api_key|adc|oauth>
```

Signing in after choosing the method is fine: Blitz tries the model again when you come back to the window, press **Check again** on the "model isn't available" note, or send a prompt. After signing in as someone else, save the provider settings (or restart the service) so Blitz picks up the new credentials. With an account sign-in, an API key in the settings or the environment is ignored, so a leftover key can't take its place; `blitz config set-auth <provider> api_key` goes back to the key.

## Google Gemini

The default provider. With an API key from [Google AI Studio](https://aistudio.google.com/apikey), `blitz config set-key gemini` is all it takes; without a model set, Blitz uses `gemini-4-argon`.

**On Google Cloud (Vertex AI).** Gemini can run in your Google Cloud project instead, billed there. The project needs the Vertex AI API enabled.

```bash
gcloud auth application-default login
blitz config set-auth gemini adc --project my-project      # --location, else global
```

```toml
[llm.gemini]
auth       = "adc"           # or "oauth": your Google account, signed in with gcloud
project_id = "my-project"    # or GOOGLE_CLOUD_PROJECT
location   = "global"        # or GOOGLE_CLOUD_LOCATION; global when unset
```

Gemini uses one credential, the first it finds: an API key (yours in the keychain, `api_key_command`, or `GEMINI_API_KEY`), so remove the key to use Google Cloud instead; then `GOOGLE_APPLICATION_CREDENTIALS` (a service account's key file, whose project is used when you set none); then your gcloud sign-in; then a Google Cloud machine's own. The agent's commands don't get these credentials unless you set `[sandbox] share_adc = true` ([secrets](safety.md#secrets)).

## Anthropic

Claude with an API key from the [Claude Console](https://console.anthropic.com/), `blitz config set-key anthropic`. Without `api_key`, Blitz also uses `ANTHROPIC_API_KEY` or `ANTHROPIC_AUTH_TOKEN`. Blitz streams, caches the system prompt, keeps thinking across tool calls, and falls back on server-side refusals (`fallbacks = "default"`, a model ID, or `"off"`).

**With an Anthropic account.** Claude can use a Claude Console sign-in from the `ant` command line tool, billed to the organization you pick when signing in. This is the Claude API's sign-in, not a Claude.ai subscription.

```bash
ant auth login                               # --profile work for more than one
blitz config set-auth anthropic oauth        # --profile work, else ant's active profile
```

```toml
[llm.anthropic]
auth    = "oauth"
profile = "work"             # optional: else ANTHROPIC_PROFILE, ant's active profile, or "default"
```

`base_url` sends Claude's requests through a proxy or gateway that speaks Anthropic's API.

## Claude on Vertex AI

Claude runs in your Google Cloud project, billed there, with the same sign-in as Gemini's. Enable the Claude models you use in Vertex AI's Model Garden first; model names stay the same (`claude-opus-5`).

```bash
gcloud auth application-default login
blitz config set-auth anthropic adc --project my-project --location global
```

```toml
[llm]
provider = "anthropic"

[llm.anthropic]
auth       = "adc"           # what set-auth wrote
project_id = "my-project"    # or GOOGLE_CLOUD_PROJECT
location   = "global"        # or GOOGLE_CLOUD_LOCATION, us-east5, europe-west1, …
```

`provider = "vertex-anthropic"` is the same without `auth`: it always uses Vertex AI, with `[llm.anthropic]`'s project and location. Use it as a model reference (`vertex-anthropic/claude-haiku-4-5`) to reach Claude on Vertex AI while the provider stays on an Anthropic key.

On Vertex AI, Claude has no server-side refusal fallback, so Blitz retries a refused request itself on the fallback: the model `fallbacks` names, or `claude-opus-4-8` for `"default"`. A conversation that fell back stays on the fallback; `fallbacks = "off"` turns it off.

## OpenAI and compatible APIs

OpenAI's models with an API key from the [OpenAI platform](https://platform.openai.com/api-keys), `blitz config set-key openai`, or `OPENAI_API_KEY`. `base_url` points Blitz at any OpenAI-compatible API instead, such as OpenRouter or a vLLM server:

```toml
[llm]
provider = "openai"

[llm.openai]
base_url = "https://openrouter.ai/api/v1"
model    = "anthropic/claude-sonnet-5"
```

For OpenRouter names that contain a slash, write the provider first in a model reference: `openai/anthropic/claude-sonnet-5`.

## Ollama

Models running on your own machine, with no key and nothing leaving it. Start [Ollama](https://ollama.com), pull a model, then:

```toml
[llm]
provider = "ollama"

[llm.ollama]
model    = "qwen3-coder"                 # empty: the first model Ollama has
base_url = "http://localhost:11434/v1"   # the default; change it for Ollama on another machine
```

Ollama has its own section, so its model and OpenAI's don't overwrite each other. Ollama loads a model on the first request after it starts, so there's nothing to start by hand. If a request names a model Ollama doesn't have, Blitz says which one and suggests `ollama pull`; if Ollama isn't running, it says to open the Ollama app or run `ollama serve`.

**What to expect from local models.** A model on your machine is much smaller than Gemini, Claude or GPT, and as an agent that shows. Agent work means choosing the right tool, calling it correctly, reading the result and going on, and small models often fail at it. They describe the steps ("run `git status`") instead of running them, write a tool call as text instead of making it, or make up an answer instead of looking it up. Blitz helps where it can: it runs tool calls a model writes as text, and starts each conversation with the workspace's git state and where the last conversation left off. But it can't make a small model reliable. For agent work, choose a model with strong tool calling, such as `qwen3:8b` or larger (`ollama show <model>` lists `tools` among its capabilities). Models made for code completion, such as `qwen2.5-coder`, are weaker at it. Small models are fine for chat, quick questions and private drafts. The desktop app says this under Ollama in Settings › Providers.

To help a small model, Blitz offers an Ollama model only its core tools: files, the shell, search, the web, asking you, opening a file, your connections' `db__` tools and the `blitz` tool. Given all ninety or so, a 7B model writes its calls as text or makes none. `[llm.ollama] all_tools = true` gives it every tool. When it writes several calls as text, one a line (a plan made before any result), Blitz runs the first and asks again with its result. In auto mode, an Ollama model doesn't review its own actions: Blitz asks you instead ([auto mode](safety.md)). In our tests, `qwen3` made its tool calls where `qwen2.5-coder:7b` mostly didn't, and Ollama stopped some of its replies with "token repeat limit reached" when it repeated itself.

## Amazon Bedrock

Claude in your AWS account, with AWS's own credentials: the environment, the shared config and credentials files, an SSO sign-in or the machine's role, or a Bedrock API key in `AWS_BEARER_TOKEN_BEDROCK`.

```toml
[llm]
provider = "bedrock"

[llm.bedrock]
region  = "us-east-1"        # else AWS_REGION, then the profile's
profile = "work"             # else AWS_PROFILE, then "default"
model   = "us.anthropic.claude-sonnet-4-5-20250929-v1:0"
auth    = "oauth"            # optional: AWS IAM Identity Center (aws sso login) for the profile
```

## Azure

OpenAI's models through Azure OpenAI, and Claude through Microsoft Foundry (a deployment whose name starts with `claude`). `model` is the deployment's name.

```toml
[llm]
provider = "azure"

[llm.azure]
resource = "my-foundry"      # <resource>.openai.azure.com and <resource>.services.ai.azure.com
model    = "gpt-5"
auth     = "entra"           # Entra ID: az login, a managed identity; else an API key
```

Without `auth = "entra"`, Blitz uses `api_key`, or `AZURE_OPENAI_API_KEY` (`ANTHROPIC_FOUNDRY_API_KEY` for Claude). `base_url` and `anthropic_base_url` replace the addresses made from `resource`.

The desktop app's provider form doesn't offer Bedrock, Azure or Claude on Vertex AI yet: set them in the settings file. A model reference can name them anywhere a model goes, for fallbacks or pinned agents: `bedrock/us.anthropic.claude-haiku-4-5-20251001-v1:0`, `azure/gpt-5-mini`.

## Your company's gateway

The `api` provider sends requests to your company's model gateway, with the headers you give it, in the API each model speaks. Where a header (or the URL's path or query) says `{{TOKEN_SCRIPT}}`, Blitz puts the token your script prints:

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

**Each model's API.** Blitz doesn't need your gateway's documentation: you say what each model speaks and where the gateway takes it. Without settings, a `claude-…` model gets Anthropic's Messages API (streaming, prompt caching and thinking included), a `gemini-…` model the Gemini API, and any other OpenAI's Responses API. `[llm.api.apis]` says otherwise, by model, with `"*"` for every model you don't name:

```toml
[llm.api.apis]
"claude-sonnet-5-5" = "messages"          # Anthropic's Messages API
"glm-5"             = "messages"          # an Anthropic-compatible model
"gemini-3-pro"      = "vertex"            # Vertex AI (or "gemini": the Gemini API)
"gpt-6-luna"        = "responses"         # OpenAI's Responses API
"*"                 = "chat_completions"  # any other: OpenAI-compatible
```

**Where each API goes.** Under `base_url`, Responses requests go to `/responses`, Chat Completions to `/chat/completions`, Gemini's to `/models/{model}:{method}` and Vertex AI's to `/publishers/google/models/{model}:{method}`; Claude's go to `/responses` too, where gateways that pass each request on by its model take them. `[llm.api.paths]` moves any of them; `{model}` is the model's name as it is (a `/` in it stays a `/` in the path), and `{method}` Google's `generateContent` or `streamGenerateContent`:

```toml
[llm.api.paths]
messages = "/anthropic/v1/messages"
vertex   = "/vertex/v1/projects/acme-ai/locations/global/publishers/google/models/{model}:{method}"
```

**The gateway's models.** Blitz asks the gateway for them at `/models` under `base_url`, with the same headers and token, and reads OpenAI's, Anthropic's, Gemini's and Ollama's answers, or a plain list. `models_path` points elsewhere (`"/v1/models"`); `models_path = "none"` says the gateway doesn't list them, and the menus offer `model`, `models` and the models `[llm.api.apis]` names instead. `models = [...]` also picks which of the listed ones the menus show.

The script runs when a session first needs the token, with the service's environment and outside the sandbox, and must finish within 30 seconds. Each session keeps its token in memory; a `401` or `403` runs the script once more and retries, and a second one fails with the message to check the script. No `OPENAI_API_KEY` or `ANTHROPIC_API_KEY` from the environment reaches the gateway. Responses API requests are never stored (`store: false`) or streamed. Requests follow the model: `temperature` and `top_p` aren't sent to models that refuse them (current Claude models, OpenAI's reasoning models), and a setting the gateway refuses for a model is left out of that model's requests from then on. Errors start `[llm.api]` and quote the gateway's reason. To see every request Blitz sends the gateway and what came back, set `gateway = "debug"` under [`[log.levels]`](telemetry.md#more-detail-from-one-part-of-blitz). A project's settings can't set `[llm.api]`.

In the desktop app, pick **Custom gateway** in **Settings › Providers & keys**: the endpoint, a row per header (**Use the token** puts `{{TOKEN_SCRIPT}}` in it) and the script; **The gateway lists its models** and where; **Each model's API**, a row per model with its API, **Every other model**, and **Add the gateway's models** (what it lists, each with the API its name suggests, once the gateway is saved); and, with **Show advanced**, **Each API's path**. Once it's saved, **Models in the menus** picks which of its models the menus show. Header values you've saved aren't shown again; leave one empty to keep it. A header written as `Bearer {{TOKEN_SCRIPT}}` in the file shows with **Use the token** on and keeps its `Bearer `. `blitz doctor` shows the endpoint, the header names, the model's API and path and where the models are listed, and `blitz doctor --online` runs the script and sends a request. `blitz models api` lists the gateway's models.

## Behind a proxy

Blitz uses `HTTPS_PROXY`, `HTTP_PROXY` and `NO_PROXY` for model APIs, MCP servers, web search and `web_fetch` (which still refuses private addresses: it checks where a host resolves before the proxy connects). If a proxy inspects TLS with the company's own certificate authority, trust it:

```toml
[network]
ca_file = "~/certs/company-root.pem"
```

## Checking it works

- `blitz doctor` checks the provider, its credentials and the model; `blitz doctor --online` also sends a request.
- `blitz config keys` shows where each provider's key comes from and how it signs in.
- `blitz models` lists the models each provider you've set up offers, with the price Blitz knows for each.
- In the desktop app, a model that isn't available says why, with **Check again** and **Set up provider** (which opens Providers & keys).
