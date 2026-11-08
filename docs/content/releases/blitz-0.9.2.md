---
title: Blitz 0.9.2
weight: 5
---

**Change models mid-conversation, and keep going.** Every conversation now has its own model. Switch it with `/model`, and Blitz tells you what the switch costs before the next turn runs. Gemini, Claude and OpenAI pick up each other's conversations, fallbacks really fall back, and your costs come with a breakdown by model. Models now search the web with their own tools, the pictures they draw show in the conversation, and Blitz learns what each model can and can't do.

### Get started

1. Update Blitz (it updates itself) or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.9.2).
2. In any conversation, type `/model anthropic/claude-opus-5-5` (or any model you've set up). Blitz says what the next turn will read and cost, then carries on with the new model.
3. To start fresh on a model instead, type `/model anthropic/claude-opus-5-5 --new`.
4. Type `/cost` to see what the conversation has cost, model by model.

### What's new

**A model for each conversation.** `/model` changes the conversation you're in, not every conversation in the workspace, and the status bar shows its model. Before switching, Blitz tells you:
- what the switch costs;
- which attachments the new model can't see;
- whether the new model can use tools at all.

A conversation that's too long for its new model is summarised first.

**Switching providers just works.** A conversation started on Gemini goes on with Claude, and the other way round, without either provider refusing what the other wrote. Each model gets back only its own reasoning, and tool calls stay paired whichever model made them.

**Fallbacks that really fall back.** When your main model fails partway through a task, the fallback picks up from where it stopped, even across providers. Before, a fallback could quietly ask for the main model again.

**Search with the model's own tools.** Gemini, Claude and OpenAI search the web, read pages and run code with their own built-in tools wherever they offer them. The sources are listed under the answer. Your permission mode, your agents' tool lists and your organization's policy still decide what's allowed, and `[tools] provider = false` turns them off.

**Pictures and audio a model makes show in the conversation**, with **Save**. Another model can see them too if you switch.

**Costs, model by model.** Blitz keeps a record of every call in each session. `/cost`, the desktop's status bar and `blitz sessions usage <id>` break the total down by model and by what each call was for. A model with no price leaves only its own calls unpriced, not the whole session. Your organization's models are priced from its catalog.

**Blitz learns what each model can do.** If a model refuses tools, images, streaming or a setting, Blitz tells you once, sends the request again without it, and remembers. Claude on Bedrock now gets its thinking and effort settings. Compaction happens at each model's own limit, not at one number for all of them. `blitz models forget <model>` clears what Blitz has learned about a model.

**Replies hold up better.** Blitz now:
- carries on when a model says the conversation is too long, by summarising it first;
- tells you plainly when a model refuses, blocks an answer or stops on recited text;
- asks once more when a tool call comes back malformed;
- keeps parts of an answer it can't show yet, instead of failing.

OpenAI and Ollama models now stream their answers.

**Fixes:**
- A folder shared with your organization could show no sync status for up to a minute after Changes pulled or pushed in it. It now shows **Saved** right away.
- A long conversation no longer makes Blitz use more memory with every turn. Once part of it is summarised, that part stays only in the session's file, and the model is sent the same prompts as before.
- A Claude conversation that had been compacted could stop with "conversation must start with a user message". It now goes on.
- OpenAI's reasoning tokens were counted twice. They're counted once.
- Tools from MCP servers with dots or long names in their names now work with every provider.

### Good to know

- `/model` with a conversation open changes only that conversation. To change what new conversations start on, use `/model … --new` or the workspace settings.
- Switching from a stronger model to a weaker one in the middle of a long task can cost quality: a new conversation with a short summary often works better.
- A model's own web search runs without the approval prompt Blitz's search tool asks for in some modes. Set `[tools] provider = false` to keep Blitz's own.
