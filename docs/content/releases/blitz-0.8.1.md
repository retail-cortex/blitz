---
title: Blitz 0.8.1
weight: 1
---

**Use the models your company already provides.** Blitz 0.8.1 works with your company's model gateway. If your organisation gives you Claude and GPT models through its own gateway, one that signs each request with special headers and a short-lived token, Blitz can now use it directly: no personal API keys, nothing extra to run.

### Get started in three steps

1. Update Blitz (the app updates itself) or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.8.1).
2. Open **Settings › Providers & keys** and choose **API**.
3. Enter your gateway's address, the headers it expects and the script that fetches your token. Then pick a model and start a chat.

Full guide: [your company's gateway](../guide/providers.md#your-companys-gateway).

### What's new

**Your gateway, your sign-in.** Tell Blitz how your company signs in: a short shell, Python or TypeScript script that prints a token. Blitz puts the token wherever the gateway wants it, in a header or in the address, and fetches a fresh one when the old one expires, without interrupting your work.

**Claude and GPT through one address.** Pick a Claude model and Blitz talks to it the way Claude expects; pick a GPT model and it does the same for GPT. Settings a model doesn't accept are left out for you, so a model swap never breaks a chat.

**Your keys stay private.** Personal API keys on your machine never reach the gateway, your tokens and headers never show up in the app, and an error tells you what the gateway said with the token hidden.

**Check it in one command.** `blitz doctor` tries your gateway and tells you what works and what to fix.

**Every provider, on one page.** The new [Providers](../guide/providers.md) guide walks through setting up each one, from Gemini and Claude to OpenAI, Ollama, Amazon Bedrock, Azure, Vertex AI and your company's gateway, with a key or a sign-in.

### Good to know

- Tested with a real gateway using Claude and GPT models. Gemini through a gateway depends on the gateway supporting it.
- Blitz now shuts down cleanly even while a project is still opening.
- Your settings, workspaces and conversations carry over from 0.8.0.
