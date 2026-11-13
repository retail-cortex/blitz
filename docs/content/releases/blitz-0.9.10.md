---
title: Blitz 0.9.10
weight: 5
---

Does your company run its own AI gateway? Blitz now works with all of it. Every model the gateway offers shows up in Blitz, and each one talks to the gateway the way the gateway expects. And you can finally drag the window from anywhere in the title bar.

### Get started

1. Update Blitz. It updates itself, or run `sudo apt upgrade`, or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.9.10).
2. Open **Settings › Providers & keys › Custom gateway** and click **Add the gateway's models**.
3. Check the API next to each model (Blitz guesses from the name), save, and pick one from the model menu.

### New

- **All your gateway's models in one click.** Blitz asks the gateway what it offers and adds every model. If your gateway doesn't list them, type them in yourself.
- **Claude, Gemini and GPT behind one gateway.** Each model uses its own API: OpenAI, Anthropic, Gemini or Vertex AI, or anything OpenAI- or Anthropic-compatible.
- **Blitz fits your gateway, not the other way round.** If your gateway uses its own URL paths, set them under **Show advanced**. You don't have to wait for Blitz to support your setup.

### Fixed

- **Drag the window by its title bar.** It used to move only from a small corner by the logo. Now any empty space in the title bar works, even with Settings open.

### Good to know

- **Already set up a gateway?** Your models keep working. One change: Gemini models now use Gemini's own API. If your gateway expects OpenAI's format for them, switch them to **OpenAI Responses**.
- **No model list?** If your gateway doesn't list its models, the model menu tells you. Turn the list off in Settings, or point it at the right place.
- **Prefer the settings file?** See [your company's gateway](../guide/providers.md#your-companys-gateway).
