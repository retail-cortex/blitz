---
title: Images
weight: 80
---

Spec: [images](../about/specs/spec_images_011.md).

Mention an image in a prompt (`what's wrong with @screenshots/login.png?`, or `@"with spaces.png"`), queue one with `/attach <path>`, paste a screenshot with `/paste`, or pass `--image` on the command line. The model also has a `view_image` tool for images it finds in the workspace. In the desktop app, attach or paste into the chat.

PNG, JPEG, GIF and WebP work with Gemini, Anthropic and OpenAI-compatible models; for Ollama, pick a vision model.

- Files are read through the workspace sandbox: blocked paths and anything outside the workspace are refused.
- Pictures larger than `[images] max_dimension` (1568 px) or 3.75 MB are scaled down and re-encoded. Headers are checked before decoding, so oversized "decompression bomb" files are rejected.
- Session files store a short reference, not the image. The picture is kept once in `~/.blitz/images` (owner-only, named by its SHA-256) and deleted after `retain_days` (30) unused. The audit log records the path and hash.
- `/paste` uses `osascript` on macOS, `wl-paste` or `xclip` on Linux, and PowerShell on Windows.
