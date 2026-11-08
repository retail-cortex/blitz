---
title: Blitz 0.9.7
weight: 1
---

**Gemini searches the web for you again.** Google now asks for one more setting when a Gemini model uses its own tools (Google Search, reading a page, running code) beside Blitz's. Without it, a conversation on Gemini 3.8 Flash could stop with "Error 400 … Please enable tool_config.include_server_side_tool_invocations". Blitz 0.9.7 sends that setting, so those conversations work again.

### Get started

1. Update Blitz (it updates itself) or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.9.7).
2. Ask a Gemini model something it needs to look up, such as "what's the latest Go release?", or add a database query to the conversation. It searches, answers and lists its sources.

### Fixes

- **Gemini's own tools work beside Blitz's.** On the Gemini API (a Gemini key), a conversation that offered Gemini's search, page reading or code running together with Blitz's tools failed with a 400. Blitz now asks Gemini to include its tool steps in the answer, and keeps them for the next turn. Switch the conversation to Claude or GPT and they read Gemini's searches as ordinary web searches. Gemini on Vertex AI wasn't affected and works as before.
