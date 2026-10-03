---
title: Attachments, PDFs and audio
weight: 80
---

Specs: [attachments](../about/specs/spec_images_011.md), [file tools](../about/specs/spec_filetools_006.md) (`export_pdf`, `generate_audio`), [models](../about/specs/spec_models_015.md) (speech).

## What you can attach

Images, PDFs, text files, audio and video, as far as the model takes them:

| | Images | PDFs | Text | Audio and video |
|---|---|---|---|---|
| Gemini | PNG, JPEG, WebP, GIF, BMP, HEIC, HEIF | read as they are | yes | MP3, WAV, AAC, FLAC, Ogg, Opus, M4A; MP4, MOV, MPEG, WebM, AVI, WMV, FLV, 3GP |
| Claude | PNG, JPEG, WebP, GIF, BMP | read as they are | yes | no |
| OpenAI, Ollama and others | PNG, JPEG, WebP, GIF, BMP | as their text | yes | no |

- **Only what the model takes.** The desktop app's attach button offers only the types the current agent's model takes, and paste, drop, `@mentions` and `/attach` refuse others with the reason ("claude-sonnet-5 can't take talk.mov"). Switch agent or model, and the list follows.
- **Text files** (notes, CSV, JSON, code; up to 1 MB) go to every model as text.
- **GIF and BMP** become PNG, which every provider takes.
- **Audio and video** go to Gemini. Up to 14 MB goes in the request; larger files, up to 2 GB from the workspace, go through Gemini's Files API, where Google keeps them for 48 hours, and Blitz uploads them again after that. With Gemini on Vertex AI there's no Files API, so 14 MB is the limit. A file dropped into the desktop app can be up to 30 MB; put a larger one in the workspace and attach it from there (`@talk.mp4`). The agent opens recordings and videos in the workspace with `view_media`.
- **Switching models** keeps a conversation's files: a model that can't take one gets a note saying so (PDFs and text, their text).
- The desktop app plays sound, and MP4, MOV and WebM video, when you open one; other video opens in the system's player.

## Images

Mention an image in a prompt (`what's wrong with @screenshots/login.png?`, or `@"with spaces.png"`), queue one with `/attach <path>`, paste a screenshot with `/paste`, or pass `--image` on the command line. The model also has a `view_image` tool for images it finds in the workspace. In the desktop app, attach or paste into the chat.

For Ollama, pick a vision model.

- Files are read through the workspace sandbox: blocked paths and anything outside the workspace are refused.
- Pictures larger than `[images] max_dimension` (1568 px) or 3.75 MB are scaled down and re-encoded. Headers are checked before decoding, so oversized "decompression bomb" files are rejected.
- Session files store a short reference, not the image. The picture is kept once in `~/.blitz/images` (owner-only, named by its SHA-256) and deleted after `retain_days` (30) unused. The audit log records the path and hash.
- `/paste` uses `osascript` on macOS, `wl-paste` or `xclip` on Linux, and PowerShell on Windows.

## PDFs in

PDFs attach the way images do: `summarise @papers/attention.pdf`, `/attach`, `--image`, or attach, paste or drop one in the desktop app. The agent reads one it finds with `view_document`, and `read_file` on a PDF gives its text, page by page.

- **Gemini and Claude read the PDF itself**, figures, tables and equations included, while it's within their API's limits (Gemini: 14 MB and 1,000 pages; Claude: 22 MB and 100 pages).
- **Other models get its text**, taken from the PDF and marked as such: OpenAI-compatible models, Ollama, Claude on Bedrock, and any PDF over those limits. A scanned PDF has no text to give; use a model that reads PDFs.
- Up to 32 MB a PDF. It's kept once in `~/.blitz/images`, like an image, with its text beside it once read. Switching models mid-session is safe: each request gives the PDF the way that model takes it.
- `/context` counts PDFs apart from images, about 1,500 tokens a page.

## PDFs out

`export_pdf` typesets a Markdown file as a PDF beside it (`notes/week1.md` → `notes/week1.pdf`, or `output`), in pure Go, so it works in the terminal, in workers and in the service. It asks before writing, like any file the agent creates, and `/undo` removes it. Ask for a PDF of something that isn't a file ("a summary of our conversation as a PDF") and only the PDF is written: the agent gives the tool the Markdown itself.

- Headings become the PDF's outline; lists, task lists, quotes, tables, code (highlighted), rules and page numbers are laid out; accents, Greek, Cyrillic and maths symbols print.
- Images the file links in the workspace are included; web images and raw HTML aren't. A Mermaid diagram prints as its code: export from the desktop app to draw it.
- Paper is A4 or Letter: `[pdf] page_size = "a4"`, `"letter"`, or `"auto"` (the default, from your locale).

In the desktop app, **Export as PDF** in a Markdown file's bar prints the preview exactly as it shows, diagrams included, with the system's own web engine. No browser needed.

## Audio

`generate_audio` reads text aloud into a sound file, for an audio overview of your notes or a two-host conversation about a paper. Set a speech model first:

```toml
[audio]
model = "gemini/gemini-2.5-flash-preview-tts"   # or "openai/gpt-4o-mini-tts"
voice = "Kore"                                   # optional
speakers = [{ name = "Ana", voice = "Kore" }, { name = "Ben", voice = "Puck" }]   # for two hosts
```

In the desktop app it's **Speech model** in a workspace's settings, under **Agent and model**. Until one is set, agents don't have the tool.

- One voice, or two: with `speakers`, each line of the script starts with a speaker's name and a colon (`Ana: So what is attention?`).
- Gemini makes `.wav` files; OpenAI makes `.mp3`. Markdown formatting isn't read out. A call takes at most `max_chars` (9,000, about ten minutes); split a longer script into parts.
- It asks first (it sends the text to the provider), and its cost goes in the session's `/cost`. With `sandbox.allow_network = false` it's refused.
- The desktop app plays `.wav`, `.mp3`, `.m4a`, `.ogg`, `.opus` and `.flac` files when you open them.
