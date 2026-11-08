---
title: Blitz 0.9.6
weight: 2
---

Pick any model from any provider right from the status bar, ask Blitz to set itself up in plain English, and run local models that actually get work done.

### Get started

1. Update Blitz (it updates itself), or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.9.6).
2. Click the model name in the status bar and pick any model: Gemini, Claude, GPT, or one running on your computer.
3. Try *"Create a SQLite database of my recipes and connect it."* Approve the card, then ask about your recipes with `@db:recipes`.

### What's new

**Every model, one click away.** The model has its own button in the status bar. Click it to see every model from every provider you've set up, grouped by provider, with its price, context size and whether it can use tools. Type to filter, then pick one and the conversation switches to it, or make it the default for new chats. In the terminal, `/model` shows the same list.

**Several providers at once.** In Settings › Providers, turn on as many providers as you like, choose one default model for new chats, and pick which models each provider shows. Leave it alone and you get just the latest models, not every preview and old snapshot.

**Ask Blitz to set itself up.** Say what you want and Blitz does it: connect a database, add an MCP server, create an agent or a skill, switch models, or answer "why isn't Ollama working?". It shows you each change to approve first. Passwords are typed by you and never reach the model. `blitz do` does the same from the terminal.

**Local models that get work done.** Models running on your computer through Ollama now write files and use their tools, instead of describing what they would do. They get a smaller set of tools and shorter instructions, sized for a small model.

**You stay in charge in auto mode.** When auto mode's reviewer blocks an action, you see why and choose **Allow anyway** or **Deny**. A local model never reviews its own work: with Ollama, Blitz asks you before changes unless you've set a separate reviewer model.

**Pictures from OpenAI.** With an OpenAI key, Pictures and the agents can draw with GPT Image models: up to four at a time, square or wide or tall, starting from up to 16 of your own pictures.

**Diagrams you can read.** A new built-in skill teaches the agents to style Mermaid diagrams so every label stays readable, in light and dark.

**"Open it" opens it.** Ask the agent to show you a file and it opens in Blitz's editor, at the right line, not in a browser.

### Fixes

- Auto mode no longer blocks new files in the workspace's scratch folder as "outside the workspace."
- Reading a database file no longer hands the model its raw bytes; it's pointed to the database's own tools.
- Saving a command over an existing one works again.
- `blitz do` no longer loses a secret piped in after the "y".

### Good to know

- Small local models vary. qwen3 (8B or larger) uses its tools reliably; qwen2.5-coder 7B often doesn't.
- To use auto mode with a local model, set a reviewer model in your settings (`[permissions.auto] model`, such as `gemini/gemini-3.8-flash`).
