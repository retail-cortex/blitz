---
title: Configuration
weight: 10
---

Spec: [configuration](../about/specs/spec_config_002.md).

## Where settings live

Blitz reads one TOML file, `~/.blitz/.env.toml`, or the directory in `$MODENV_PREFIX`, or the one given with `--config DIR`. `blitz config init` writes a commented copy with every setting at its default; `blitz config path` says which file is in use and `blitz config show` prints the settings in effect.

A `.env.toml` inside a project is **ignored** unless you pass `--config .`: a cloned repository must not be able to redirect your API key or turn off approvals.

Each workspace can have its own settings, kept in `~/.blitz/workspaces/<name>-<hash>/.env.toml` (never in the project) and laid over the global ones, so a project can use its own key, provider or model. The desktop app edits both in **Settings**: forms for providers and keys, and the settings file itself.

## Sharing settings with your team

A repository can carry settings for everyone who works in it: `.blitz/settings.toml` (commit it) and `.blitz/settings.local.toml` (your own; Blitz warns if git tracks it). They use the same keys as `.env.toml`, but a project may set only some of them, and your own settings win where they conflict.

```toml
# .blitz/settings.toml
[permissions]
deny = ["shell(rm -rf *)"]           # applies at once
allow = ["shell(make test)"]         # waits for your trust

[[hooks.pre_tool]]
command = "./scripts/lint-hook.sh"   # waits for your trust

[[mcp.servers]]
name = "db"
command = "npx"
args = ["@acme/db-mcp"]
```

- **At once**, because they only tighten: deny and ask rules, blocked paths, lower limits (`tools.max_parallel`, `tools.shell_timeout_seconds`, `tools.max_file_size_bytes`), a higher `skills.policy.min_hitl_tier`, `skills.policy.deny_tools`, and lower worker limits. The project's agents (`./agents`) and skills (`./skills`, `.agents/skills`) load as prompt text too.
- **After you trust them**, because they run code or loosen a policy: hooks, MCP servers (always sandboxed, never auto-approved), allow rules, `sandbox.shell_writable_paths` inside the workspace, `blitz.default_model` and `[agent_models]` (only providers you've set up), more `workers.policy.allow` kinds, and the scripts of the project's skills.
- **Never**: API keys, base URLs and other endpoints, telemetry, where logs and audit files go, bypass and auto-approval, turning the sandbox off or opening the network. Blitz ignores them and says which file and key.

When a workspace's settings need trust, the REPL lists what they would do and asks **[t]rust, [d]on't trust, [s]how the files**; the desktop app shows the same in a dialog. Your answer is kept in `~/.blitz/trust.json` for that exact content: if the settings change, or a script their hooks or MCP servers run, or a project skill's scripts, Blitz asks again. Until then those settings stay off, and the rest applies.

- `/trust` (REPL), `blitz trust [dir]` and Settings › Workspaces › **Project settings** show what applies, what waits and what was ignored, and change the decision; `blitz trust --revoke` forgets it.
- With nobody to ask (`blitz exec`, a pipe), settings that need trust stay off; `--trust-project` trusts them for that run only.
- `blitz doctor` lists the project's settings and anything ignored.
- `blitz.trust_workspace` and `--trust-workspace` are deprecated: the project's agents and skills now load without them, and they trust the project's settings for each run without recording it.

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

The credentials are Google's usual ones: `GOOGLE_APPLICATION_CREDENTIALS` (a service account's key file), gcloud's application-default login, or a Google Cloud machine's own. The project needs the Vertex AI API enabled. The agent's commands get the same credentials, so code that calls Google Cloud works from them too ([secrets](safety.md#secrets)).

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

On Vertex AI, Claude has no server-side refusal fallback, so Blitz retries a refused request itself on the fallback: the model `fallbacks` names, or `claude-opus-4-8` for `"default"`. A conversation that fell back stays on the fallback. `fallbacks = "off"` turns it off.

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
