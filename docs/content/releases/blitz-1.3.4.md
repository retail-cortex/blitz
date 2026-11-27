---
title: Blitz 1.3.4
weight: 2
---

Long sessions stay lean. Blitz 1.3.4 trims the repeated listings, huge search dumps and stale tool outputs that used to pile up across turns, caches your conversation history with Claude from turn to turn, and stops runaway loops before they burn through your tokens.

### Get started

1. Update Blitz. It updates itself, or run `sudo apt upgrade` or `brew upgrade --cask blitz`, or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v1.3.4).
2. Work through a multi-step coding or browsing session. The context meter climbs much more slowly, and Claude turns reuse the cached conversation history.

### What's better

- **Folder listings and file searches aren't repeated.** `list_files` and `glob` now remember what they already showed in the conversation, just as `read_file` does. When an agent lists the same folder or runs the same file pattern again and nothing on disk has changed, Blitz tells it that the listing is unchanged instead of sending the whole list back.
- **Smarter `grep`.** `grep` adds `files_only` (just the matching file paths) and `count` (match counts per file) modes, and defaults to 20 matching lines in `content` mode while reporting the total match and file counts so the agent can narrow its query instead of pulling hundreds of lines into the chat.
- **Claude prompt caching across turns.** In addition to caching the system instructions, Claude now places a rolling cache breakpoint on the conversation's last stable turn, so each step in a turn and each follow-up turn reuses the conversation prefix from the cache instead of re-processing it from scratch.
- **Automatic pruning of stale reads, old command output and old browser screenshots.** When a file is read again after an edit or for a new line range, earlier reads of that file in older turns are collapsed to a short placeholder. Large command outputs from older turns are trimmed to their head and tail, only the two most recent browser screenshots stay in full image form in the history, and the browser tool reports only new console lines since the last step.
- **Focused sub-agent handoffs and tighter guardrails.** Delegated sub-agents return a concise summary rather than their raw tool transcript, MCP tool results are capped at 100 KiB, plan mode filters write-tool schemas out of the model request, and a circuit breaker stops a turn on the fourth identical failing tool call instead of letting a stuck agent loop.

### Good to know

- An agent that genuinely needs a fresh folder listing or glob result can pass `force: true`, and `grep` accepts `max_matches` up to 500 when a wider look is needed.
