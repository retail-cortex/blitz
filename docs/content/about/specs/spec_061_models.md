---
title: "061 · Every provider's models, one click away"
weight: 61
---

*A provider has many models: Gemini has gemini-3.8-flash and gemini-4-carbon, Anthropic has several Claudes, Ollama has whatever you pulled. Blitz shows them all, grouped by provider, wherever a model is chosen. The status bar's agent and model become two buttons, so the agent and the model are picked independently, and the model menu lists every model of every provider you've set up. The CLI's `/model` picker lists the same.*

| | |
|---|---|
| Status | **Built** (2026-11-08, the owner's request: "This type of control is missing and is fundamental. The UI should support many providers and providers have many models."). Decisions in §6 taken as recommended. |
| Source | The list in `pkg/engine/runtime/listmodels.go` (`ListModels`, `ConfiguredProviders`, `ChatModel`, `ChatModels`, `NotSetUp`, `ollamaAnswers`); `ConfigService.ListModels` with `chat_only` and `refresh` (`proto/blitz/v1/config.proto`, `pkg/service/server/models.go`, its 10-minute cache); the backend's `ListModels` (`pkg/engine/models.go`, its own cache; `pkg/client/client.go`); the status bar's agent and model buttons in `apps/desktop/web/src/StatusBar.tsx` and `apps/desktop/web/src/ModelMenu.tsx`, the menu's filter in `pkg/webui/src/controls.tsx` (`Menu` `searchAt`, `filterMenu`); the CLI's picker in `apps/cli/internal/tui/pickers.go` |
| Depends on | [spec_056_conversation_adapters](spec_056_conversation_adapters.md) (CA-100 a conversation's own model, what a switch costs), [spec_053_enterprise](spec_053_enterprise.md) (EN-20 the organization's models, EN-35 own providers), [spec_005_approvals](spec_005_approvals.md) (APR-14c auto mode with a local model) |

## 1. Purpose

Blitz can run on many providers, and each provider has many models, but picking one means knowing its exact name. The desktop's status bar shows the agent and the model as one button whose menu lists only agents, and a conversation's model can be changed only by typing a name in the workspace's settings. The CLI's `/model` picker offers only the current model, pinned ones and ones with settings. Choosing a model is fundamental, so it should be one click, with every model the person can use in view.

## 2. The model list

- **MOD-01** **Every provider set up, every chat model.** `Workspace.ListModels` answers, for each provider set up (`ConfiguredProviders`: the organization's first, then the person's own as its policy allows, EN-20/EN-35), its chat models: the provider's own list, with Blitz's facts for each (display name when the provider gives one, context window, whether it calls tools, sees images and thinks, its price), sorted newest family first then by name. A provider that can't be reached is listed with why, and without models.
- **MOD-02** **Chat models only.** Models that can't hold a conversation are left out (`chatModel`): embeddings, image and video generators (Imagen, Veo, the `-image` models, which Pictures lists, spec_058), speech (`-tts`, audio-only), and retired families the provider still lists (`aqa`, `learnlm`). A model whose name Blitz doesn't know is kept: it might be new.
- **MOD-03** **Ollama when it answers.** Ollama is listed when it's the provider, when `[llm.ollama] base_url` is set, or when Ollama answers at its default address on this machine within a short timeout (300 ms), with the models it has pulled. It needs no key.
- **MOD-04** **Kept a while.** A provider's list is kept for 10 minutes per workspace, so opening the menu is instant; **Refresh** (and `/model --refresh`) asks again. A provider set up or removed in Settings clears the list.
- **MOD-05** **Choosing checks.** A model chosen from the list is built before it's used; one that can't be (its key gone, a model the organization's policy doesn't allow) is refused with why, as typing its name is (spec_056).
- **MOD-06** **Not set up yet.** Below the providers set up, the list names the ones that aren't, each with **Set up** (Settings › Providers at that provider), so a person who wants Claude sees how to get it.

## 3. The status bar: agent and model apart

- **MOD-10** The status bar's agent-and-model button becomes two: **agent** (its icon and name) and **model** (the conversation's model, else the workspace's). Each opens its own menu, so either is changed without touching the other.
- **MOD-11** The **agent** menu is today's: the workspace's agents, each with its description and pin, and **Run settings…**.
- **MOD-12** The **model** menu lists MOD-01's models grouped by provider (a heading for each, with a mark for the organization's), the current one checked, each with a one-line detail (its price per million tokens in and out, its context window, "no tools" when it can't call them). With more than 12 models, a search box filters them as you type. Choosing one sets the conversation's model (CA-100: the switch's cost is said, as `/model` says it), or, before the first message, the workspace's. **Use for new chats in this workspace** at the bottom makes the current model the workspace's default. **Refresh** and the not-set-up providers (MOD-06) end the menu.
- **MOD-13** When the active agent is pinned to a model, the menu says so ("Coder runs on claude-haiku-4-5: its pin decides") and choosing a model changes the conversation's model for the other agents, as `/model` does.
- **MOD-14** Auto mode with a local model (APR-14c) holds as before: choosing an Ollama model shows the mode's warning.

## 4. The CLI

- **MOD-20** `/model` typed with a name switches as today. `/model` alone on a terminal opens the picker with MOD-01's models, grouped by provider (the provider as a dim heading), the current one first and marked, the detail as in MOD-12; typing filters. `/model --refresh` asks the providers again. Without a terminal, `/model` prints the current model and the list (`blitz models` prints the same list).
- **MOD-21** `blitz models [provider…]` lists the same models, chat models only, with `--all` for everything the provider lists.

## 5. Tests

- **MOD-30** `chatModel` against each provider's names (embeddings, image, video, speech and retired ones out; unknown ones in); `ListModels` with fake providers (one failing, Ollama answering and not, an organization with and without own providers), its cache and refresh; `Workspace.ListModels` and the service call; the desktop's model menu (grouped, the current one checked, search, choosing sets the conversation's model, before a message the workspace's, the pin's note, Refresh, Set up) in the page's tests and the fake app; the CLI's picker with grouped models and filtering.

## 6. Decisions

Taken as recommended (2026-11-08), the owner to confirm:

1. Models are sorted **newest family first** within a provider (recommended: the one you probably want is on top), or alphabetically.
2. Unknown model names are **kept** (recommended: new models appear before Blitz knows them), or hidden until Blitz knows them.
