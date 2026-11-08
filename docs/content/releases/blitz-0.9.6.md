---
title: Blitz 0.9.6
weight: 1
---

**Local models that actually do the work.** With a model running on your computer through Ollama, the agent writes files and runs its tools again instead of pasting what it would have written. And when auto mode's reviewer says no, you decide with a click.

### Get started

1. Update Blitz (it updates itself) or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.9.6).
2. Choose an Ollama model, such as `ollama/qwen2.5-coder:7b`. The status bar shows ⚠ **Ask**: Blitz asks you before changes instead of letting the model judge its own.
3. Ask it to write a file. Approve it in the card, and it's written.

### What's new

**You decide when the reviewer says no.** In auto mode, a reviewer model approves the agent's actions for you. When it denies one while you're in the chat, you now see it with the reviewer's reason and two buttons: **Allow anyway** or **Deny**. Before, the denial went straight back to the agent, and typing "I approve" did nothing. The terminal asks the same way.

**Auto mode waits for a proper reviewer.** Without a reviewer model of its own, auto mode used the chat's model to review the agent's actions. A small model on your computer would judge its own, and get it wrong. Blitz now uses **Ask** while an Ollama model does the work, and says why. Set a reviewer model (`[permissions.auto] model`, such as `gemini/gemini-3.8-flash`) to use auto mode with a local model, or switch to a cloud model and auto comes back. [How auto mode works](../guide/safety.md).

**Ask Blitz to set itself up.** "Create a SQLite database of my recipes and add a connector for it" now just works: the agent creates the file, shows you the connection it'll save, and once you approve, `@db:recipes` works in your next message. The same goes for switching models, checking why something doesn't work ("why isn't Ollama working?"), adding an MCP server, an agent, a skill, a rule or a workflow, and opening Studio, Settings or Pictures for you. Every change is approved first; passwords are typed by you, hidden, and never reach the model; and in the terminal, `blitz do` runs the same commands. [Asking Blitz to set itself up](../guide/blitz-commands.md).

**Every model, one click away.** The status bar's agent and model are now two buttons. Click the model for every model of every provider you've set up, Gemini's, Claude's, OpenAI's and whatever Ollama has pulled, grouped by provider, with each one's price, context window and whether it calls tools; type to filter. Choose one and the conversation moves to it, or make it the default for new chats. In the terminal, `/model` lists the same. [Switching models](../guide/models.md#switching-models-in-a-conversation).

**Choose your providers, and your default.** Settings › Providers now lets several providers be active at once: **Activate** makes a provider's models available without changing what you use. **Default model** at the top is the one choice of the model new chats start on, from every active provider. Each provider's **Models in the menu** chooses what the model menu lists for it; left empty, it lists the latest models only (Gemini 4 and 3.8, Claude Opus, Sonnet and Haiku, GPT 6.1 and 5), not every preview and old snapshot.

**Pictures from OpenAI.** With an OpenAI key, Pictures and the agent's `generate_image` draw with OpenAI's image models too (GPT Image 2.5 Sunburst and Flare, GPT Image 2 and earlier): up to four at once, square, landscape or portrait, starting from up to 16 of your pictures.

**"Open it" opens it.** Ask the agent to show a file it wrote, or any file in your workspace, and it opens in Blitz's editor (Markdown rendered, at the line it names), instead of a browser. [Opening a file for you](../guide/search.md).

### Fixes

- Local models on Ollama wrote their tool calls as text, a whole plan at once, and Blitz ran none of them: `db-info.md` never got written. Blitz now runs the first, and offers a local model only its core tools, which made qwen3 call them reliably in our tests. Ollama itself stopped some long replies ("token repeat limit reached") when the model repeated itself: that's the model, and a stronger one (`qwen3:8b` or larger) avoids it.

- Auto mode's reviewer denied a new file in `.agents/scratch/` as "outside the workspace". Blitz now tells the reviewer each file's full path and that it's inside the workspace, and a denial that says otherwise goes to you instead.
