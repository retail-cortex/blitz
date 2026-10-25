---
title: Blitz 0.8.1
weight: 4
---

**Your company's AI, now in Blitz.** Most companies don't hand out API keys. They run a model gateway: one address, company sign-in, every request accounted for. Until now, that left Blitz out. Blitz 0.8.1 plugs straight in: Claude and GPT on your company's account, under your company's rules, with no key of your own and nothing extra to install.

### Up and running in a minute

1. Update Blitz (it updates itself) or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.8.1).
2. Open **Settings › Providers & keys** and choose **API**.
3. Paste your gateway's address, add the headers it expects, and give Blitz the command you already use to get a token. Pick a model and say hello.

Your platform team's gateway docs have the address and the token command. [Your company's gateway](../guide/providers.md#your-companys-gateway) has a complete example, and the new [Providers](../guide/providers.md) guide covers every other provider too.

### What's new

**Sign in the way your company does.** Whatever prints your token, whether your company's CLI or a few lines of Python or TypeScript, Blitz runs it, puts the token where the gateway expects it, and fetches a fresh one when it expires. No more pasting tokens that die after an hour.

**Claude and GPT, one setting.** Move between Claude and GPT without touching your configuration. Blitz speaks each model's own API to the gateway, so Claude keeps streaming, prompt caching and extended thinking, and an option a model doesn't support is dropped instead of breaking your chat.

**Nothing leaks.** Personal API keys on your laptop never reach the company gateway, tokens and header values never appear in the app, and when the gateway says no, you see its reason with the token masked.

**Know it works before you start.** `blitz doctor --online` signs in, sends a test request and tells you exactly what to fix.

### Good to know

- Tested against a real company gateway with Claude Sonnet 5.5 and GPT models. Gemini works if your gateway supports it.
- Your settings, workspaces and conversations carry over from 0.8.0.
