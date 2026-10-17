---
title: "040 · Workspace manifest"
weight: 40
---

*What a workspace's .agents folder holds, each kind listed and edited in the desktop app* (`spec_manifest_040`)

| | |
|---|---|
| Status | **In progress** since v0.7.1 (the owner's request, 2026-10-17). Done: the menu, the drawer, agents, secrets, AI drafts, secrets completion (MAN-01–MAN-08). Next: skills, rules, guardrails, evals, workflows, each with its own spec where it adds behaviour. |
| Source | `apps/desktop/web/src/manifest/` (`ManifestDrawer.tsx`, `Draft.tsx`), `MenuBar.tsx`, `simpleMode.ts`, `ui/controls.tsx` (`Tabs`), `SecretSuggest.tsx`, `secretComplete.ts`, `files/secretCompletion.ts`, `pkg/engine/draft.go`, `apps/service/internal/server/manifest.go` |
| Tests | `pkg/engine/draft_test.go`, `apps/service/internal/server/mcp_test.go` (`TestDraftManifestOverTheAPI`), `apps/desktop/web/src/secretComplete.test.ts`, `apps/desktop/web/src/simpleMode.test.ts` |

## 1. Purpose

A workspace's `.agents` folder holds what shapes its agents: agents, skills, workflows, evals, rules, guardrails, workers. The desktop app shows each kind and lets every one be created, read, changed and deleted, without opening the files.

## 2. Requirements

- **MAN-01** The menu bar is **Workspaces**, **Changes**, **Manifest** and **Help** (`menuIds`; Manifest with advanced settings, DSK-60a). It replaces the Agents and Workers menus. **Manifest** lists Agents, Skills, Workflows, Evals, Rules, Guardrails and Secrets, each opening the manifest drawer on it; then the workers, as the Workers menu had them (**Add worker**, **Background runs**, the runs going now with Stop, each worker with its actions); then **Reload agent files** (DSK-91d). A worker that failed puts a red dot on Manifest, and runs going or waiting a count (DSK-80c).
- **MAN-02** The manifest drawer (`ManifestDrawer`) lists the kinds at its left, as Settings does its sections, and shows the chosen kind's list and editor. **Agents** is the agent editor with the workspace's agents beside the form (`AgentEditor` `list`; DSK-91b). **Secrets** is the workspace's own secrets (spec_secrets_039 SEC-08), each shown as `{secrets:workspace:key}` with its stored name. A kind not built yet says what it is and where its files go.
- **MAN-03** Workers keep their own drawer (DSK-91), opened from the menu's worker entries.
- **MAN-04** `Tabs` (`ui/controls.tsx`): tabs over one panel, the chosen one underlined, ← → Home End moving between them; for the editors that come.
- **MAN-05** **Draft with AI** (`DraftButton`, **Draft**, or **Redraft** when there's text): a request (optional: empty improves what's there), then the model's draft with what changed, **Use** putting it in the form unsaved, **Back** asking again. `WorkspaceService.DraftManifest` (kind, name, description, the text now, the request, a script's language) has the suggestions model (else the reviewer's, else the main one) write it from the kind's brief (`agent`, `skill`, `script`, `rule`, `guardrail`, `eval`, `workflow_step`), secrets redacted from what it's sent; nothing is saved. An unknown kind or nothing to go on is `INVALID_ARGUMENT`; a model that fails is a `problem`, not an error. An agent's instructions have it.
- **MAN-06** `{secrets:` completes as it's typed (spec_secrets_039 SEC-09) in the composer, an agent's instructions, an MCP server's environment and header values (global names only), the settings file and the Files editor: `global:` and `workspace:` first (`workspace:` only with a workspace), then the names in either scope; a name goes in scoped. Escape closes the list (not the drawer it's in). The text boxes (`SuggestTextarea`) warn of a named secret that isn't there.
- **MAN-07** The names come from `ListSecrets`, kept for 30 s and forgotten when a secret is added or deleted (`secretNames`, `forgetSecretNames`); values are never fetched.
- **MAN-08** The workspace's settings no longer hold its secrets: they're in the manifest.
