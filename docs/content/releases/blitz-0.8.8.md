---
title: Blitz 0.8.8
weight: 1
---

**Pick up right where you left off.** Blitz now remembers where each conversation stood: what you were doing, what got done and decided, and what's next. Start a new conversation tomorrow, say "where were we?", and your agent answers from that, plus the state of your repository. Blitz is also no longer just a coding agent. It's an agent for all the work in your workspace (code, documents, research, data) that hands real code changes to a specialist with the right skills loaded for your stack.

### Get started in three steps

1. Update Blitz (it updates itself) or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.8.8).
2. Work as usual, then switch to a new conversation. Blitz writes down where the old one stands as you go.
3. In the new conversation, ask "Let's pick up where we left off". Your agent tells you where the work stands and what's next, then checks what changed since.

### What's new

**Conversations that pick up where you left off.** When you move on from a conversation, Blitz writes a short handoff: where it stands, what's done, the decisions you made (and what you ruled out), what's still open, and the next step. A new conversation starts out knowing the latest one in full and the four before it in a line each, so "what did we do yesterday?" gets a real answer. Ask about an older one and the agent reads it with its new `recall_session` tool. The summaries stay on your machine, beside the agent's notes, with any credentials masked. They never go in your project. [Picking up where you left off](../guide/extending.md#picking-up-where-you-left-off)

**An agent that knows the workspace.** Each conversation starts with where your repository stands: the branch, what's uncommitted and the latest commits. Ask where the project is and Blitz reads your specs, your instructions, git and the code before answering. It doesn't tell you it has no context, and it doesn't hand you commands to run yourself.

**Blitz the generalist, with a coder.** Blitz now does the work in your workspace, whatever it is: a document, a data question, research, a quick fix. A substantial code change goes to the new built-in **coder**, with the goal, the files involved and what done looks like. Small edits (a line, a setting, a typo) Blitz makes itself, since handing those off costs more than the edit. The working rules that make Blitz's code good are kept word for word in both. [Agents](../guide/agents.md)

**Skills for your stack.** The coder looks at what a change touches and loads the matching skills before writing code. **Go** covers errors, small interfaces, context and goroutines, with `-race` tests. **Python** uses your project's own tools (`uv run`, Poetry), pytest, ruff and type checks. **React** covers your stack's styles, state and effects done right, accessibility and tests by role.

**Playwright, with the built-in browser.** Ask for end-to-end tests and the agent first walks the flow in Blitz's own browser, through your approvals, then keeps it as Playwright tests. A failing test's steps are replayed the same way to see what broke. No Playwright config yet? It offers to write one from your dev command, port and package manager, and adds the `test:e2e` script and `.gitignore` entries.

**Small talk stays small.** "Good morning" gets a sentence back, not a question form.

**Each provider keeps its own model.** Switch from Ollama to Gemini and back, and Ollama still has your model. Ollama has its own `[llm.ollama]` settings and no longer gets sent OpenAI's `gpt-4o` (the cause of a puzzling "404 Not Found"). With no model chosen, it uses the first one you've pulled. A provider's model list shows only that provider's models. [Providers](../guide/providers.md)

**Errors that say what went wrong.** A failed call now says what the provider said, not just a status code: "Ollama doesn't have the model … run `ollama pull …`", or open the Ollama app when it isn't running. Keys are always masked.

**Local models do more.** Small models often write a tool call as text instead of making it. Blitz now spots those calls in the reply and runs them.

### Good to know

- **Local models are less reliable as agents.** A model on your machine is far smaller than Gemini, Claude or GPT, and agent work shows it. Small ones often describe the steps instead of running them, or make up an answer. Settings › Providers now says so under Ollama. For agent work, choose one with strong tool calling, such as `qwen3:8b` or larger. Code-completion models such as `qwen2.5-coder` are weaker at it. [What to expect from local models](../guide/providers.md#ollama)
- Summaries are written by the small model (the one the welcome suggestions use), at most one at a time, and short conversations (one question and its answer) get none. In the CLI, a conversation you quit is summarised the next time it's needed. Turn summaries off with `[memory] summaries = false`.
- Conversations from before this release get a summary the first time a new conversation lists them or the agent asks for one.
- A summary is a record of what was said. The agent checks the workspace before relying on it.
