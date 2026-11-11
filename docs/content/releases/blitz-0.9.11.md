---
title: Blitz 0.9.11
weight: 1
---

Your agents stop rereading the same files. They remember what they've already seen, so turns are faster and each one costs fewer tokens.

### Get started

1. Update Blitz. It updates itself, or run `sudo apt upgrade`, or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.9.11).
2. Ask an agent about a file, then ask a follow-up. It answers from what it already read instead of fetching the file again.

### New

- **A file read once stays read.** When an agent asks for a file it has already seen and the file hasn't changed, Blitz tells it "you have this already" instead of sending the whole file again. If the file changed, or the conversation was summarized, it gets the fresh copy.
- **Short files come in one go.** Agents used to read a 200-line file sixty lines at a time, one call per piece. Files up to 400 lines now arrive whole.
- **Wrong path? Blitz suggests the right one.** When an agent looks for a file in the wrong place, Blitz names the closest matches in your workspace, so it stops guessing.
- **Smoother handoffs.** When one agent hands over to another, as `/setup` does, the next one uses what the first already read instead of starting over.

### Good to know

- An agent that really needs a file again can still ask for a fresh copy.
- **Includes the latest security fix** for Blitz's web connections (Go 1.27.4).
