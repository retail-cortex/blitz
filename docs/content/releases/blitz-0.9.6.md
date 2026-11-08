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

### Fixes

- Auto mode's reviewer denied a new file in `.agents/scratch/` as "outside the workspace". Blitz now tells the reviewer each file's full path and that it's inside the workspace, and a denial that says otherwise goes to you instead.
