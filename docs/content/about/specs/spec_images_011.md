---
title: "011 · Images and documents"
weight: 11
---

*Images and documents* (`spec_images_011`)

| | |
|---|---|
| Status | Implemented. Reverse-engineered from `53f8c53` (2026-09-24) and kept with the code since; its paths and names checked 2026-09-27 (`//tools/specs`). |
| Source | `pkg/images/*.go`, `pkg/pdftext/pdftext.go`, `pkg/engine/tools/images.go`, `documents.go`, `pkg/engine/runtime/images.go`, `documents.go`, `openai_images.go`, `anthropic.go`, `contextparts.go`; `/attach`, `/paste` in `apps/cli/internal/tui`; `apps/desktop/web/src/attachments.ts` |
| Tests | `pkg/images/images_test.go`, `documents_test.go`, `pkg/engine/tools/images_test.go`, `documents_test.go`, `pkg/engine/runtime/images_test.go`, `contextparts_test.go`, `apps/cli/internal/tui/attach_test.go`, `apps/cli/images_test.go`, `apps/desktop/web/src/attachments.test.ts` |
| Depends on | [spec_filetools_006](spec_filetools_006.md) (sandboxed reads) |

## 1. Purpose

Users and the agent can put pictures and PDFs in front of models. Images are validated and normalised once, stored content-addressed outside session files, referenced in history by a short URI, and expanded to bytes only in outgoing requests. A PDF goes to a model that reads PDFs as it is, figures and all, and to one that doesn't as its text (§6).

## 2. Sources

- **IMG-01** `@path` or `@"path with spaces"` mentions in a prompt (at the start or after whitespace, so e-mail addresses don't count; trailing `.,;:!?)` trimmed); only image extensions (`.png .jpg .jpeg .gif .webp`) are taken, de-duplicated in order; other `@paths` are the workspace's: their content goes with the prompt ([spec_workspace_018](spec_workspace_018.md) WS-21a).
- **IMG-02** `--image PATH` (repeatable; failure is a usage error), `/attach <path>` and `/attach clear`, `/paste` (clipboard: `osascript` on macOS, `wl-paste` or `xclip` on Linux, PowerShell on Windows; `ErrNoClipboardImage` otherwise). Queued attachments go with the next real prompt (not `/btw`).
- **IMG-03** The `view_image` tool (args `path`): loads a workspace image and returns `path, image_uri, mime, width, height, resized, note`; the picture follows the tool result in the next request.
- **IMG-04** Files are read **through the workspace sandbox**: blocked and out-of-workspace paths are refused. Identical images (same SHA-256) are attached once. The audit log records path and hash, not the picture.
- **IMG-05** Everything is disabled by `[images] enabled = false` (`ErrImagesDisabled`); `view_image` is then not registered.

## 3. Preparation

- **IMG-10** Input limit `images.max_input_mb` (20 MB). The format is detected from bytes, never the name; only PNG, JPEG, GIF, WebP.
- **IMG-11** Decompression bombs are rejected from the header before decoding: more than 100 M pixels is refused. The image is then fully decoded (providers reject corrupt images).
- **IMG-12** If the long edge exceeds `max_dimension` (1568) or the encoded size exceeds 3.75 MB, the image is scaled (Catmull-Rom, aspect kept) and re-encoded: PNG for PNG/GIF sources if it fits, else JPEG at quality 85 → 70 → 50 with transparency flattened onto white; still too big is an error.
- **IMG-13** Metadata: display name (base name), MIME, dimensions, original bytes, resized flag, SHA-256.

## 4. Storage and references

- **IMG-20** Store: `~/.blitz/images` (0700), files named by SHA-256 (0600). Reuse refreshes an image's age; images unused for `retain_days` (30) are pruned when a workspace opens (0 = keep).
- **IMG-21** History and session files hold a `FileData` part with URI `blitz-image:<sha256>`. Before each model call the references are expanded to inline bytes on a **copy** of the request; an image belonging to a tool result is placed right after it. A missing stored image is replaced by a placeholder.
- **IMG-22** The transcript records attached image names (`[images: a.png]`), not data.

## 6. Documents (PDFs)

- **IMG-50** A PDF is attached as an image is: `@paper.pdf` (IMG-01 takes `.pdf` too), `--image`, `/attach`, the desktop's attach button, paste and drop, and `view_document` (spec_filetools_006 FS-73). `Prepare` knows one by its `%PDF-` header (within the first kilobyte), takes up to 32 MB (`MaxDocumentBytes`, Anthropic's request limit) whatever `max_input_mb` says, refuses one it can't read or that's password-protected, and keeps it as it is with its page count (`Image.Pages`; `Summary`: "paper.pdf 12 pages, 2.1 MB"). The store keeps it as `<sha256>.pdf`; the audit log records its pages.
- **IMG-51** Which models read PDFs: `runtime.SupportsDocuments(provider, model)`: Gemini's, and Claude's (`claude-*`) through Anthropic's API or Vertex AI. Others (OpenAI, Ollama, Azure, Claude on Bedrock) get the text.
- **IMG-52** The expansion (IMG-21) decides per request, by the model about to answer (`documentPolicy`): a model that reads PDFs gets the bytes while the PDF is within its API's limits (Gemini 14 MB and 1,000 pages, Anthropic 22 MB and 100 pages); otherwise, and for a fallback chain unless every model in it reads PDFs, the reference becomes `<document name="paper.pdf" pages="12">` with a note and the text (`pkg/pdftext`, each page headed `--- Page N ---`, cut at 300,000 characters). Switching models mid-session is therefore safe. The text is read once and kept beside the PDF (`<sha256>.txt`, pruned with it); a PDF whose text can't be read becomes a note.
- **IMG-53** Anthropic: a PDF is a base64 `document` block, inside the tool result after `view_document`. OpenAI/Ollama: only `image/*` inline data becomes `input_image`; anything else is a note ("[application/pdf attachment omitted: this model can't read it]"), never a request the API rejects.
- **IMG-54** `/context` counts PDFs apart from images, at 1,500 tokens a page ("PDF documents"), attached or from `view_document`.
- **IMG-55** An `@paper.pdf` mention attaches the PDF; the prompt's file mentions leave PDFs (and pictures) out of the `<mentioned-files>` block. The desktop's chips show a PDF's pages and size and the PDF icon, and its sent prompt shows the PDF's name; PDFs up to 30 MB upload (what fits a service request); the service's `Image` carries `pages` (spec_service_021).

## 5. Provider handling
- **IMG-30** Gemini: native inline data. Anthropic: base64 image blocks, or inside the tool result for `view_image` ([spec_models_015](spec_models_015.md) MDL-23). OpenAI/Ollama: marker substitution into `input_image` items (MDL-42). For Ollama a vision model is required.
- **IMG-31** Agents are told they can see images when support is on ([spec_engine_016](spec_engine_016.md) ENG-03).
