---
title: "058 · Pictures"
weight: 58
---

*A first-class place to make pictures: an icon beside the chat box opens **Pictures**, a panel built for image models. You choose the model, write what you want, add pictures to start from or match (from the computer or the file tree), set the shape, size, count and style, and refine the result over several turns. Every result can be saved into the workspace's file tree, opened in the editor, mentioned in a chat or used as the next reference. What the panel offers follows what the chosen model can do.*

| | |
|---|---|
| Status | **Phase 1 built** (2026-11-07, for v0.9.4), with phase 2's Imagen turns (count, avoid, people, seed, prompt improvement): PIC-01–PIC-53, PIC-60–PIC-63, PIC-70–PIC-73. **Planned:** Try again and Vary (PIC-42's buttons), Open in Pictures from the chat (PIC-54), the organization gateway's other settings (PIC-63), `blitz picture` (PIC-80), `generate_image` on the catalog (PIC-81). The owner took the proposed answers to §13 (2026-11-07): the middle panel, explicit Save, `pictures/`, the `[images] model` first. |
| Source | `pkg/images/models.go` (the catalog and its checks), `pkg/engine/runtime/pictures.go` (Gemini, Imagen and the organization's models), `pkg/engine/pictures.go` (sessions, turns, history, saving), `pkg/service/server/pictures.go` and `WorkspaceService`'s Pictures calls in `proto/blitz/v1/workspace.proto`, `apps/desktop/web/src/pictures/` (the panel) |
| Tests | `pkg/images/models_test.go`, `pkg/engine/runtime/pictures_test.go`, `pkg/engine/pictures_test.go`, `pkg/service/server/mcp_test.go` (`TestPicturesOverTheAPI`), `apps/desktop/web/src/pictures/pictures.test.ts` |
| Builds on | [spec_006_filetools](spec_006_filetools.md) FS-80–FS-82 (`generate_image`), [spec_015_models](spec_015_models.md) MDL-95–MDL-97 (`[images] model`, an organization's image models, prices), [spec_029_files](spec_029_files.md) (the file tree), [spec_011_images](spec_011_images.md) (attachments), [spec_053_enterprise](spec_053_enterprise.md) (the gateway's `image_generation`, GW-04b) |

## 1. Purpose

Today a picture comes from asking an agent: `generate_image` takes a prompt, up to three references, a ratio and a size, and saves one file. That suits "draw a poster" in the middle of other work, but not the work of making a picture: trying variations, comparing them, refining one over several turns, keeping the good one. Image models offer far more than the tool shows: up to 14 references, panoramic ratios, 4K, several images at once (Imagen), thinking, search grounding, negative prompts, output formats. **Pictures** is the place for that work, as the editor is for files: the model's controls, a history of turns, and the results one click from the file tree.

## 2. Two kinds of image model

Blitz drives two kinds, and the panel follows the kind of the model chosen:

- **Gemini image models** (`generateContent` with `responseModalities` TEXT and IMAGE; `generationConfig.imageConfig`): `gemini-3-pro-image`, `gemini-nano-banana-2.1`, `gemini-3.1-flash-image`, `gemini-3.1-flash-lite-image`, `gemini-2.5-flash-image`. They answer with text and **one** picture per turn, take reference pictures, keep a conversation (each turn sees the earlier ones, its pictures included), and some think before drawing or ground in Google Search.
- **Imagen models** (`generateImages`; `GenerateImagesConfig`): `imagen-4.0-generate-preview`, `imagen-3.0-generate-002`, `imagen-3.0-fast-generate-001` (and `-001`). Text to picture only: **1 to 4** pictures per request, no references, no conversation; a negative prompt, a person filter, a seed.

The Go SDK Blitz uses (`google.golang.org/genai` v1.74.0) has every field this spec uses: `ImageConfig` (`AspectRatio`, `ImageSize`, `PersonGeneration`, `OutputMIMEType`, `OutputCompressionQuality`), `ThinkingConfig.ThinkingLevel` (`MINIMAL`, `LOW`, `MEDIUM`, `HIGH`), `GoogleSearch.SearchTypes.ImageSearch`, and `Models.GenerateImages` with `NegativePrompt`, `NumberOfImages`, `AspectRatio`, `ImageSize`, `Seed`, `PersonGeneration` (`DONT_ALLOW`, `ALLOW_ADULT`, `ALLOW_ALL`), `OutputMIMEType`, `OutputCompressionQuality`, `EnhancePrompt` and `AddWatermark`.

## 3. The capability catalog

- **PIC-01** What each model can do is **data**, one entry per model in a catalog (pkg/images/models), never a branch in the panel: its kind (gemini or imagen), display name, references (the most in all, and per role: characters, objects, styles), the most pictures per request, the sizes, the ratios, the thinking levels (and whether thinking is always on), search grounding (none, web, web and image), negative prompt, person filter, seed, output formats, and price (per picture for Imagen; per million tokens, input and output, for Gemini, with what a picture of each size costs in output tokens).
- **PIC-02** The catalog's first entries, as the owner gave them on 2026-11-07 (Nano Banana 2.1's prices too: $1.50 a million input tokens, $0.15 cached, $30 a million image tokens, so 1K $0.0336, 2K $0.0504, 4K $0.113; search grounding $14 a thousand; its text and thinking, $7.50 a million, are counted at the image rate, a little over, as Blitz has one output price per model), **each checked against Google's documentation when it's written** (a field the documentation doesn't confirm is left at its conservative value until it is):

| Model | Kind | References (characters + objects) | Pictures | Sizes | Ratios | Thinking | Grounding |
|---|---|---|---|---|---|---|---|
| `gemini-3-pro-image` | gemini | 14 (4 + 6) | 1 | 1K, 2K, 4K | standard + panoramic | always on: Medium, High | web |
| `gemini-nano-banana-2.1` | gemini | 14 (4 + 10) | 1 | 1K, 2K, 4K | standard + panoramic | Minimal, Medium, High | web + image |
| `gemini-3.1-flash-image` | gemini | 14 (4 + 10) | 1 | 0.5K, 1K, 2K, 4K | standard + panoramic | Minimal, Medium, High | web + image |
| `gemini-3.1-flash-lite-image` | gemini | 1 | 1 | 1K | standard | fixed (minimal) | none |
| `gemini-2.5-flash-image` | gemini | 14 | 1 | 1K, 2K | standard | none | none |
| `imagen-4.0-generate-preview` | imagen | 0 | 1–4 | 1K, 2K | Imagen's | none | none |
| `imagen-3.0-generate-002` | imagen | 0 | 1–4 | fixed (~1K) | Imagen's | none | none |
| `imagen-3.0-fast-generate-001` | imagen | 0 | 1–4 | fixed (~1K) | Imagen's | none | none |

  Standard ratios: 1:1, 2:3, 3:2, 3:4, 4:3, 4:5, 5:4, 9:16, 16:9, 21:9. Panoramic: 1:4, 4:1, 1:8, 8:1. Imagen's: 1:1, 3:4, 4:3, 9:16, 16:9. Reference pictures: PNG, JPEG or WebP, at most 7 MB each. Sizes are written with a capital K (`1K`; `1k` is refused).
- **PIC-03** A model the catalog doesn't know (a new one, an organization's alias) is offered by its family (`*-image`: a Gemini image model with the most conservative of its family's values; `imagen-*`: Imagen 3's), marked "capabilities assumed", and corrected as it answers: a setting it refuses is learned (`modelfacts` `no_param`, spec_056) and its control hidden for that model from then on.
- **PIC-04** Which models are offered: the personal providers' (Gemini with a key, or Vertex AI) whose catalog entries match the provider's model list, and an organization's image models (MDL-95a). The `[images] model` is chosen first; with none, `gemini-nano-banana-2.1` when offered, else the first offered. **Owner:** the default (§13).

## 4. Opening Pictures

- **PIC-10** An icon in the chat box's row (an image with a brush, beside **Attach** and **Connect a database**, labelled "Make a picture (/picture)") opens Pictures for the workspace. So do `/picture` (with text: that text as the first prompt), the command palette, and in the file tree an image's **Make a variation** or **Edit in Pictures** (the image as the first reference), or a folder's **Make a picture here** (pictures save there).
- **PIC-11** Pictures opens as the editor's views do: a panel that slides down in the middle from under the menu bar (DSK conventions: it stays until closed, the title bar drags, nothing closes it by a click outside), taking the room the editor had; the chat beside it stays usable, and a picture can be dragged from the panel into the chat box. With no image model offered, the panel says how to get one (a Gemini key in Settings › Providers, or the organization's) instead of its controls.

## 5. The panel

- **PIC-20** Two parts: **controls** on the left (they fold to a strip), and the **session** on the right: the turns so far, newest at the bottom, the prompt box under them. Narrow, the controls go above the session.
- **PIC-21** **Model**: a drop-down of the offered models, grouped by kind, each with its price ("about 4 ¢ a picture") and, for an assumed one, a mark. Changing the model keeps every setting the new model allows and resets the rest to its defaults, saying which ("4:1 isn't offered by Imagen 4: back to 16:9").
- **PIC-22** **Prompt**: a text box (⌘Enter makes the picture), required. For Imagen its length is counted against the model's limit (480 tokens, estimated) and a longer one is refused before it's sent; Gemini's is not limited here.
- **PIC-23** **References** (Gemini models; hidden for Imagen): a drop zone taking pictures from the computer, the clipboard, the file tree (dragged, or **Add from the workspace**: a picker of the workspace's images) and the session's own results (**Use as reference**). Each shows as a thumbnail with its role: **Character**, **Object** or **Style** (chosen on the thumbnail; Character keeps a person or creature the same, Object keeps a thing faithful, Style takes the look). The catalog's limits hold: the total, and per role (a fifth character is refused, saying the most). Type and size are checked as they're added (PNG, JPEG, WebP; at most 7 MB; a larger PNG or JPEG is scaled down first, saying so). A blocked path can't be a reference.
- **PIC-24** **Pictures**: 1 to 4 (Imagen); for Gemini, 1, the control shown locked with why ("Gemini models make one picture per turn: ask again for another").
- **PIC-25** **Shape**: a grid of the model's ratios, each drawn as its shape, standard first, then panoramic; default 1:1.
- **PIC-26** **Size**: pills of the model's sizes (`0.5K`, `1K`, `2K`, `4K`), default 1K; hidden for a model with one size. The price shown follows the size.
- **PIC-27** **Thinking** (Gemini models that think): the model's levels (Minimal, Medium, High), default Medium; for one that always thinks, its levels only. The model's thoughts, when it shares them, show folded in the turn.
- **PIC-28** **Search the web** (models that ground): a switch, off by default; for models that ground in image search too, a second, **and image search**. A grounded turn lists its sources, as a chat's answers do.
- **PIC-29** **Advanced** (folded): **Avoid** (the negative prompt; Imagen); **People** (Imagen, and Gemini where the catalog allows: Don't show people, Adults only, Anyone); **Format** (PNG, JPEG with a quality slider, WebP); **Seed** (Imagen: the same seed and settings give the same picture); **Improve my prompt** (Imagen's prompt enhancement, off by default); for Gemini models, a note that what to avoid goes in the prompt ("Avoid text in the picture").
- **PIC-30** Every control's state is kept per workspace for the next time Pictures opens (the model, the settings, not the prompt or the references).

## 6. Turns and multi-turn

- **PIC-40** **Make** (or ⌘Enter) sends the request; the turn appears at once with the prompt, the references as thumbnails and the settings as chips; while it runs it shows what the model streams (its thoughts, its text) and a **Stop**. Its pictures appear as they come, each with its size and what it cost. A refusal says why in the model's words or the reason it gave (a safety stop: "The model didn't draw it (it stopped: IMAGE_SAFETY)"), and the turn's settings stay for the next try.
- **PIC-41** A session is a conversation for Gemini models: each turn sends the earlier turns (their prompts, references and the pictures made, with the model's thought signatures, as spec_056 keeps them), so "make the sky darker" changes the last picture. **Start from this** on any picture makes the next turn refine that one (it becomes the last picture the model sees, and a reference). The history sent is trimmed oldest first to the model's input limit, saying so; the pictures referenced by the next turn are never trimmed.
- **PIC-42** For Imagen models each turn stands alone (they have no conversation); **Try again** repeats a turn's settings, **Vary** repeats them with a new seed.
- **PIC-43** A session may change models between turns; a switch from Gemini to Imagen starts its turns afresh (said in the turn), and back to Gemini continues the conversation.
- **PIC-44** Sessions are listed per workspace (**History**: name, when, the last picture as its thumbnail), reopened, renamed and deleted; a session is named after its first prompt.

## 7. Pictures and the file tree

- **PIC-50** Every picture made is kept with its session (in the service's data for the workspace, not in the workspace's files) until the session is deleted.
- **PIC-51** **Save** writes a picture into the workspace: to the session's folder (the folder it was opened on, else `[images] folder`, default `pictures/`), named from the prompt and numbered (`city-skyline-1.png`, never overwriting); **Save as…** picks the folder and name in a picker of the workspace's folders. The file tree shows it at once, and it opens from there. The extension follows the picture's format. The workspace's file rules hold (a blocked or read-only path refuses, saying why). **Owner:** whether pictures are saved as they're made (§13).
- **PIC-52** A saved picture remembers where it came from: a sidecar note isn't written into the workspace; the session records the path, and the picture's turn shows **Saved as pictures/city-skyline-1.png** (a link that opens it in the editor).
- **PIC-53** Each picture's actions: **Save**, **Save as…**, **Open** (in the editor's image preview), **Use as reference**, **Start from this**, **Copy** (to the clipboard as a picture), **Add to chat** (saved first if it isn't, then mentioned as `@path` in the chat box), **Delete** (from the session).
- **PIC-54** From the chat: a picture an agent drew with `generate_image` gets **Open in Pictures**, which starts a session with that picture as the first result (its prompt and settings as the turn's), so it can be refined here.

## 8. What's sent

- **PIC-60** A Gemini turn is `generateContent` with the session's history and the new user turn: the prompt's text, then each reference as `inlineData` (its MIME type and data), each preceded by a short text naming its role ("Character reference 1:", "Object reference 2:", "Style reference:"), since the API has no role field; `generationConfig.responseModalities` `TEXT` and `IMAGE`; `imageConfig` with `aspectRatio`, `imageSize` (only values the catalog allows), and the person and format settings where allowed; `thinkingConfig.thinkingLevel` when chosen; `tools: [{googleSearch: {searchTypes: …}}]` when grounding is on.
- **PIC-61** An Imagen turn is `generateImages` with the prompt and `config`: `numberOfImages`, `aspectRatio`, `imageSize` (when the model has sizes), `negativePrompt`, `personGeneration`, `outputMimeType` and `outputCompressionQuality`, `seed`, `enhancePrompt`.
- **PIC-62** The service checks every request against the catalog before it's sent (a value the model doesn't take is refused with what it does take, never passed on), so the CLI and other clients get the panel's rules.
- **PIC-63** An organization's image models go through its service (MDL-95a): a Gemini-kind model as the gateway's `image_generation` tool, which carries `aspect_ratio` and `image_size` today (GW-04b); the panel's other Gemini settings (thinking, grounding, format, people) need the gateway to pass them (a GW item in blitz-enterprise's spec); until it does, those controls are hidden for organization models. Imagen through the organization needs the gateway to serve it; until then, organization Imagen models aren't offered.

## 9. Cost, privacy and safety

- **PIC-70** Before **Make**, the panel shows what the turn will cost, about: Imagen's price per picture times the count; Gemini's output tokens for the size (from the catalog) plus the input (the prompt, references and history, estimated), at the model's prices (MDL-96, MDL-97). After, the turn shows what it cost; it's in `/cost` and the usage ledger as purpose `image` (the session's ID as the run). The workspace's spending limits hold: a turn over the limit isn't sent.
- **PIC-71** The first time a workspace sends pictures to a model (references, or a conversation's history), Pictures says where they go (Google, or the organization's service) once, and remembers the answer for the workspace.
- **PIC-72** Pictures made by Google's models carry Google's invisible SynthID watermark; the panel says so in its help, and doesn't claim otherwise.
- **PIC-73** The model's refusals and safety stops are shown, never retried around; the person filter's default is the model's own.

## 10. Parity

- **PIC-80** **CLI**: `blitz picture "<prompt>"` with `--model`, `--ratio`, `--size`, `--count`, `--ref <path>[:character|object|style]` (repeatable), `--thinking`, `--search[=image]`, `--avoid`, `--people`, `--format`, `--seed`, and `--out <path>` (else saved as PIC-51 says); `--continue <session>` adds a turn to a session; `blitz picture list|show|delete`. It prints each picture's path and cost, and refuses what the model doesn't take, as PIC-62.
- **PIC-81** **The agent**: `generate_image` takes the same catalog: more references (to the model's limit), any of its ratios and sizes, and `count` for Imagen; its pictures are kept as a Pictures session too, so the user can open them in the panel (PIC-54).

## 11. Tests

The catalog's entries and family defaults (PIC-01–PIC-03); request validation for every field, model by model (PIC-62); the payloads for both kinds, references with roles, history and grounding (PIC-60, PIC-61), against a fake model as strict as the SDK; sessions: turns, history trimming, switching kinds, saving (names, numbering, file rules) (PIC-41–PIC-53); the organization's path through a fake gateway (PIC-63); cost estimates and the limit (PIC-70); the panel's visibility matrix (the controls each model shows, locks and hides) and its journeys in the fake service, with screenshots; the CLI's flags (PIC-80).

## 12. Phases

1. The catalog, `WorkspaceService`'s Pictures calls, Gemini image models through a personal key: the panel with every Gemini control, references from the computer, clipboard and file tree, multi-turn sessions, saving into the tree, the chat icon and `/picture` (PIC-01–PIC-53 for Gemini, PIC-60, PIC-62, PIC-70–PIC-73).
2. Imagen: count, negative prompt, people, seed, prompt enhancement, Try again and Vary (PIC-42, PIC-61).
3. The organization's models, and the gateway items they need (PIC-63); **Open in Pictures** from the chat and the agent's tool on the catalog (PIC-54, PIC-81).
4. `blitz picture` (PIC-80).

## 13. Open decisions (Owner)

1. **Where Pictures opens**: the middle panel from under the menu bar, as the editor's views (proposed), or a side panel beside the chat.
2. **Saving**: explicit **Save** with a per-session **Save each picture** switch (proposed), or every picture saved as it's made.
3. **The default folder**: `pictures/` (proposed), or beside the session's first reference.
4. **The default model**: the `[images] model`, else `gemini-nano-banana-2.1` when offered (proposed).
5. **Imagen's availability**: Imagen on the Gemini API needs a paid key, and `imagen-3.0-*` may be retired by Google; ship Imagen in phase 2 only for the models Google still serves when it's built.
6. **The pasted capability figures**: the reference limits per role, the 7 MB limit, 0.5K and the 1:8 ratios, the thinking levels per model and image-search grounding are taken as given; each is checked against Google's documentation as the catalog is written, and the catalog wins over this table where they differ.
