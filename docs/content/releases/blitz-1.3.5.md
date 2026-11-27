---
title: Blitz 1.3.5
weight: 1
---

Your agents search first and read only the lines they need. Blitz 1.3.5 pairs workspace search with ready-to-read line windows, caps first-time reads of long files at 500 lines, keeps command execution on your machine rather than a model provider's cloud container, and keeps your context meter accurate even when background reviewers run.

### Get started

1. Update Blitz. It updates itself, or run `sudo apt upgrade` or `brew upgrade --cask blitz`, or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v1.3.5).
2. Restart the Blitz service when the app offers to, so the updated tool and caching behaviour take effect.
3. Ask an agent to track down and change something in a large file: it searches first, then reads just the matching window of lines.

### What's better

- **Agents search first and read the lines that matter.** `search_workspace` now gives the agent a ready-to-read line window (`lines`, such as `495-555`) around every file hit, so it can pass that range straight to `read_file`. When an agent reads a long file without a line range, `read_file` now returns the first 500 lines instead of dumping thousands of lines into the chat at once—and remembers those 500 lines so a repeat call isn't sent twice. Blitz's built-in agents (`blitz`, `coder` and `planning-agent`) are tuned to use `search_workspace`, `grep` and `lsp` to zero in on the right section before reading.
- **Local commands stay on your computer.** When an agent has Blitz's shell tool (`run_shell_command`), Blitz no longer offers the model provider's remote cloud code-execution container alongside it. Gemini and Claude never try to run scripts for your local files in a cloud container that doesn't have them.
- **Claude caches your tools, and large outputs age out after two turns.** Claude now caches the tool declarations alongside the system prompt and the conversation history, so tool-heavy workspaces don't re-pay for their tool schemas on every step. Large shell or `web_fetch` outputs from more than two turns ago are trimmed to their first and last lines, keeping long sessions light without disturbing the active turn's cache.
- **Accurate context meter when the auto reviewer runs.** Background permission reviews, prompt hooks and background tasks no longer overwrite the conversation's prompt size, so the context meter in the status bar and automatic summarization always reflect your real conversation.
- **Live cost and context in the status bar.** The status bar's session cost and context meter now update after every model step while a turn is running, instead of waiting until the whole turn finishes, so you can see what a long turn is spending as it works and hit **Stop** whenever you want.
- **Clear guidance when an inline script trips the shell parser.** When a multi-line `python3 -c` or `node -e` command has quotes the shell parser refuses, Blitz tells the agent right away to wrap the script in single quotes or pass it via a quoted `<<'EOF'` heredoc instead of retrying the broken command.

### Good to know

- Files up to 500 lines still arrive whole in a single `read_file` call, and an agent can read any section of a longer file by passing `start_line` and `end_line`.
- Agents without local shell access can still use a provider's remote code-execution tool when `provider_tools` enables it.
