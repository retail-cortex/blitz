---
title: Blitz 1.2.0
weight: 2
---

See how the agent thinks. The new **Reasoning** view draws each turn as a map: every model call, the reasoning it shared, the tools it chose and what came back, live as the turn runs and for every chat you've had. And this release is the most hardened Blitz yet, with a new mode for when you really do want the agent outside its sandbox.

### Get started

1. Update Blitz. It updates itself, or run `sudo apt upgrade` or `brew upgrade --cask blitz`, or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v1.2.0).
2. Open a chat and click the new graph button next to **History**.
3. Pick any turn: follow the model's calls down the middle, its thinking on the left and its tools on the right. Click a card to read it in full, or **Show in chat** to jump back to that moment.

### New

- **The Reasoning view.** A turn reads top to bottom, like the chat. Each model call shows which model answered, its tokens, how long it took and what it cost. Failed tools stand out in red, and sub-agents open to show their own work. While a turn runs, a **Thinking…** card shows the reasoning as it streams, and **Follow** keeps it in view. See the [Reasoning guide](../guide/reasoning.md).
- **Gemini shows its thinking.** Gemini models that think now share their thought summaries, in the chat and in the Reasoning view. It costs nothing extra: the thinking was billed either way.
- **Unsafe mode, for when you mean it.** A new permission mode runs the agent's commands outside the sandbox, for the times a build or tool truly needs the whole machine. Only you can choose it, each time: from the mode menu (which asks you to confirm) or with `/mode unsafe`. The agent can never switch to it, it's never saved, and it's off while you're connected to an organization. The status bar shows it in red. See [Safety](../guide/safety.md).

### Safer

- **Your git setup is off limits to the agent.** It can't change a repository's git config or hooks, so nothing it writes runs later behind your back.
- **Language servers ask first.** Before a folder's own code runs for code intelligence (its Python environment, its build tool), Blitz asks you once. Until you say yes, the language servers still work, just without that folder's code.
- **Private addresses ask first.** Opening a page on your local network or an internal host asks before it goes.
- **Your sign-ins stay yours.** When the agent opens a page signed in with one of your browser profiles, a site outside that profile's own asks first, with **Allow for this chat**.
- **Approvals show everything.** Saving a skill shows exactly what changes, saving a workflow shows its schedule, its mode and every step, and only you can confirm a change, never a command the agent runs.
- **Your organization, your models.** When you connect to an organization, its models come from the organization, and your local Ollama models stay available. Disconnect and your own providers are back.
- **Viewers can't change files.** In an organization workspace you can only view, the editor and file tree say so and change nothing.
- **Steadier all round.** Many fixes for races, lost settings edits, lost usage records, terminals that could hang, and files that could be overwritten, each with a test so it stays fixed.

### Good to know

- The Reasoning view is in the desktop app. A command-line version and an outline layout are on the way.
- Reasoning shows when the provider sends it: Claude when thinking is on, Gemini's thinking models, and summaries from OpenAI's reasoning models. A local model's thinking isn't separated from its answer yet.
- Costs in the Reasoning view are estimates at today's prices.
- Without the sandbox (Windows, or with the sandbox turned off), unsafe mode's protections rest on the commands you approve.
