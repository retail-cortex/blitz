---
title: Blitz 0.8.6
weight: 1
---

**Set up in a minute, not an afternoon.** This release makes the first ten minutes with Blitz much shorter. Providers & keys shows every model provider at once and spots the ones you already have, a key is checked the moment you paste it, and connecting a database is pick a kind, paste a URL, done.

### Get started in three steps

1. Update Blitz (it updates itself) or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.8.6).
2. Open **Settings › Providers & keys**. If a key is in your environment, you're signed in with `gcloud` or `ant`, or Ollama is running, it's already listed: press **Use**.
3. Type `/connect postgres://you@your-host/your-db` (or `/connect ~/data/notes.sqlite`) in the chat, press **Test and save**, and ask your agent about your data.

### What's new

**Every provider at a glance.** Providers & keys lists Gemini, Claude, OpenAI, Ollama and your company's gateway together, each saying where it stands: a key in the keychain, a key found in the environment, signed in, or not set up. Blitz notices what's already on your machine (an `ANTHROPIC_API_KEY`, a Google Cloud or Anthropic Console sign-in, Ollama running) so switching is one click. [Providers](../guide/providers.md)

**Keys checked as you paste.** Paste an API key and Blitz tries it before saving: it says the key works and how many models it reaches, or exactly why not. **Get a key** takes you to the right page for each provider.

**No more silent first message.** Until a model is ready, the welcome page says so with **Set up a provider**, and the "model isn't available" bar in a chat now has a button that takes you straight to the fix.

**Databases in four steps.** A new connection starts with its kind: SQLite, PostgreSQL, BigQuery or AlloyDB, simplest first. Paste a connection URL and the fields fill themselves (the URL, with its password, is cleared). Pick a SQLite file with **Choose…**. The name follows the database. **Test and save** connects before saving, and the connection then stays open so you can choose its tools. [Connectors](../guide/connectors.md)

**Connect from the chat.** `/connect` with a URL or a file, or the new database button beside the composer, opens a new connection ready to save. Connectors is in Settings without advanced settings now.

**A tidier Settings.** Your own agents, skills, jobs and secrets are tabs of one **Library**, and MCP servers sit with the databases under **Connectors**: ten entries instead of fourteen. Old links still land in the right place.

**Run the agent's commands yourself.** When the agent suggests a shell command, its code block has a ▶ button: the command lands in the chat box as `!make test`, in marigold so you can't mistake it for a message. Press Enter and it runs in your workspace, with its output streaming into the chat (Stop any time). Or type `!` and any command yourself, just like in the terminal. Your commands run as you, outside the agent, and the agent never sees them.

**MCP servers for one project.** A server only one workspace needs (its staging database, its design files) now lives with that workspace: add it in **Studio › Connectors** or with `blitz mcp add --workspace`. Switch any of your global servers off for one workspace without touching the others, and **Make global** when a workspace's server should be everywhere. [MCP servers](../guide/mcp.md#one-workspaces-servers)

**Guardrails you can trust out of the box.** The built-in prompt-injection guardrail now takes injected instructions out of what a tool returns and keeps the rest of the page, and has a fast model confirm a match first, so an article that merely quotes an attack isn't flagged. The limits guardrail catches force pushes and deletes of your disk or home folder however they're spelled (but lets `--force-with-lease` through), and stops a runaway turn at 200 model calls or $10. Studio's Overview now grades them on what they really do. [Guardrails](../guide/extending.md)

**Studio with names.** In a wide window Studio names each kind beside its icon, and the Overview opens a kind's details only when you click its ring.

### Good to know

- Bedrock, Azure and Claude on Vertex AI are still set in the settings file.
- Ollama is found on its usual address on the machine running the Blitz service.
- Connection URLs are read for PostgreSQL-style servers; BigQuery and AlloyDB still take their fields.
- A guardrail file you saved before keeps what it says: to get the new limits patterns, put `defaults` in its `commands` (or delete the file); for prompt injection's new redact, take `action: warn` out.
