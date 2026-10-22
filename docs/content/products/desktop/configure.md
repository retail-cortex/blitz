---
title: Configure the desktop app
weight: 10
---

Everything the desktop app can change is in **Settings**: the gear at the top right, or ⌘, (Ctrl+, on Windows and Linux). On macOS it's also **Blitz › Settings…** in the menu bar.

Start with **Providers & keys**: until a provider has a key or a sign-in, no chat, job or eval can run. The rest has sensible defaults you can come back to.

## Show advanced

Settings opens with the few panes most people need: **Providers & keys**, **Appearance**, **Workspaces** and **About**. **Show advanced**, in Settings' title bar, adds the rest (custom agents and jobs, MCP servers, connectors, secrets, permissions, the service and its logs), model and generation settings in each workspace, and the **Studio** menu in the menu bar. Turn it off again to go back to sensible defaults.

![Settings with Show advanced on: Getting started, Global setup, Workspaces and access, and System](../../../images/desktop/settings-providers-signin.png)

## Providers & keys

Where Blitz's models come from: the provider, its key or sign-in, and the default model. These are the global settings every workspace uses unless it sets its own.

![Providers & keys with Gemini chosen and its key in the keychain](../../../images/desktop/settings-providers.png)

1. Choose the **Provider**: Gemini, Anthropic, OpenAI, Ollama, or API for your company's gateway.
2. Paste its API key. It goes into the macOS Keychain (the Secret Service on Linux); the settings file only refers to it. The box under the key says where the key comes from now: **Keychain**, **Environment**, **Plain text** in the settings file (with **Move to keychain**), or **Not set**.
3. Or, with **Show advanced** on, choose how it signs in under **Sign in with**: an **API key**, **Google Cloud (ADC)**, or an account (**Google account (OAuth)**, **Anthropic account (OAuth)**), then press **Sign in**.
4. Optionally, set the **Default model**. Empty, Blitz uses the provider's own default.
5. Press **Save**. If the model still isn't available, the form says why.

**Your company's gateway.** Choose **API** and fill in the **Endpoint**, a row per header (**Use the token** puts the script's token in that header) and the **Token script**, which runs on the service's machine at the start of each session and prints the token. Header values you've saved aren't shown again; leave one empty to keep it.

![The API provider: endpoint, two headers, one using the token, and a shell token script](../../../images/desktop/settings-providers-api.png)

Bedrock, Azure and Claude on Vertex AI aren't in the form yet: set them in the settings file. [Providers](../../guide/providers.md) covers every provider, its key or sign-in, and checking it works.

## A workspace's own settings

Each workspace can have its own provider, key, model, permissions and more, kept in `~/.blitz` and never in the folder itself. Open them from **Workspaces** in the menu bar (the gear beside the workspace's name), or from **Settings › Workspaces**.

![recipes' own settings: Workspace, Agent and model, Behaviour, then the advanced ones](../../../images/desktop/settings-workspace.png)

- **Agent and model**: the agent the workspace starts with, and its model.
- **Behaviour**: the permission mode (such as **Auto**) and the agency level, how much the agent takes on by itself. [Safety](../../guide/safety.md#permission-modes) describes the modes; [Agents](../../guide/agents.md) the agency levels.
- With **Show advanced** on: **Thinking**, **Generation** (temperature and the rest, for this model), **Search**, **API keys** and the provider for this workspace only, **Permission rules**, **Standing approvals**, **Project settings** (what the project's own `.blitz/settings.toml` asks for, and whether you've trusted it), the workspace's **Settings file**, **Language servers** and **Context**.

A row marked **This workspace** overrides the global setting; **This model** applies to that model wherever it's used. [Configuration](../../guide/configuration.md) explains how the global, workspace and project settings combine.

## Appearance

How the window looks and behaves, for every workspace.

![Appearance: theme, colour themes, language, notifications, documents, chat input, density and transparency](../../../images/desktop/settings-appearance.png)

- **Theme**: **System**, **Light** or **Dark**, with a **Light theme** and a **Dark theme** each (Blitz's own, One Light or One Dark Pro).
- **Interface language**: the window's language. The language the model answers in is set per workspace.
- **Notifications** when the agent needs you or finishes a longer turn while you're elsewhere, and how **Workflow tasks** that wait for you say so.
- **Documents**: **Document styles…** for how Markdown previews look.
- **Chat input** pinned to the bottom or with the conversation, **Density**, **Transparency** (glass surfaces), **Conversation width** and **Document width**.
- With **Show advanced** on: **Show thinking**, and **Continue after background tasks**.

## Global setup

With **Show advanced** on, these panes hold what every workspace shares. A workspace's own agents, skills, jobs, connectors and secrets are in [Studio](studio/_index.md), which never changes the global ones.

| Pane | What it holds |
|---|---|
| **Agents** | Your own agents, in `~/.blitz/agents`, there in every workspace. Choose one to edit it, or **+** for a new one. [Agents](../../guide/agents.md) |
| **Skills** | Skill registries (such as your organization's Castor registry) to install skills from, and the built-in skills. [Skills](../../guide/skills.md) |
| **Jobs** | **Limits for every job** (model calls, cost, time, runs at once, and what jobs may be given), your global jobs, and every workspace's jobs, open or not. [Jobs](../../guide/workers.md) |
| **MCP servers** | Servers that give the agent more tools, each switched on or off. **Add server** for a new one. [MCP servers](../../guide/mcp.md) |
| **Connectors** | Your own database connections, there in every workspace; a workspace can turn any of them off. [Connectors](studio/connectors.md) |
| **Secrets** | Tokens and passwords every workspace can use as `{secrets:global:name}`, kept in the keychain and put in only as Blitz calls the model. |

![Settings › Jobs: the limits for every job, and the global jobs](../../../images/desktop/settings-jobs.png)

![Settings › MCP servers: three servers, two on](../../../images/desktop/settings-mcp.png)

## Workspaces and access

- **Workspaces** lists the workspaces you've opened. Choose one for its own settings (above).
- **Permissions** holds the rules for every workspace: **allow** runs matching actions without asking, **ask** always asks, **deny** never runs them; deny wins over ask, and ask over allow. **Try it on** checks a command or path against them. **Allow common read-only commands** lets `ls`, `cat`, `grep`, `git status` and the like run without asking. [Safety](../../guide/safety.md) has the rules' syntax.

![Settings › Permissions: a deny rule, an allow rule and Try it on](../../../images/desktop/settings-permissions.png)

## System

- **Service**: the Blitz service holds your workspaces and runs jobs, also while the window is closed. **Restart** or **Stop** it, start it at login, and **Show in the system tray**. [The service](../service.md) explains what it does.
- **Logs**: the service's log by day, filtered by level (**All**, **Info**, **Warnings**, **Errors**) and text. The first place to look when something fails.
- **About**: the version, the license and the third-party notices.

![Settings › Service: running, starts at login, and the tray](../../../images/desktop/settings-service.png)

## The settings file

Everything the forms set is in `~/.blitz/.env.toml`. With **Show advanced** on, **Show the settings file** (the `</>` button in Settings' title bar) opens it in an editor that checks it before it's saved: completion for tables, settings and values, every setting described on hover, and **Validate**, which marks problems at their lines. Keep API keys out of it: set them in **Providers & keys**. [Configuration](../../guide/configuration.md) lists where every setting lives.
