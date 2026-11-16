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

Blitz is then connected to that organization from the start, and Settings › Providers › Organization and `blitz enterprise status` say it's set by device management: it can't be changed or disconnected there. Each person still signs in as themselves. To try a file before sending it out, name it in `BLITZ_MANAGED_SETTINGS`.

The desktop app, the CLI and the tray share one sign-in: the Blitz service on your computer keeps it. Your sign-in lasts as long as your organization allows (often a working day); Blitz renews it on its own until then, and asks you to sign in again after.

## What changes while you're connected

- **The models are your organization's.** Settings › Providers shows your organization as the one provider: choose the default model there from the ones it offers you, or leave it to your organization's default. Under the choice, Blitz says what that model can do (tools, images, thinking, how much it reads at once) and its price; `blitz models` lists the same. Workspaces and agents can name one of them too (`enterprise/<model>`). A model your settings name that it doesn't offer gives way to its default. Your own keys and providers are set aside, not removed: disconnect and they're back. Those your organization's policy lets you keep (Ollama on your computer, say) stay below its models, under **Your own providers**, with the models it allows of each.
- **Its limits apply.** Your organization may cap what you spend, a day or a month, and how fast you can send requests.
- **What you send goes to it.** Your requests to models go through your organization's service, which may keep a record of them under its policies. In one of your organization's workspaces, each request says which, so its guardrails and limits apply. Ask your admins what they keep.
- **Signed out, the models wait.** A workspace tells you to sign in rather than falling back to your own keys.

A workspace's settings can't connect Blitz to an organization, or change which one: only you can, in your own settings (`[enterprise] url`).

## Your organization's agents, rules and policy

Your organization keeps a **library** of agents, skills, workflows, rules and guardrails, each versioned and tested by its evals before it's published. Its admins decide, in its admin console, which of them apply to whom (everyone, some groups, or one person) and how: **required** (you have it, locked) or **available** (yours to switch on). Blitz gets yours with your policy (below) when you sign in, follows changes as your admins make them, and keeps them for offline work until they expire; past that, the organization's models wait until Blitz reaches it again.

A required agent, skill, rule or guardrail is locked: a file of yours or a workspace's of the same name doesn't replace it. An available one you switch on comes before your own, so a file of yours of that name wins. Each of the organization's agents runs the skill versions it was published with, beside your own. Your choices are kept by your organization, so they follow you to every computer. Its connections and MCP servers still come with its catalog, if it has one.

Its required workflows that run before a commit or push install that [git hook](workflows.md#running-before-a-commit-or-push) in each repository you open, and its workflows can call its [API connectors](workflows.md#calling-services).

When your organization's service doesn't answer (it's down, or you're offline), Blitz works on without it, signed in or not: your workspaces open and their files, the editor, Git and the terminal work as ever, and the agent waits for the organization's models, saying the service can't be reached. Blitz gives up on a service that doesn't answer within seconds, then stops asking for half a minute at a time, so nothing waits on it.

**Settings › Organization** shows your organization's library, and Settings › Agents and Skills, and Studio's Rules and Guardrails, end with **From your organization**: each required one locked and saying who decided it (your organization, a group of yours by name, or an admin for you alone), each available one with a switch. From the terminal, `blitz enterprise items` lists them and `blitz enterprise items enable skill/<name>` (or `disable`) switches one; `blitz enterprise policy` lists your policy.

Its policy may also switch off Blitz's own built-in agents, skills or guardrails (never the agent your sessions start with), turn off tools for every agent, your own `!` commands, your own MCP servers or connections (or the kinds of connection it doesn't list, judged by what a file connects to, not by its `# kind:` comment), or your own model providers. Where it turns off your own MCP servers, that includes those a trusted project or a plugin brings. It may also turn off your own [plugins](extending.md#plugins), whether installed, from a marketplace or a folder: then none of their skills, agents, commands, hooks or MCP servers loads, a workspace says "your organization turns off your own plugins", and so does `blitz plugin list`. Until Blitz has your organization's policy (before your first sign-in), or if its copy is damaged, nothing of your own (plugins included) is allowed and the organization's models wait; Blitz says so when it starts. Studio's Connectors marks the organization's connections, and says why one of yours isn't used. Where it allows your own providers, name one with its prefix (`anthropic/claude-…`) to use it; bare model names are your organization's.

### Your policy

Your organization's admins set its policy in its admin console: for everyone, for groups, and for one person. Blitz gets **yours**, the result for you, when you sign in and again whenever an admin changes it. **Settings › Providers › Organization › Your policy** lists every setting, its value for you, and who decided it: the default, your organization, your group (by name), or an admin for you alone, and whether it's locked. You can't change it there; ask your admins.

- **Your own providers, one by one.** The policy may allow some of your own providers and not others, and only some of their models: Ollama with `llama3*`, for example. Settings › Providers then offers just those; the model menu and `blitz models` list only the models it allows; and a model it doesn't allow is refused, saying which ones it does. With no model chosen, Ollama uses the first one you've pulled that it allows.
- **Every refusal says who decided.** "Your organization doesn't allow connections of your own (the policy for Data decides it…)" tells you which level to ask about.
- **Its rules and guardrails apply to you** before a workspace's and your own, as a locked catalog's do.

A service from before this policy keeps sending its catalog's policy, which applies to everyone the same way.

## Yours, with your organization

Your own agents, skills, workflows, rules and guardrails (in `~/.blitz`) sync with your organization's service, so they follow you to every computer you sign in on: Blitz sends what you change and brings in what you changed elsewhere each time it fetches your policy, and `blitz enterprise items sync` (or **Sync now** in Settings › Organization) does it at once. Removed on one computer, it's removed on the others. Changed on two before they synced, the one you're at wins and the other's is kept in `~/.blitz/conflicts` for you to compare. Your organization's admins can read them (never change them), and its policy says which kinds it keeps; a skill with a file that isn't text, or a linked skill folder, stays on this computer.

When your organization takes them, **Share** one with everyone (Settings › Organization, or `blitz enterprise items share skill/<name>`). Its evaluator judges it, with the checks and model your admins chose: accepted, it's published in the library as yours for the groups they chose; rejected, you're told why, in Blitz (`blitz enterprise items submissions`) and by your notices. Fix it and share it again. A kind with no evaluator takes no submissions.

Your workflow and eval runs are reported to your organization too, so its admins see what ran and what it cost.

## Your organization's databases

Your organization's admins can connect its databases once, in its console, for the people its policy allows: its service holds the connection and its passwords, so you never need credentials of your own. The ones you may use show up beside your own connections:

- **In Studio › Resources › Connectors**, marked as your organization's, run by its service. You can't open or switch them off: your admins change them.
- **In the Data menu and with `@db:`**, like any database: ask for what you need, or write SQL. `blitz data` lists them too, marked "your organization's", and `blitz data query <name> "<SQL>"` runs one.
- **For the agent**, as `org__<name>_execute_sql` and `org__<name>_list_tables`.

Every query runs through your organization's service, as you, and its policy decides what you may do with each: which tables, whether you may change data, and how many rows come back. A query it doesn't allow is refused, saying who decided. Your organization keeps a log of the queries, with who asked. One of your own connections with the same name wins it.

Your organization can share **MCP servers** the same way: its service holds their tokens, and their tools reach your agent as `org__<name>_<tool>`, each call made through the service as you and logged.

Some shared BigQuery connections **query as you**, with your own Google account, so you see exactly what your own access allows. Allow it once: Settings › Providers › Organization › **Data as you** › **Allow with Google** (or `blitz enterprise consent`), and revoke it there whenever you like.

## Your organization's look and terms

If your organization has its own name, logo and colours, Blitz wears them: in the window's title, the top bar, the welcome page, its menus, dialogs and notifications, the tray, the terminal's banner and help, and About, which ends with "Built on Blitz". Where Blitz speaks of itself as a program (its releases, its license, reinstalling it), it keeps its own name. They're kept on your computer, so Blitz looks the same offline, and they follow changes your admins make within about ten minutes.

If your organization has terms, Blitz shows them before anything else when you sign in, and again when they change. **Accept** records it with your organization; **Decline** signs you out of it. In the terminal, `blitz enterprise login` asks `[y/N]` after signing in. A login run from a script, with no terminal to ask in, asks nothing and stays signed in, but your organization's features wait until the terms are accepted: run `blitz enterprise terms` in a terminal, or open the Blitz app, which asks.

## Your usage and limits

Settings › Providers & keys shows **Your usage** of your organization's models, today and this month, by model; each budget and token quota that applies to you, with what's left and when it starts again; and the models' rate limits. If your organization keeps full logs of what's sent to its models, and yours are among them, it says so. When you reach a limit, Blitz says which one and when it resets, rather than calling it a model failure. From the terminal: `blitz enterprise usage`.

## Job notices

When a job of yours runs on your organization's service, Settings › Providers › Organization's **Job notices** says how you hear of it: by email, by a webhook of yours (a Slack or Teams incoming webhook shows the notice as it is), or both; for a failure and a run waiting for your answer, or **Every run**. From the terminal: `blitz enterprise notices`, with `--email`, `--webhook <address>` and `--every-run` to turn them on, `--email=false`, `--webhook ""` and `--every-run=false` to turn them off. The status bar, Studio › Jobs and the tray show runs waiting for your answer too.

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

Settings › Providers › Organization › **Disconnect**, or `blitz enterprise disconnect`: you're signed out, and Blitz uses your own providers again. `blitz enterprise logout` signs out but stays connected.

## When something's wrong

`blitz doctor` says whether you're signed in to your organization; with `--online` it also says whether its service answers.

| What you see | What to do |
|---|---|
| "Sign in to your organization … to use its models" | Sign in: Settings › Providers › Organization, or `blitz enterprise login` |
| "… isn't answering" | Check your network or VPN; **Retry** |
| "… offers you no models" | Ask your admins for access to a model |
| "Accept the organization's terms" | Your organization changed its terms: accept them where it asks, or in a terminal with `blitz enterprise terms` |
