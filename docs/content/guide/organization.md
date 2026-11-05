---
title: Your organization
weight: 6
---

Spec: [Blitz for organizations](../about/specs/spec_053_enterprise.md) (EN-10 to EN-22).

If your company runs Blitz Enterprise, connect Blitz to it: you sign in with your work account, and Blitz uses the models your organization offers you, under its limits, instead of keys of your own. Blitz itself stays the same, free and open source; the service is your organization's.

## Connect and sign in

**In the desktop app:** Settings › **Organization**. Enter the address your admins gave you, such as `https://blitz.acme.com`, and **Connect**. Then **Sign in**: your browser opens your company's sign-in page, and Blitz is signed in when you finish there.

**From the terminal:**

```sh
blitz enterprise connect https://blitz.acme.com
blitz enterprise login        # shows an address and a code to enter in any browser
blitz enterprise status       # who's signed in, and whether the service answers
```

`blitz enterprise login --browser` signs in in a browser on this computer instead of with a code.

### Set by your IT team

A company's device management (Jamf, Intune and the like) can connect every computer for you, with a machine-wide settings file:

| System | File |
|---|---|
| macOS | `/Library/Application Support/Blitz/managed.toml` |
| Windows | `%ProgramData%\Blitz\managed.toml` |
| Linux | `/etc/blitz/managed.toml` |

```toml
[enterprise]
url = "https://blitz.acme.com"
workspaces_dir = "/Users/Shared/Acme"   # optional: where the organization's workspaces are cloned
```

Blitz is then connected to that organization from the start, and Settings › Organization and `blitz enterprise status` say it's set by device management: it can't be changed or disconnected there. Each person still signs in as themselves. To try a file before sending it out, name it in `BLITZ_MANAGED_SETTINGS`.

The desktop app, the CLI and the tray share one sign-in: the Blitz service on your computer keeps it. Your sign-in lasts as long as your organization allows (often a working day); Blitz renews it on its own until then, and asks you to sign in again after.

## What changes while you're connected

- **The models are your organization's.** Settings › Providers shows your organization as the one provider: choose the default model there from the ones it offers you, or leave it to your organization's default. Under the choice, Blitz says what that model can do (tools, images, thinking, how much it reads at once) and its price; `blitz models` lists the same. Workspaces and agents can name one of them too (`enterprise/<model>`). A model your settings name that it doesn't offer gives way to its default. Your own keys and providers are set aside, not removed: disconnect and they're back.
- **Its limits apply.** Your organization may cap what you spend, a day or a month, and how fast you can send requests.
- **What you send goes to it.** Your requests to models go through your organization's service, which may keep a record of them under its policies. In one of your organization's workspaces, each request says which, so its guardrails and limits apply. Ask your admins what they keep.
- **Signed out, the models wait.** A workspace tells you to sign in rather than falling back to your own keys.

A workspace's settings can't connect Blitz to an organization, or change which one: only you can, in your own settings (`[enterprise] url`).

## Your organization's agents, rules and policy

If your organization publishes a catalog, Blitz works under it: its agents, skills, commands, rules, guardrails, MCP servers and connections join yours. Each is either a **default**, which your own or a workspace's of the same name replaces, or **locked**, which applies over them. A plugin's agent can't take a locked agent's name either: it's reported and left out. Blitz fetches it when you sign in, follows changes as your admins publish them, and keeps it for offline work until it expires; past that, the organization's models wait until Blitz reaches it again.

When your organization's service doesn't answer (it's down, or you're offline), Blitz works on without it, signed in or not: your workspaces open and their files, the editor, Git and the terminal work as ever, and the agent waits for the organization's models, saying the service can't be reached. Blitz gives up on a service that doesn't answer within seconds, then stops asking for half a minute at a time, so nothing waits on it.

Studio's Agents, Skills, Rules and Guardrails lists end with **From your organization**: what the catalog gives this workspace, each marked locked or default and with the groups it's for. A locked one is changed by your organization's Admins, through a review. A default one has **Copy to this workspace**, which puts it in `.agents` for you to change; the workspace's copy then takes its place. From the terminal, `blitz enterprise catalog` lists them and `blitz enterprise catalog copy rule/house-style` copies one.

Its policy may also switch off Blitz's own built-in agents, skills or guardrails (never the agent your sessions start with), turn off tools for every agent, your own `!` commands, your own MCP servers or connections (or the kinds of connection it doesn't list, judged by what a file connects to, not by its `# kind:` comment), or your own model providers. Where it turns off your own MCP servers, that includes those a trusted project or a plugin brings. It may also turn off your own [plugins](extending.md#plugins), whether installed, from a marketplace or a folder: then none of their skills, agents, commands, hooks or MCP servers loads, a workspace says "your organization turns off your own plugins", and so does `blitz plugin list`. Until Blitz has your organization's policy (before your first sign-in), or if its copy is damaged, nothing of your own (plugins included) is allowed and the organization's models wait; Blitz says so when it starts. Studio's Connectors marks the organization's connections, and says why one of yours isn't used. Where it allows your own providers, name one with its prefix (`anthropic/claude-…`) to use it; bare model names are your organization's.

## Your organization's look and terms

If your organization has its own name, logo and colours, Blitz wears them: in the window's title, the top bar, the welcome page, its menus, dialogs and notifications, the tray, the terminal's banner and help, and About, which ends with "Built on Blitz". Where Blitz speaks of itself as a program (its releases, its license, reinstalling it), it keeps its own name. They're kept on your computer, so Blitz looks the same offline, and they follow changes your admins make within about ten minutes.

If your organization has terms, Blitz shows them before anything else when you sign in, and again when they change. **Accept** records it with your organization; **Decline** signs you out of it. In the terminal, `blitz enterprise login` asks `[y/N]` after signing in. A login run from a script, with no terminal to ask in, asks nothing and stays signed in, but your organization's features wait until the terms are accepted: run `blitz enterprise terms` in a terminal, or open the Blitz app, which asks.

## Your usage and limits

Settings › Providers & keys shows **Your usage** of your organization's models, today and this month, by model; each budget and token quota that applies to you, with what's left and when it starts again; and the models' rate limits. If your organization keeps full logs of what's sent to its models, and yours are among them, it says so. When you reach a limit, Blitz says which one and when it resets, rather than calling it a model failure. From the terminal: `blitz enterprise usage`.

## Publishing to your organization

If you're one of your organization's Admins, Studio's **Publish to organization…** (the cloud button at the top) sends the workspace's agents, skills, rules, guardrails, commands and connectors to its catalog. Choose each one, say whether it's **Locked** (nobody changes or switches it off) or a **Default** (a workspace or person may), and which groups it's for. They go as one review: an Admin approves it in the admin console's **Catalog › Reviews** (you may approve your own; the audit log says so), and everyone's Blitz gets them with the next version. Anything the catalog would refuse is marked before you send it. From the terminal: `blitz enterprise publish` lists what can go; `blitz enterprise publish agent/writer --lock locked --groups sales` sends it; `blitz enterprise reviews` shows how they stand.

## Job notices

When a job of yours runs on your organization's service, Settings › Organization's **Job notices** says how you hear of it: by email, by a webhook of yours (a Slack or Teams incoming webhook shows the notice as it is), or both; for a failure and a run waiting for your answer, or **Every run**. From the terminal: `blitz enterprise notices`, with `--email`, `--webhook <address>` and `--every-run` to turn them on, `--email=false`, `--webhook ""` and `--every-run=false` to turn them off. The status bar, Studio › Jobs and the tray show runs waiting for your answer too.

## Your organization's workspaces

Signed in, the **Workspaces** menu lists your organization's workspaces you have a role in, under its name. Open one, and Blitz gets it the first time into `~/Blitz/<organization>/<workspace>` (`[enterprise] workspaces_dir` moves it), then opens it from there. If you're only a Viewer of one, it's read-only on your computer: Blitz's file tools, its commands and the editor won't change it, and say why.

**New workspace…** in the same menu makes one, with you its owner. Switch on **Just for me** to make it yours alone: it's named `~you/<name>`, so everyone can have one called `home`. Otherwise it's a team workspace you can invite people to.

To share a folder you already have, open it and choose **Share <folder> with <organization>…**. It stays where it is and is kept in step from then on. The folder, your changes and its history are checked for secrets first: if any are found, nothing is committed or sent, and Blitz tells you which file and line. A folder with more than 50,000 files to check, in its changes and its history together, isn't shared: share a copy without its history. Then your changes are committed. If the folder can't be sent at once (the service is out of reach, say), it's shared all the same and sent as it syncs. A repository's own `origin` (GitHub, say) stays as it was; your organization's is the remote `organization`.

From the terminal: `blitz enterprise workspaces`, `blitz enterprise open <workspace>`, `blitz enterprise create <name>` (`--personal` for one of your own), and `blitz enterprise share [folder]` (`--name`, `--personal`).

A workspace with sync on saves and shares itself: a few seconds after you or an agent stop changing it (never mid-turn), Blitz saves it and sends it to your organization, and brings in others' changes between turns. If you and someone else changed the same file, both versions are kept: theirs under the file's name, yours beside it as `name (conflict, You).ext`. The status bar says how it stands: **Saved**, **Saving…**, **Offline: saved on this computer**, **Signed out: sign in to sync**, **Not saved: the service refused it**, **Not saved: it holds what looks like a secret** (a changed file has what looks like an API key or a password: the menu names the file and line, and nothing is sent until you take it out), or **Both versions kept** with how many files. Blitz's service does this, so it goes on with the window closed. To send your changes now, click the status and choose **Save now**: it saves what's changed, brings in others' changes and sends yours at once, and the menu says why when it can't. From the terminal: `blitz enterprise sync` in the workspace (`blitz enterprise sync status` only says how it stands).

Blitz signs Git in for you in those folders, as you. To use Git's own clients elsewhere with your organization's service:

```sh
git config --global credential.https://blitz.acme.com.helper "!blitz git-credential"
```

## Disconnect

Settings › Organization › **Disconnect**, or `blitz enterprise disconnect`: you're signed out, and Blitz uses your own providers again. `blitz enterprise logout` signs out but stays connected.

## When something's wrong

`blitz doctor` says whether you're signed in to your organization; with `--online` it also says whether its service answers.

| What you see | What to do |
|---|---|
| "Sign in to your organization … to use its models" | Sign in: Settings › Organization, or `blitz enterprise login` |
| "… isn't answering" | Check your network or VPN; **Retry** |
| "… offers you no models" | Ask your admins for access to a model |
| "Accept the organization's terms" | Your organization changed its terms: accept them where it asks, or in a terminal with `blitz enterprise terms` |
