---
title: "config"
weight: 5
---

Settings: loading, layering and editing `.env.toml`. Source: [`pkg/config`](https://github.com/retail-cortex/blitz/tree/main/pkg/config).

`pkg/config` loads Blitz's settings: one TOML file, `.env.toml`, in `~/.blitz` (or `$MODENV_PREFIX`, or `--config`), with a workspace's own settings from `~/.blitz/workspaces` laid over it, API keys resolved from the OS keychain (`pkg/secrets`), and environment variables applied last. It reads through [modenv](https://github.com/rrmcguinness/modenv). Nothing is read from the workspace itself.

It also edits the file in place, a line at a time, keeping comments, for the settings the front ends change: permission rules, pins, model settings, the locale, keys, and the raw file the desktop app edits.

## API

| Name | |
|---|---|
| `Config` | Every setting, by section: `Blitz`, `LLM`, `Tools`, `Sandbox`, `Permissions`, `MCP`, `Hooks`, `Skills`, `Workers`, `Web`, `Telemetry`, `UI`, `Pricing`, `ModelSettings`, … |
| `Load`, `LoadWorkspace`, `DefaultConfig` | The global settings; a workspace's, layered; the defaults |
| `SetAPIKey`, `SecureAPIKey`, `RemoveAPIKey`, `Describe` | Keys in the keychain, and where each provider's key comes from |
| `SetValue`, `SavePermissionRules`, `SaveAgentModel`, `SaveModelSettings`, `SaveUILocale` | In-place edits |
| `ReadSettingsFile`, `WriteSettingsFile` | The raw file, validated before it's written |
| `DefaultPricing`, `DefaultBlockedPaths`, `DefaultDeniedCommands`, `DefaultScrubEnv` | Built-in defaults |

## Used by

every app and most packages.

Spec: [config](../about/specs/spec_config_002.md).
