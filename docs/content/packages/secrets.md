---
title: "secrets"
weight: 6
---

API keys in the OS keychain. Source: [`pkg/secrets`](https://github.com/retail-cortex/blitz/tree/main/pkg/secrets).

`pkg/secrets` keeps API keys out of settings files: in the macOS Keychain, in the Secret Service on Linux (GNOME Keyring, KWallet), or, where neither is available, in an owner-only file beside the settings. A settings file refers to a stored key as `keychain:<name>`, and `pkg/config` resolves the reference when it loads.

## API

| Name | |
|---|---|
| `Store`, `Default(dir)`, `SetDefault` | The platform's store |
| `FileStore`, `Memory` | The fallback file; an in-memory store for tests |
| `Ref`, `ParseRef`, `RefPrefix` | `keychain:` references |

## Used by

pkg/config.

Spec: [config](../about/specs/spec_config_002.md).
