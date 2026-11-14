---
title: Blitz 1.1.0
weight: 1
---

Know where everything lives. Settings is now everything every workspace shares, and each workspace's Studio is everything that's its own, its settings included. Both lay out your agents, skills, evals, jobs, connectors, secrets and browsers the same way, under **Resources**, so once you've found something in one, you know where it is in the other. And your own agents and skills, the ones every workspace has, can finally be proven with evals too.

### Get started

1. Update Blitz. It updates itself, or run `sudo apt upgrade` or `brew upgrade --cask blitz`, or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v1.1.0).
2. Open **Settings** (the gear): six entries now, each with its tabs. Your own agents, skills and the rest are under **Resources**.
3. Click the pencil beside a workspace in **Workspaces**: its own settings open in its **Studio**, beside its resources.
4. To check one of your own agents, open it in **Settings › Resources › Agents** and choose **Evals**.

### New

- **One place for each thing.** Settings holds what every workspace shares; a workspace's Studio holds what's only its own, starting with its settings. No more wondering whether a workspace's model lives in Settings or in Studio.
- **Resources, laid out the same everywhere.** Agents, Skills, Evals, Jobs, Connectors, Secrets and Browsers are tabs under **Resources**, in Settings for every workspace and in Studio for this one. MCP servers, and what they offer, sit under Connectors.
- **Evals for your own agents and skills.** Draft, run and refine evals for the agents every workspace shares, from **Settings › Resources › Evals**. They run in a folder of their own, so they never touch a project.
- **A shorter Settings.** Ten entries in four groups became six: Providers (with your organization's sign-in), Personality (with Voice), Appearance, Resources, Access (workspaces and permissions) and System (the service, its logs, and About).
- **Choose a workspace's browser where its agents work.** Studio › Resources › Browsers shows your browser profiles and which one this workspace uses; **Use here** switches it.
- **More room to search.** The search bar now stretches across the top bar, and Help is a ? icon beside Settings.

### Good to know

- Everything you'd set is where it was, just regrouped: nothing is reset or moved on disk.
- Your own evals can use your own agents and skills and Blitz's built-in ones. A project's agents keep their evals in that project's Studio.
- Without **Show advanced**, a workspace's Studio shows just its Settings and Connectors.
