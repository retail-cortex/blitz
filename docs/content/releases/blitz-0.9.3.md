---
title: Blitz 0.9.3
weight: 10
---

**Ask Blitz for a picture, and get one.** Set an image model, and whatever model you're talking to draws posters, illustrations and logos into your workspace, and changes the pictures you already have. And Blitz no longer treats every request as a coding task.

### Get started

1. Update Blitz (it updates itself) or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.9.3).
2. In the desktop app, open a workspace's settings and set **Image model** under **Agent and model** to `gemini/gemini-3.1-flash-image`. In the terminal, add `[images] model = "gemini/gemini-3.1-flash-image"` to the workspace's settings.
3. Ask: "draw a poster for Friday's study group". The picture shows in the conversation and is saved in the workspace.

### What's new

**Pictures, on request.** The agent draws with Gemini's image models (3.1 Flash Image, 3.1 Flash Lite Image or 3 Pro Image) and saves the picture where you say. Mention a picture to change it or match its style ("make @logo.png blue"). It sees what it drew, so "make the sky darker" works. It asks before sending anything to Google, and each picture's cost (about 7 cents with 3.1 Flash Image) shows in `/cost`. With your organization's service, choose one of its image models: pictures go through the service, with no Gemini key of your own. [How it works](../guide/images.md#pictures).

### Fixes

- Asked for a picture, a document or an answer, the Blitz agent first listed your workspace's files and read them, as if every task were about code. It now looks at your files only when the task needs them.
- In the visual Markdown editor, every tab shared one place: switching tabs lost your cursor and scroll, so typing after a switch went nowhere ([#96](https://github.com/retail-cortex/blitz/issues/96)). Each tab now comes back where you left it, ready to type.
- An organization's image model chosen as a conversation's model failed with "response included no output items". The pictures it draws now show in the conversation (with an updated organization service).
- Gemini 4 Argon's costs were estimated at a placeholder price. They now use Google's published price ($2 in, $10 out a million tokens, doubling on December 1).
- The CLI showed each tool call twice with Gemini, and `--output-format stream-json` wrote it twice. Each call shows once now, as in the desktop app.
