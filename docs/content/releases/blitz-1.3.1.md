---
title: Blitz 1.3.1
weight: 1
---

Gemini keeps talking through a quiet stretch. When your connection goes through a gateway or proxy, a Gemini reply that paused to think could stop with an error. Now it carries on to the end.

### Get started

1. Update Blitz. It updates itself, or run `sudo apt upgrade` or `brew upgrade --cask blitz`, or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v1.3.1).
2. Ask a Gemini agent for something long. It answers in full, however long it pauses along the way.

### Fixed

- **Gemini replies no longer stop with "invalid stream chunk".** A gateway or proxy between Blitz and the model can add lines of its own to a reply's stream: a keep-alive (`:heartbeat`) to hold a quiet connection open, or a field of its own. Streams are meant to skip lines they don't read, and Claude and OpenAI always did, but Google's Gemini library failed the whole reply on any line that wasn't the reply itself: `Agent execution error: [llm.api] iteratedResponseStream: invalid stream chunk: :hearbeat`. Blitz now sets aside every line a model's library doesn't read before the reply reaches it. Keep-alives still count as the stream being alive, so a reply that is slow but on its way is never taken for a stalled one.
