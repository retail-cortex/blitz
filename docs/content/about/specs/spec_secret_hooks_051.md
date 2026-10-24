---
title: "051 · Secrets hook"
weight: 51
---

*A pre-commit hook that stops commits adding API keys and other secrets, put in by the harness setup* (`spec_secret_hooks_051`)

| | |
|---|---|
| Status | **Built** (v0.8.4, the owner's request: "when the harness is created, and a git repository is created, we should also add githooks to ensure API keys are not committed") |
| Source | `pkg/secretscan` (`scan.go`: the scan; `hook.go`: the hook, `Install`, `Staged`), `apps/cli/security.go` (`blitz security scan-secrets`, `blitz security install-hook`), `pkg/engine/changes.go` (`GitInit`, `InstallSecretHook`), `pkg/engine/tools/secrethook.go` (`install_secret_hook`), `WorkspaceService.InstallSecretHook` (`apps/service/internal/server/workspace.go`), the Changes menu (`apps/desktop/web/src/MenuBar.tsx`, `Workspace.tsx`), `pkg/engine/agents/builtin/project-setup.md`, `harness-update.md` |
| Tests | `pkg/secretscan/scan_test.go` (`TestScan`, `TestInstall`, `TestHookStopsACommit`, `TestStaged`), `apps/cli/security_test.go` (`TestScanSecretsAndInstallHook`), `pkg/engine/changes_test.go` (`TestGitInit`), `pkg/engine/tools/jobs_test.go` (`TestInstallSecretHookTool`), `apps/service/internal/server/git_test.go` (`TestInitGitRepo`) |
| Depends on | [spec_harness_learning_048](spec_harness_learning_048.md) (the setup), [spec_files_029](spec_files_029.md) (FIL-59, git in the window), [spec_secrets_039](spec_secrets_039.md) (where keys belong: the keychain) |

## 1. Purpose

Agents write configuration, scripts and `.env` files, and Blitz adds new files to git at the end of a turn (FIL-60). A key pasted into a file, or written by an agent that was given one, can be committed and pushed before anyone sees it. Blitz keeps its own keys in the keychain (spec_secrets_039); a repository it sets up should refuse to commit anyone's.

## 2. The scan

- **SH-01** `secretscan.Scan(name, data)` finds, a line at a time: known key formats anywhere (a private key's `BEGIN … PRIVATE KEY` line, AWS access keys, Anthropic and OpenAI keys, Google API keys, GitHub and GitLab tokens, Slack tokens, Stripe live keys, Hugging Face tokens); and in env and config files (`.env`, `.env.*` but not `.example`, `.sample` or `.template`; `.toml`, `.yaml`, `.ini`, `.json`, `.properties` and the like; `.npmrc`, `.pypirc`, `credentials`) a value of 16 characters or more, with letters and digits and high entropy, assigned to a key-like name (password, secret, token, API or access key, credential), unless it's a placeholder (`${…}`, `<…>`, `your-…`, `changeme`, `keychain:…`, `env:…`, `op://…`). A line with `blitz:allow-secret` is skipped; binary files and files over 1 MB too. A finding names the file, the line and the kind, with the value masked (its first four characters).
- `blitz security scan-secrets --staged` scans what the commit being made adds (each added, copied, changed or renamed file as staged, `git show :<path>`), or `blitz security scan-secrets <file>…` the files named; it lists the findings on stderr, says how to deal with them (the keychain or an ignored `.env`, the marker for a false alarm, `git commit --no-verify`) and exits 1.

## 3. The hook

- **SH-02** `.githooks/pre-commit` (versioned, so everyone who clones the repository has it; `git config core.hooksPath .githooks` turns it on in a clone) runs `blitz security scan-secrets --staged` when `blitz` is on PATH, else searches the lines being added (`git diff --cached -U0`) for the same key formats with `grep -E`, printing only their first four characters. `secretscan.Install` writes it (0755) at the repository's top and sets `core.hooksPath` in the repository's own config, and reports `installed`, `present` (it's there already) or `other_hooks`: a repository with `core.hooksPath` set elsewhere, husky, the pre-commit framework, lefthook, a `.git/hooks/pre-commit`, or a `.githooks/pre-commit` of its own is left alone, and the detail says which (add `blitz security scan-secrets --staged` to them).
- It goes in when the harness is set up: project-setup calls `install_secret_hook` once the workspace is a repository (made then or already), asked about as a write showing the hook; when the window makes a repository (`InitGitRepo` → `GitInit`; a failure is logged, the init isn't); and with `blitz security install-hook [dir]`.

## 4. For an existing repository

- **SH-03** The Changes menu's **Protect against committing keys** (`WorkspaceService.InstallSecretHook`), whose snack says what happened (and, for `other_hooks`, which hooks); harness-update offers it to a harness without it (HE-52).

## 5. Not in scope

- Scanning history already committed (`gitleaks detect`, `trufflehog`): a key that was committed is to be revoked, whatever the hook says.
- A server-side check (push protection): the hosting service's.
