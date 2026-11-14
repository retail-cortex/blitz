---
title: Configure the desktop app
weight: 10
---

What every workspace shares is in **Settings**: the gear at the top right, or ⌘, (Ctrl+, on Windows and Linux). On macOS it's also **Blitz › Settings…** in the menu bar. What belongs to one workspace (its own settings, agents, skills, jobs, connectors and secrets) is in that workspace's [Studio](studio/_index.md).

Settings has seven entries at its left, each holding its panes as tabs: **Providers** (Providers & keys, Organization), **Personality** (Personality, Voice), **Appearance**, **Resources** (Agents, Skills, Connectors, Browser, Secrets), **Jobs**, **Access** (Workspaces, Permissions) and **System** (Service, Logs, About).

Start with **Providers & keys**: until a provider has a key or a sign-in, no chat, job or eval can run, and the welcome page says so with **Set up a provider**. The rest has sensible defaults you can come back to.

## Show advanced

Settings opens with the few panes most people need: **Providers & keys**, **Organization**, **Personality**, **Voice**, **Appearance**, **Connectors**, **Workspaces** and **About**. **Show advanced**, in Settings' title bar, adds the rest (your own agents and their evals, skills, secrets and jobs, MCP servers under Connectors, browser profiles, permissions, the service and its logs), model and generation settings in each workspace, and the **Studio** menu in the menu bar. Turn it off again to go back to sensible defaults.

![Settings with Show advanced on: Providers, Personality, Appearance, Resources, Access and System, open on Resources › Agents](../../../images/desktop/settings-resources.png)

## Providers & keys

Where Blitz's models come from: the provider, its key or sign-in, and the default model. These are the global settings every workspace uses unless it sets its own.

![Providers & keys: Gemini in use with its key in the keychain, Anthropic's key in the settings file, the others not set up](../../../images/desktop/settings-providers.png)

The list shows every provider at once, each with where it stands: a key **in the keychain** or **found in the environment**, **Signed in**, a Google Cloud or Anthropic Console sign-in **found on this machine**, Ollama **running on this machine**, or **Not set up**. The one Blitz uses says **In use**; **Use** switches to one that's ready, and **Set up** opens one found on this machine on its sign-in.

1. Pick a provider: Gemini, Anthropic, OpenAI, Ollama, or **Custom gateway** for your company's gateway. Its form opens below the list.
2. Paste its API key (**Get a key** opens the provider's page for one). Blitz tries it as you paste and says whether it works and how many models it reaches, before anything is saved. Keys go into the macOS Keychain (the Secret Service on Linux); the settings file only refers to them. A key written in the settings file has **Secure** to move it to the keychain.
3. Or, with **Show advanced** on, choose how it signs in under **Sign in with**: an **API key**, **Google Cloud (ADC)**, or an account (**Google account (OAuth)**, **Anthropic account (OAuth)**), then press **Sign in**.
4. Optionally, set the **Default model**. Empty, Blitz uses the provider's own default.
5. Press **Save and use** (**Save** for the one in use). If the model still isn't available, the form says why.

**Your company's gateway.** Pick **Custom gateway** and fill in the **Endpoint**, a row per header (**Use the token** puts the script's token in that header) and the **Token script**, which runs on the service's machine at the start of each session and prints the token. Header values you've saved aren't shown again; leave one empty to keep it.

![The API provider: endpoint, two headers, one using the token, and a shell token script](../../../images/desktop/settings-providers-api.png)

Bedrock, Azure and Claude on Vertex AI aren't in the form yet: set them in the settings file. [Providers](../../guide/providers.md) covers every provider, its key or sign-in, and checking it works.

## A workspace's own settings

Each workspace can have its own provider, key, model, permissions and more, kept in `~/.blitz` and never in the folder itself. They're in the workspace's **Studio › Settings**: open them from **Workspaces** in the menu bar (the pencil beside the workspace's name), the status bar's agent menu, **Settings › Access › Workspaces** (**Open in Studio**), or the Studio menu.

![recipes' Studio › Settings: Workspace, Agent and model, Behaviour, then the advanced ones](../../../images/desktop/settings-workspace.png)

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

## Resources

What every workspace shares: the same tabs as a workspace's **Studio › Resources** (which holds that workspace's own and never changes these), plus **Browser**, which only Settings has. **Connectors** shows without **Show advanced**; the rest with it.

| Tab | What it holds |
|---|---|
| **Agents** | Your own agents, in `~/.blitz/agents`, there in every workspace. Choose one to edit it, or **+** for a new one. Each row says how many evals cover it: click that to open them, or **Draft evals** when there are none. **Evals**, Agents' second tab, holds the evals of your own agents and skills, in `~/.blitz/evals`; they run in a folder of their own, `~/.blitz/home`, as the global jobs do, so they never touch a project. [Agents](../../guide/agents.md), [Evals](../../guide/evals.md) |
| **Skills** | Skill registries (such as your organization's Castor registry) to install skills from, and the built-in skills. [Skills](../../guide/skills.md) |
| **Connectors** | Your own database connections, there in every workspace; a workspace can turn any of them off. **+** starts with the kind, takes a pasted connection URL, and tests before it saves (`/connect` and **Connect a database…** in the chat box's database menu open it). With **Show advanced** on, also your **MCP servers**: servers that give the agent more tools, and the resources they offer, each switched on or off, **+** for a new one. [Connectors](studio/connectors.md), [MCP servers](../../guide/mcp.md) |
| **Browser** | Browser profiles: the sign-ins the agents browse with. Make one, **Sign in…** to the sites it's for (a window of the app's; **Sign in with Chrome…** for jobs and the command line), list the sites it may browse without asking, and choose it for a workspace in its **Studio › Settings** (**Browser**). **Sign out of everything** clears it; **Delete** removes it. [The browser](../../guide/search.md#the-browser) |
| **Secrets** | Tokens and passwords every workspace can use as `{secrets:global:name}`, kept in the keychain and put in only as Blitz calls the model. |

![Settings › Resources › Connectors › MCP servers: three servers, two on](../../../images/desktop/settings-mcp.png)

## Jobs

**Limits for every job** (model calls, cost, time, runs at once, and what jobs may be given), your global jobs, and every workspace's jobs, open or not. [Jobs](../../guide/workers.md)

![Settings › Jobs: the limits for every job, and the global jobs](../../../images/desktop/settings-jobs.png)

## Access

- **Workspaces** lists the workspaces you've opened. **Open in Studio** shows one with its Studio open on its own settings (above); a closed one can be **Forget**-ten.
- **Permissions** holds the rules for every workspace: **allow** runs matching actions without asking, **ask** always asks, **deny** never runs them; deny wins over ask, and ask over allow. **Try it on** checks a command or path against them. **Allow common read-only commands** lets `ls`, `cat`, `grep`, `git status` and the like run without asking. [Safety](../../guide/safety.md) has the rules' syntax.

![Settings › Access › Permissions: a deny rule, an allow rule and Try it on](../../../images/desktop/settings-permissions.png)

## System

Your organization's sign-in is under **Providers › Organization**, and **Voice** under **Personality**.

- **Service**: the Blitz service holds your workspaces and runs jobs, also while the window is closed. **Restart** or **Stop** it, start it at login, and **Show in the system tray**. [The service](../service.md) explains what it does.
- **Logs**: the service's log by day, filtered by level (**All**, **Info**, **Warnings**, **Errors**) and text. The first place to look when something fails.
- **About**: the version, the license and the third-party notices.

![Settings › System › Service: running, starts at login, and the tray](../../../images/desktop/settings-service.png)

## The settings file

Everything the forms set is in `~/.blitz/.env.toml`. With **Show advanced** on, **Show the settings file** (the `</>` button in Settings' title bar) opens it in an editor that checks it before it's saved: completion for tables, settings and values, every setting described on hover, and **Validate**, which marks problems at their lines. Keep API keys out of it: set them in **Providers & keys**. [Configuration](../../guide/configuration.md) lists where every setting lives.
