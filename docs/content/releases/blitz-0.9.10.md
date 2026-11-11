---
title: Blitz 0.9.10
weight: 1
---

Your company's gateway, your way: every model it serves, each in the API it speaks. And the window moves when you drag it.

### Get started

1. Update Blitz (it updates itself, or `sudo apt upgrade`), or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.9.10).
2. Open **Settings › Providers & keys › Custom gateway**, then **Add the gateway's models**. Each one arrives with the API its name suggests; change any that's wrong, and save.
3. Pick one in the model menu and ask it something.

### New

- **Every model your gateway serves, in the menus.** Blitz asks the gateway for its list (at `/models`, or where you say), with your headers and token, and understands OpenAI's, Anthropic's, Gemini's and Ollama's answers. A gateway that doesn't list them? Switch it off and add the models yourself.
- **Each model in its own API.** Choose per model: OpenAI Responses, OpenAI-compatible (Chat Completions), Anthropic Messages (Claude, or any Anthropic-compatible model), the Gemini API or Vertex AI. **Every other model** sets the rest in one go.
- **Your gateway's paths, not ours.** With **Show advanced**, set where each API's requests go under the endpoint: `{model}` and `{method}` fill themselves in. Nothing about your gateway needs to be built into Blitz.
- **Gemini through your gateway.** `gemini-…` models now speak Gemini's own API by default, or Vertex AI's when you pick it.

### Fixes

- **Drag the window from anywhere free in the title bar.** Only the logo's corner used to move it; now the whole space between the menus and the search field does, and with Settings (or any view) open, its header row moves the window too.

### Good to know

- Already using a gateway? Your models answer as before: Claude's requests still go to `/responses` with the others'. Two things are new: a `gemini-…` model now gets the Gemini API rather than OpenAI's (set it to **OpenAI Responses** if your gateway wants that), and Blitz asks the gateway for its models at `/models`. If your gateway has no list there, the model menu says so: point **The gateway lists its models** at the right path, or switch it off.
- In the settings file it's `[llm.api.apis]`, `[llm.api.paths]` and `models_path`: see [your company's gateway](../guide/providers.md#your-companys-gateway). `blitz models api` lists the gateway's models, and `blitz doctor` says what each model speaks and where.
