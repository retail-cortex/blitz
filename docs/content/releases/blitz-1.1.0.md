---
title: Blitz 1.1.0
weight: 1
---

Know where everything lives. Settings is now everything every workspace shares, and each workspace's Studio is everything that's its own, its settings included. Both lay out your agents, skills, connectors and secrets the same way, under **Resources**, so once you've found something in one, you know where it is in the other. Your own agents can finally be proven with evals too, and each agent shows its evals right on its row.

### Get started

1. Update Blitz. It updates itself, or run `sudo apt upgrade` or `brew upgrade --cask blitz`, or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v1.1.0).
2. Open **Settings** (the gear). Your own agents, skills and the rest are under **Resources**; your global jobs under **Jobs**.
3. Click the pencil beside a workspace in **Workspaces**: its own settings open in its **Studio**, beside its resources.
4. In **Resources › Agents**, click an agent's evals count to see what proves it, or **Draft evals** to get some written.

### New

- **One place for each thing.** Settings holds what every workspace shares; a workspace's Studio holds what's only its own, starting with its settings. No more wondering whether a workspace's model lives in Settings or in Studio.
- **Resources, laid out the same everywhere.** Agents, Skills, Connectors and Secrets are tabs under **Resources**, in Settings for every workspace and in Studio for this one; Settings also has Browser, for the browser profiles every workspace shares. MCP servers, and what they offer, sit under Connectors.
- **Evals live with your agents.** Evals are now Agents' second tab, and every agent's row says how many evals cover it: one click opens them.
- **Evals for your own agents and skills.** Draft, run and refine evals for the agents every workspace shares, from **Settings › Resources › Agents › Evals**. They run in a folder of their own, so they never touch a project.
- **Pick which of your agents each workspace gets.** A workspace's Studio lists your own agents under its own, each with a switch: turn one off and that workspace no longer offers it, while every other workspace keeps it.
- **Switch rules on and off.** Every rule in Studio › Rules has a switch, so you can pause a check without deleting it.
- **Jobs where they belong.** Jobs sit right above Workflows in Studio, and have their own place in Settings.
- **A shorter Settings.** Ten entries in four groups became seven: Providers (with your organization's sign-in), Personality (with Voice), Appearance, Resources, Jobs, Access (workspaces and permissions) and System (the service, its logs, and About).
- **Your gateway, your paths.** In a custom gateway, Gemini and Vertex AI paths no longer have to include the model or the method: send every call to one fixed path if that's how your gateway works. The ⓘ beside each path lists what it can fill in.
- **Ollama at work.** When your organization allows it, keep using Ollama on your computer, or another provider of your own, alongside its models: Settings › Providers lists just the ones it allows, with the models it allows of each, and the model menu offers only those.
- **Your organization's policy, explained.** Settings › Providers › Organization › **Your policy** shows everything your organization decides for you, and who decided each: everyone, your group, or an admin just for you. When something's refused, Blitz says who decided, so you know whom to ask.
- **More room to search.** The search bar now stretches across the top bar, and Help is a ? icon beside Settings.

### Good to know

- Everything you'd set is where it was, just regrouped: nothing is reset or moved on disk.
- Switching one of your own agents off is your choice for that workspace and isn't shared with your team; switching a rule off is saved in the rule's file, so it travels with the project.
- A workspace's own agent can't be switched off there: choose another one first.
- Your own evals can use your own agents and skills and Blitz's built-in ones. A project's agents keep their evals in that project's Studio.
- Without **Show advanced**, a workspace's Studio shows just its Settings and Connectors.
