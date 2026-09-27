---
description: Review the uncommitted changes (or the given target) for security vulnerabilities
argument-hint: "[branch | files]"
mode: plan
---
Do a security review of code changes. Target: $ARGUMENTS

If no target is given, review the working tree's uncommitted changes (`git diff HEAD`, plus untracked files); a branch means what it adds over the default branch; files mean those files.

Look for exploitable problems: injection (SQL, shell, template, path traversal), missing or broken authentication and authorization, secrets in code or logs, unsafe deserialization, SSRF, insecure defaults, weak cryptography, race conditions with security impact, and untrusted input reaching dangerous sinks. For each finding give the file and line, how an attacker would exploit it, the impact, and the fix. Verify each by tracing the data flow before reporting it; don't report theoretical issues without a path from input. End with a short summary by severity. Don't change any files.
