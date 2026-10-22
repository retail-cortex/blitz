---
title: Guardrails
weight: 60
---

**Studio › Guardrails** keeps the agent safe and within limits. Guardrails watch every step of a turn: your prompt, each tool call before it runs, what each tool brings back, and the answer. Three are built in and on by default; you can tune them, switch them off, or write your own in plain English. For the quality of the work rather than safety, use [rules](rules.md).

## Open it

Choose **Studio › Guardrails**. Studio's menu shows once **Show advanced** is on, in the Settings title bar.

![Studio › Guardrails: the built-in guardrails, each with a switch, and your own](../../../../images/desktop/studio-guardrails.png)

**Built in** lists the three, each with the stage it checks, what it does and a switch:

- **Prompt injection**: instructions hidden in web pages, files and tool output.
- **Secrets and personal data**: credentials, and personal data if you choose, kept out of answers.
- **Limits**: spending, model calls, files changed, domains and commands.

**Your own** lists the guardrails you've written.

## Tune a built-in guardrail

1. Choose a built-in guardrail, or its settings icon.
2. Change its settings. For **Prompt injection**:
   - **When it fails**: **Warn** lets it through and tells the agent it's data to work with, not orders; **Block** withholds it.
   - **Confirm with a model**: a fast model checks each match before acting, so quoted or discussed text isn't flagged. It costs a model call per match.
   - **Tools**: the tools whose results it watches, comma-separated. Empty means those that bring in outside content (web, browser, MCP servers).
   - **Guards**: every agent, or only the agents you choose.
3. **Try it**: paste what a tool might bring back, such as a web page's text, and choose **Check** to see whether it's caught.
4. Choose **Save**. Its settings go in its file in `.agents/guardrails`, shared with your team.

![The Prompt injection guardrail's settings, with Try it](../../../../images/desktop/studio-guardrails-edit.png)

**Secrets and personal data** masks credentials in answers (**Redact**, **Warn** or **Block**); **Personal data too** adds email addresses, phone numbers, card numbers and social security numbers. **Limits** sets the most spent, model calls and files changed per turn, the allowed domains, and commands not to run. Switching a guardrail off is saved in its file too; **Reset to defaults** deletes the file, so it's on again with its default settings.

## Write your own

1. Choose **+** (**New guardrail**).
2. Choose what it **Checks**: **Prompts**, **Tool results**, **Answers** or **Actions** (each tool call, before it runs).
3. Choose **Block** or **Warn**.
4. Say **What to catch**, in plain English, and what's fine, to avoid false alarms: *Deleting a git branch, or a database table.*
5. **Try it**, then **Save**. It's saved in `.agents/guardrails`.

A guardrail that can't be checked and blocks stops what it guards.

More: [Extending Blitz](../../../guide/extending.md#project-memory) and [Safety](../../../guide/safety.md).
