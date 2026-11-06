---
title: Blitz 0.9.3
weight: 1
---

**What you see in the terminal is what the model did.** With Gemini, the CLI showed every tool call twice, so a file read once looked like it was read again. Each call now shows once.

### Get started

Update Blitz (it updates itself) or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.9.3).

### Fixes

- The CLI showed each tool call twice with Gemini, and `--output-format stream-json` wrote it twice. Each call shows once now, as in the desktop app.
