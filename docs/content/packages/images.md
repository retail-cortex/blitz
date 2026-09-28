---
title: "images"
weight: 8
---

Preparing pictures for vision models. Source: [`pkg/images`](https://github.com/retail-cortex/blitz/tree/main/pkg/images).

`pkg/images` normalizes an image once (format checked, oversized pictures scaled down and re-encoded, decompression bombs refused from their headers), writes it to a content-addressed store, and refers to it in history by a short `blitz-image:` URI. Session files hold that reference instead of megabytes of base64; `Expand` swaps the bytes back in just before each model request.

## API

| Name | |
|---|---|
| `Prepare(name, data, Options)` | Check, scale and re-encode an image |
| `Store`, `OpenStore` | `~/.blitz/images`, by SHA-256, pruned after `retain_days` |
| `Expand`, `Part`, `HasRefs` | References in and out of model requests |
| `Mentions`, `IsImagePath` | `@image.png` mentions in a prompt |
| `ReadClipboard` | `/paste` |

## Used by

apps/cli, apps/cli/internal/tui, apps/service/internal/server, pkg/api, pkg/client, pkg/engine, pkg/engine/runtime, pkg/engine/tools.

Spec: [images](../about/specs/spec_images_011.md).
