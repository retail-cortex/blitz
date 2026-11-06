---
title: Blitz 0.9.0
weight: 3
---

**Blitz for your whole organization.** Connect Blitz to your organization's Blitz Enterprise service and it becomes your company's Blitz: you sign in the way your company does, use the models it offers through its gateway, work under its catalog of agents, skills and rules, keep shared workspaces in step with your team, and run jobs on the service while your laptop is closed. It wears your organization's name, logo and colours. When the service is down, Blitz carries on with your files.

### Get started

1. Update Blitz (it updates itself) or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.9.0).
2. Open **Settings › Organization**, enter your organization's address (your admins have it; it's also on the service's own page), and **Sign in**. In the terminal: `blitz enterprise connect <address>`, then `blitz enterprise login`.
3. That's it: the Workspaces menu lists your organization's workspaces, Settings › Providers shows its models, and Studio shows its agents and jobs. [Your organization](../guide/organization.md)

No organization? Nothing changes for you: Blitz works exactly as before.

### What's new

**Sign in once, the way your company does.** Blitz signs in through your organization's identity provider in your browser (Google, Okta, Microsoft and the like), and keeps the sign-in in your keychain. Your admins can set the organization for every machine through device management, so there's nothing to type at all.

**Your organization's models, no keys of your own.** Its models come through its gateway, with what each can do and costs. Settings shows your usage this month and your budgets and limits, and when a limit is reached Blitz says which, and when it resets. `blitz enterprise usage` shows the same in the terminal.

**Its agents, skills and rules, alongside yours.** Your organization's catalog of agents, skills, commands, rules, guardrails, MCP servers and connections joins your own. Each one is either a default you can override, or locked, which always applies. Admins publish a workspace's items to the catalog from Studio (**Publish to organization…**) or `blitz enterprise publish`, for review.

**Shared workspaces that keep themselves in step.** Open one of your organization's workspaces from the Workspaces menu and Blitz clones it and syncs it as you save. **Save now** in the status bar sends your changes at once, and you can share a folder with your team where it already is. If you're only a Viewer of a workspace, your copy of it is read-only.

**Jobs that run on the service.** Mark a job `runs_on: service` and it runs on your organization's service on its schedule, laptop closed or not. Its runs show live in Studio and the terminal, with their files. A run that needs your approval waits for you in the tray. When a run succeeds, its changes land on the workspace's main and sync into your folder. A run whose changes clash with yours waits on its own branch for **Add to my workspace**.

**It looks like yours.** Your organization's name, logo and colours appear in the window, the menus, the tray, the terminal's banner, and the page your browser ends on after signing in. Blitz asks you to accept your organization's terms when it has them.

**Works offline, too.** If the service is down or you're offline, your workspaces still open, and the files, editor, Git and the terminal work as usual. The agent says it's waiting for your organization. Blitz gives up on a service that doesn't answer within seconds, and the Files view shows what it's waiting for.

**Open a page in a browser.** Right-click an HTML file in Files for **Open in Browser** (a tab in Blitz) or **Open in System Browser**, where its scripts run and the scripts, styles and pictures beside it load as they would from a web server. **Open** stays the quick preview.

### Good to know

- Everything organizational is opt-in: without `[enterprise] url`, Blitz never contacts an organization's service.
- Your own providers still work beside your organization's where its policy allows them. Settings says what the policy allows.
- The catalog and branding are kept on your computer, checked against your organization's signature, so Blitz looks and behaves the same offline until the catalog expires.
- Vector search in a SQLite database (sqlite-vec's `vec0` tables) works on Linux computers with Intel processors, where it could stop with "out of bounds memory access".
