---
title: "legal"
weight: 11
---

The license texts every program shows. Source: [`pkg/legal`](https://github.com/retail-cortex/blitz/tree/main/pkg/legal).

`pkg/legal` embeds the repository's `LICENSE` (Apache 2.0), `NOTICE` (Blitz's copyright and Code Puppy's MIT notice) and `THIRD_PARTY_NOTICES` (the licenses of the software the programs include). Bazel copies them in when the package is built, so they can't drift from the repository's.

The CLI shows them with `blitz license` and `/license`, the service with `blitzd --license`, and the desktop app in **Settings › About**.

## API

| Name | |
|---|---|
| `License`, `Notice`, `ThirdParty` | The texts |
| `Summary(command)` | A short notice pointing at the full texts |

## Used by

apps/cli/internal/tui, apps/service, apps/desktop.

Spec: [release readiness](../about/specs/spec_release_readiness_030.md).
