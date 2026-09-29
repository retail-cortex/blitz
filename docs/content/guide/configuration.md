---
title: Configuration
weight: 10
---

Spec: [configuration](../about/specs/spec_config_002.md).

## Where settings live

Blitz reads one TOML file, `~/.blitz/.env.toml`, or the directory in `$MODENV_PREFIX`, or the one given with `--config DIR`. `blitz config init` writes a commented copy with every setting at its default; `blitz config path` says which file is in use and `blitz config show` prints the settings in effect.

A `.env.toml` inside a project is **ignored** unless you pass `--config .`: a cloned repository must not be able to redirect your API key or turn off approvals.

Each workspace can have its own settings, kept in `~/.blitz/workspaces/<name>-<hash>/.env.toml` (never in the project) and laid over the global ones, so a project can use its own key, provider or model. The desktop app edits both in **Settings**: forms for providers and keys, and the settings file itself.

## Providers and models

```toml
[llm]
provider = "gemini"                  # gemini (default), anthropic, openai or ollama

[llm.anthropic]
model = "claude-opus-5"
```

The model comes from `llm.<provider>.model` unless `blitz.default_model` or `--model` overrides it. `--model anthropic/claude-sonnet-5` (or `/model` in a session) can switch provider.

- **Gemini** takes a key, or signs in with Google Cloud (below).
- **Anthropic** streams, caches the system prompt, keeps thinking across tool calls, and falls back on server-side refusals (`llm.anthropic.fallbacks = "default"`, or `"off"`). It takes a key, signs in with an Anthropic account, or runs on Vertex AI with Google Cloud (below). Without `api_key` it also uses `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, or an `ant auth login` profile.
- **OpenAI** covers every OpenAI-compatible API (OpenRouter, vLLM, …) through `base_url`.
- **Ollama** runs local models.

[Models](models.md) covers fallbacks, per-agent models and generation settings.

## API keys

```bash
blitz config set-key gemini          # reads the key from stdin, stores it in the OS keychain
blitz config keys                    # where each provider's key comes from
blitz config secure-key anthropic    # move a key written in the file into the keychain
blitz config remove-key openai
```

Keys go to the macOS Keychain, the Secret Service on Linux (GNOME Keyring, KWallet), or, where neither is available, an owner-only `~/.blitz/secrets.toml`. The settings file only refers to the key (`api_key = "keychain:…"`). With `--workspace` (`-w`) these commands work on the current workspace's own settings.

A key in the environment (`GEMINI_API_KEY`, `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`) works too, but the service started at login doesn't see your shell's environment; use the keychain for it.

## Signing in without a key

Gemini and Claude can use an account instead of an API key: the desktop app's **Sign in with**, `blitz config set-auth`, or `auth` in the settings. The sign-in belongs to the machine the Blitz service runs on, so sign in there.

**Gemini with Google Cloud (Application Default Credentials).** Gemini runs on Vertex AI in your Google Cloud project, billed there.

```bash
gcloud auth application-default login
blitz config set-auth gemini adc --project my-project      # --location, else global
```

```toml
[llm.gemini]
auth = "adc"
project_id = "my-project"    # or GOOGLE_CLOUD_PROJECT
location = "global"          # or GOOGLE_CLOUD_LOCATION; global when unset
```

The credentials are Google's usual ones: `GOOGLE_APPLICATION_CREDENTIALS` (a service account's key file), gcloud's application-default login, or a Google Cloud machine's own. The project needs the Vertex AI API enabled.

**Claude on Vertex AI (Google Cloud ADC).** Claude runs in your Google Cloud project, billed there, with the same sign-in as Gemini's. Enable the Claude models you use in Vertex AI's Model Garden first; model names stay the same (`claude-opus-5`).

```bash
gcloud auth application-default login
blitz config set-auth anthropic adc --project my-project --location global
```

```toml
[llm.anthropic]
auth = "adc"
project_id = "my-project"    # or GOOGLE_CLOUD_PROJECT
location = "global"          # or GOOGLE_CLOUD_LOCATION, us-east5, europe-west1, …
```

On Vertex AI, Claude has no server-side refusal fallback: a refused request ends as refused (`fallbacks` doesn't apply).

**Claude with an Anthropic account (OAuth).** Claude uses an Anthropic Console sign-in from the `ant` command line tool, billed to the organization you pick when signing in. This is the Claude API's sign-in, not a Claude.ai subscription.

```bash
ant auth login                                              # --profile work for more than one
blitz config set-auth anthropic oauth                       # --profile work, else ant's active profile
```

```toml
[llm.anthropic]
auth = "oauth"
profile = "work"             # optional: else ANTHROPIC_PROFILE, ant's active profile, or "default"
```

Signing in after choosing the method is fine: Blitz tries the model again when you come back to the window, press **Check again** on the "model isn't available" note, or send a prompt. After signing in again as someone else, save the provider settings (or restart the service) so Blitz picks up the new credentials.

With `oauth`, an `ANTHROPIC_API_KEY` or `api_key` is ignored, so a leftover key can't take the profile's place. `blitz config set-auth <provider> api_key` goes back to the key; `blitz config keys` shows how each provider signs in, and `blitz doctor` checks it.

## Editing in place

Blitz changes its settings file a line at a time, keeping your comments: `/permissions … --save`, `/pin_model`, `/model_settings`, `/locale` and the desktop app's forms all write that way. `/pin_model` and `/unpin` write the workspace's settings; `/model_settings` and `/locale` the global ones.

## Files under ~/.blitz

| Path | |
|---|---|
| `.env.toml` | Settings |
| `workspaces/` | Each workspace's own settings |
| `sessions/`, `checkpoints/`, `images/` | Saved sessions, undo checkpoints, attached images |
| `audit/`, `logs/` | The audit log and the diagnostic log |
| `approvals.json` | Approvals answered "always" |
| `agents/`, `skills/`, `commands/`, `rules/`, `BLITZ.md` | Your own agents, skills, commands, rules and instructions |
| `envs/`, `uc_tools/` | Skill scripts' Python environments; tools built by `universal_constructor` |
| `run/blitz.sock` | The service's socket |
| `desktop.json` | The desktop app's own settings |

Everything that could hold a secret or your work is owner-only.
