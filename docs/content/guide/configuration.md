---
title: Configuration
weight: 10
---

Spec: [configuration](../about/specs/spec_config_002.md).

## Where settings live

Blitz reads one TOML file, `~/.blitz/.env.toml`, or the directory in `$MODENV_PREFIX`, or the one given with `--config DIR`. `blitz config init` writes a commented copy with every setting at its default; `blitz config path` says which file is in use and `blitz config show` prints the settings in effect.

A `.env.toml` inside a project is **ignored** unless you pass `--config .`: a cloned repository must not be able to redirect your API key or turn off approvals.

Each workspace can have its own settings, kept in `~/.blitz/workspaces/<name>-<hash>/.env.toml` (never in the folder itself) and laid over the global ones, so a workspace can use its own key, provider or model. The desktop app edits both in **Settings**: forms for providers and keys, and the settings file itself.

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

- **At once**, because they only tighten: deny and ask rules, blocked paths, lower limits (`tools.max_parallel`, `tools.shell_timeout_seconds`, `tools.max_file_size_bytes`), a higher `skills.policy.min_hitl_tier`, `skills.policy.deny_tools`, and lower job limits. The project's agents (`.agents/agents`) and skills (`./skills`, `.agents/skills`) load as prompt text too.
- **After you trust them**, because they run code or loosen a policy: hooks, MCP servers (always sandboxed, never auto-approved), allow rules, `sandbox.shell_writable_paths` inside the workspace, `blitz.default_model` and `[agent_models]` (only providers you've set up), more `workers.policy.allow` kinds, and the scripts of the project's skills.
- **Never**: API keys, base URLs and other endpoints, telemetry, where logs and audit files go, bypass and auto-approval, turning the sandbox off or opening the network. Blitz ignores them and says which file and key.

When a project's settings (`.blitz/settings.toml`) need trust, the REPL lists what they would do and asks **[t]rust, [d]on't trust, [s]how the files**; the desktop app shows the same in a dialog. Your answer is kept in `~/.blitz/trust.json` for that exact content: if the settings change, or a script their hooks or MCP servers run, or a project skill's scripts, Blitz asks again. Until then those settings stay off, and the rest applies.

- `/trust` (REPL), `blitz trust [dir]` and Settings › Workspaces › **Project settings** show what applies, what waits and what was ignored, and change the decision; `blitz trust --revoke` forgets it.
- With nobody to ask (`blitz exec`, a pipe), settings that need trust stay off; `--trust-project` trusts them for that run only.
- `blitz doctor` lists the project's settings and anything ignored.
- `blitz.trust_workspace` and `--trust-workspace` are deprecated since v0.2.0: the project's agents and skills now load without them, and they trust the project's settings for each run without recording it.

## Providers and keys

Which provider Blitz's models come from, its API key or sign-in, and the provider each workspace uses: see [Providers](providers.md). [Models](models.md) covers fallbacks, per-agent models and generation settings.

## Editing in place

Blitz changes its settings file a line at a time, keeping your comments: `/permissions … --save`, `/pin_model`, `/model_settings`, `/locale` and the desktop app's forms all write that way. `/pin_model` and `/unpin` write the workspace's settings; `/model_settings` and `/locale` the global ones.

## Files under ~/.blitz

| Path | |
|---|---|
| `.env.toml` | Settings |
| `workspaces/` | Each workspace's own settings |
| `sessions/`, `checkpoints/`, `images/` | Saved sessions, undo checkpoints, attached images and PDFs |
| `audit/`, `logs/` | The audit log and the diagnostic log |
| `approvals.json` | Approvals answered "always" |
| `agents/`, `skills/`, `commands/`, `rules/`, `BLITZ.md` | Your own agents, skills, commands, rules and instructions |
| `envs/`, `uc_tools/` | Skill scripts' Python environments; tools built by `universal_constructor` |
| `run/blitz.sock` | The service's socket |
| `desktop.json` | The desktop app's own settings |

Everything that could hold a secret or your work is owner-only.
