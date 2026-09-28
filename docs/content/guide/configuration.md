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

- **Gemini** takes a key, or Vertex AI credentials (`project_id` and `location`).
- **Anthropic** streams, caches the system prompt, keeps thinking across tool calls, and falls back on server-side refusals (`llm.anthropic.fallbacks = "default"`, or `"off"`). Without `api_key` it uses `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, or an `ant auth login` profile.
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

## Editing in place

Blitz changes its settings file a line at a time, keeping your comments: `/permissions … --save`, `/pin_model`, `/model_settings`, `/locale` and the desktop app's forms all write that way.

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
