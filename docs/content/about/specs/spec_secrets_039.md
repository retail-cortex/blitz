---
title: "039 · Secrets"
weight: 39
---

*The user's secrets, kept in the system's keychain and named in prompts and settings as `{secrets:key}`* (`spec_secrets_039`)

| | |
|---|---|
| Status | **Implemented** since v0.7.1 (the owner's request, 2026-10-17). |
| Source | `pkg/secrets` (`vault.go`, `wincred_windows.go`, `secrets.go`), `pkg/config/providers.go` (`Vault`, `SecretScope`), `pkg/redact` (`Remember`, `Mask`), `pkg/engine/runtime/engine.go` (`WithSecrets`, `beforeModel`, `afterModel`), `pkg/engine/workspace.go` (`expandSecrets`, `mcpWithSecrets`), `apps/service/internal/server/secrets.go`, `apps/desktop/web/src/SecretsEditor.tsx` |
| Tests | `pkg/secrets/vault_test.go`, `pkg/redact/remember_test.go`, `pkg/engine/runtime/secrets_test.go`, `pkg/engine/secrets_test.go`, `apps/service/internal/server/secrets_test.go`, `apps/desktop/web/src/SecretsEditor.test.ts` |

## 1. Purpose

Tokens and passwords a workspace needs (an MCP server's token, an API a prompt talks about) shouldn't be pasted into prompts, settings files or conversations. A secret is stored once, in the system's keychain, and named where it's needed.

## 2. Requirements

- **SEC-01** A secret has a key (letters, digits, `_`, `.` and `-`, up to 64) and a scope: `global`, or a workspace's own (`config.SecretScope`: its settings folder's name, `<name>-<hash8>`). Its value is stored in the OS store (spec_service_021's `secrets.Store`) under `urn:blitz:<scope>:secrets:<key>` (`secrets.URN`).
- **SEC-02** The OS store is the macOS Keychain, the Secret Service on Linux, and *since v0.7.1* the Windows Credential Manager (generic credentials `dev.blitz/<name>`, for the user only); elsewhere an owner-only file. On Windows a name not yet in the Credential Manager is read from the file earlier versions kept, and moved over.
- **SEC-03** Secrets are listed by name from an index beside the settings, `secret-names.toml` (key and when it was set; never a value), so every store can list them (`secrets.Vault`).
- **SEC-04** A secret is named in a prompt, an agent's or the workspace's instructions, and an MCP server's `env` and `headers` values, with its scope or without: `{secrets:global:key}` is always the global secret, `{secrets:workspace:key}` only the workspace's own (no fallback), and `{secrets:key}` the workspace's own, else the global one (`Vault.Resolve`). A placeholder whose secret isn't there stays as written.
- **SEC-05** Values go in only as the model is called: the system instruction in the agent's `InstructionProvider`, the messages in copies made in `beforeModel`. The session, the transcript, the hooks and the audit log keep `{secrets:key}`. MCP values are put in when the workspace builds its tools and when its MCP servers reload (MCP-45).
- **SEC-06** Each value used is remembered by the redactor (`redact.Remember`), so logs, telemetry and the audit log mask it, and an answer that echoes it shows it masked (`afterModel`).
- **SEC-07** `ConfigService.ListSecrets`, `SetSecret` and `DeleteSecret`, each for a scope (an empty workspace for the global one): a list gives names, when each was set and where values are kept, never a value; a bad key is `INVALID_ARGUMENT`.
- **SEC-08** The desktop app: **Settings › Library › Secrets** (advanced, after Providers & keys) holds the global secrets only, and **Studio › Secrets** a workspace's own ([spec_studio_040](spec_studio_040.md) STU-02), both through `SecretsEditor`: each secret shown as the name to use, `{secrets:global:key}` or `{secrets:workspace:key}`, with where it's stored (`ListSecretsResponse.prefix` and the key: `urn:blitz:global:secrets:key`, `urn:blitz:<scope>:secrets:key`) and when it was set; **Replace** (a new value; the old one is never shown) and **Delete** (asked first); a name and a value to add one.
- **SEC-09** `{secrets:` completes as it's typed wherever a secret can be named (STU-06), and a text box warns of a named secret that isn't there.
