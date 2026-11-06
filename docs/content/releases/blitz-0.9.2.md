---
title: Blitz 0.9.2
weight: 1
---

**Change models mid-conversation, and keep going.** Every conversation now has its own model: switch it with `/model`, and Blitz tells you what the switch costs before the next turn runs. Moving between Gemini, Claude and OpenAI, or falling back from one to another when a provider is down, now just works, where before some switches ended in an error.

### Get started

1. Update Blitz (it updates itself) or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.9.2).
2. In any conversation, type `/model anthropic/claude-opus-5-5` (or any model you've set up). Blitz says what the next turn will read and cost, then carries on with the new model.
3. To start fresh on a model instead, type `/model anthropic/claude-opus-5-5 --new`.

### What's new

**A model for each conversation.** `/model` changes the conversation you're in, not every conversation in the workspace. The status bar shows the conversation's model. A conversation that's too long for the model you move it to is summarised first.

**Switching providers is safe.** A conversation started on Gemini goes on with Claude, and the other way round, without the provider refusing what the other one wrote. Each model gets back only its own reasoning; tool calls keep their pairing whichever model made them.

**Fallbacks that really fall back.** When your main model fails partway through a task, the fallback picks up from where it stopped, even across providers. Before, a fallback could quietly ask for the main model again.

**Long Claude conversations keep going after a summary.** A Claude conversation that had been compacted could stop with "conversation must start with a user message". It goes on now.

**Costs you can trust.** OpenAI's reasoning tokens are counted once, not twice, and a fallback model with no price is shown as unpriced instead of being charged at your main model's rate.

**Tools from MCP servers with unusual names** (dots, long names) now work with every provider.

### Good to know

- `/model` with a conversation open changes only that conversation. To change what new conversations start on, use `/model … --new` or the workspace settings.
- Switching from a stronger model to a weaker one in the middle of a long task can cost quality: a new conversation with a short summary often works better.
