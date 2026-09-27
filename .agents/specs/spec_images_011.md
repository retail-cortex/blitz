# spec_images_011 — Images

| | |
|---|---|
| Status | Implemented (reverse-engineered from `53f8c53`) |
| Source | `pkg/images/*.go`, `pkg/engine/tools/images.go`, `pkg/engine/runtime/images.go`, `openai_images.go`; `/attach`, `/paste` in `apps/cli/internal/tui` |
| Tests | `pkg/images/images_test.go`, `pkg/engine/tools/images_test.go`, `pkg/engine/runtime/images_test.go`, `apps/cli/internal/tui/attach_test.go`, `apps/cli/images_test.go` |
| Depends on | [spec_filetools_006](spec_filetools_006.md) (sandboxed reads) |

## 1. Purpose

Users and the agent can put pictures in front of vision models. Images are validated and normalised once, stored content-addressed outside session files, referenced in history by a short URI, and expanded to bytes only in outgoing requests.

## 2. Sources

- **IMG-01** `@path` or `@"path with spaces"` mentions in a prompt (at the start or after whitespace, so e-mail addresses don't count; trailing `.,;:!?)` trimmed); only image extensions (`.png .jpg .jpeg .gif .webp`) are taken, de-duplicated in order; other `@paths` are left for the model.
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

## 5. Provider handling
- **IMG-30** Gemini: native inline data. Anthropic: base64 image blocks, or inside the tool result for `view_image` ([spec_models_015](spec_models_015.md) MDL-23). OpenAI/Ollama: marker substitution into `input_image` items (MDL-42). For Ollama a vision model is required.
- **IMG-31** Agents are told they can see images when support is on ([spec_engine_016](spec_engine_016.md) ENG-03).
