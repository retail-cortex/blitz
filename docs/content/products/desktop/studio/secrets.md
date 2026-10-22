---
title: Secrets
weight: 90
---

**Studio › Secrets** keeps tokens and passwords for this workspace in your system's keychain, so they never land in a prompt, a file, a conversation or a log. You name a secret where it's needed, such as `{secrets:staging_token}` in a workflow's API call, and Blitz puts the value in only as it's used.

## Open it

Choose **Studio › Secrets**. Studio's menu shows once **Show advanced** is on, in the Settings title bar.

![Studio › Secrets: add a secret by name and value](../../../../images/desktop/studio-secrets.png)

## Add a secret

1. Type its **Name**, such as `github_token`: letters, digits, `_`, `.` and `-`, up to 64.
2. Paste its **Value**.
3. Choose **Add**.

The list shows each secret's name and when it was set, never its value. Replace a value without seeing the old one, or **Delete** the secret; prompts and settings that name it keep the name as written.

## Use it

Write `{secrets:name}` in an agent's instructions, a workflow, or an MCP server's settings. This workspace's secret is used first, then a global one of the same name. The value goes in only when it's used: the transcript, the audit log and the file keep the placeholder, and a value a model echoes back is shown masked.

Global secrets, for every workspace, are in **Settings › Secrets**.

More: [Workflows](../../../guide/workflows.md) (secrets in API calls) and [Safety](../../../guide/safety.md#secrets).
