---
title: Security log
weight: 25
---

Security problems found in Blitz, what they allowed, and how they were fixed, newest first, with the known gaps still open. Each fix has a test that fails without it. To report a problem, don't open a public issue: use the repository's **Security** tab (**Report a vulnerability**), as [contributing](https://github.com/retail-cortex/blitz/blob/main/docs/CONTRIBUTING.md) says.

The threat model is in [sandboxing and trust](../architecture/security.md): above all, text the agent reads (a file, a web page, an issue) steering the commands it runs. So a flaw that lets one of those commands escape the sandbox, reach Blitz's credentials, or get Blitz itself to act outside the sandbox counts, even though it needs a prompt injection first.

| ID | Severity | Status | What |
|---|---|---|---|
| [SEC-2026-08](#sec-2026-08) | High | Fixed 2026-10-01 | The sweep of stale ADC copies followed a link planted from the sandbox |
| [SEC-2026-07](#sec-2026-07) | Medium | Fixed 2026-10-01 | On a stock Ubuntu 24.04, Linux's sandbox was off and commands ran unsandboxed |
| [SEC-2026-06](#sec-2026-06) | Medium | Fixed 2026-10-01 | Commands the model ran could read Blitz's own sign-ins |
| [SEC-2026-05](#sec-2026-05) | Medium | Fixed 2026-09-30 | GO-2026-5764: a crafted AWS event stream could crash Bedrock calls |
| [SEC-2026-04](#sec-2026-04) | Low | Fixed 2026-09-30 | A turn's time limit or cancellation couldn't stop a sandboxed command's set-up |
| [SEC-2026-03](#sec-2026-03) | Low | Fixed 2026-09-30 | Checking an MCP server's command left a credential copy until Blitz exited |
| [SEC-2026-02](#sec-2026-02) | Low | Fixed 2026-09-30 | A cancelled browser action could still reach the page |
| [SEC-2026-01](#sec-2026-01) | Low | Fixed 2026-09-30 | Undo snapshots checked a symlink's size, not its target's |

## Fixed

### SEC-2026-08

**The sweep of stale ADC copies followed a link planted from the sandbox.** High. Introduced in `4e9103a` (2026-09-30); fixed in `799b4b8` (2026-10-01). Found by a security review.

With `sandbox.share_adc` on and a provider signed in with Google's ADC, each command got a copy of the credentials in `~/.cache/blitz/credentials`, and Blitz swept copies over a day old when a workspace opened. `~/.cache` is writable inside the sandbox, so a prompt-injected command could replace that directory with a link to, say, the home directory: the next sweep, outside the sandbox, would follow it and delete everything there older than a day, and copies would be written through the link.

The copies now live in `~/.blitz/run/credentials`, beside the service's socket, which sandboxed commands can read but not write. The directory must be a real one only its owner can enter, or the command doesn't start; the sweep doesn't follow a link where it should be, and removes only its own `run-*` directories.

### SEC-2026-07

**On a stock Ubuntu 24.04, Linux's sandbox was off and commands ran unsandboxed.** Medium. Fixed in `0764340`, `929c6d6` and `904a46a` (2026-10-01).

Ubuntu 24.04 restricts the unprivileged user namespaces bubblewrap needs, and with `sandbox.shell = "auto"`, the default, Blitz then ran the agent's commands without the sandbox, saying so only in `blitz doctor` and `/sandbox`. The GitHub Action, which works on text anyone can write in an issue, ran the same way.

`sandbox.shell` now defaults to `required` on Linux: without the sandbox, a workspace doesn't open, and the error says how to fix it. `blitz security fix-apparmor`, and **Allow bubblewrap** in the desktop app, install an AppArmor profile that lets bwrap alone create user namespaces, every other program staying restricted. The Action sets up the sandbox on Linux runners. `sandbox.shell = "auto"` still runs commands unsandboxed, for those who choose it; settings files that `blitz config init` wrote before say so themselves, and keep it.

### SEC-2026-06

**Commands the model ran could read Blitz's own sign-ins.** Medium. Fixed in `0764340` (2026-10-01).

When Blitz signed in with Google's ADC, every command the model ran got a copy of the credentials, which hold a refresh token (since `4e9103a`, 2026-09-30); and the `ant` command's sign-in (`~/.config/anthropic`) was never blocked. A prompt-injected command with network access could send either away.

Sharing ADC is now opt-in (`[sandbox] share_adc = true`, in your own settings: a project can't set it), and the `ant` sign-in is a blocked path unless Claude signs in with it.

### SEC-2026-05

**GO-2026-5764: a crafted AWS event stream could crash Bedrock calls.** Medium (denial of service through a dependency). Fixed in `f8b0a4a` (2026-09-30), found by `govulncheck` in CI.

The Bedrock provider reached `github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream` v1.6.3, whose decoder panics on some input; updated to v1.7.8.

### SEC-2026-04

**A turn's time limit or cancellation couldn't stop a sandboxed command's set-up.** Low. Fixed in `f65554a` (2026-09-30).

On Linux, before a session's first sandboxed command, Blitz scans the writable roots for blocked paths (up to 50,000 entries, seconds on a large `/tmp`), and nothing could interrupt it: a turn limited to 300 ms ran its command for seconds. The scan now checks the command's context and stops; the command doesn't start.

### SEC-2026-03

**Checking an MCP server's command left a credential copy until Blitz exited.** Low. Fixed in `93ac51e` (2026-09-30).

`NewMCPManager` built a guarded command to check a stdio server's command and never released it: a pipe leaked, and with ADC shared, a copy of the credentials stayed on disk until Blitz exited. It's released now.

### SEC-2026-02

**A cancelled browser action could still reach the page.** Low. Fixed in `c840d16` (2026-09-30).

A browser call whose context was already cancelled was still sent over DevTools, and on a fast connection reported success: a click or navigation the agent's turn had cancelled could happen. It now returns the context's error first.

### SEC-2026-01

**Undo snapshots checked a symlink's size, not its target's.** Low. Fixed in `93ac51e` (2026-09-30).

A large file behind a link was read into the snapshot despite the size limit, and a small one behind a long link wasn't restored by undo.

## Open

Known gaps, tracked here until they're fixed or deliberately kept.

- **SEC-2026-O1. `fix-apparmor` installs a file root reads from `/tmp`.** Low. The profile is written to a temporary file the user owns, then copied into place by `sudo` or `pkexec`; a process already running as the user could swap the file while the password is asked for. A process like that has other ways to the user's password, so this is hardening: pipe the profile to root instead, and accept only a root-owned bwrap.
- **SEC-2026-O2. bubblewrap hides only blocked paths that exist when a command starts.** A blocked file a command creates is visible to that command; macOS blocks it at once. Kept: bubblewrap masks paths, not patterns.
- **SEC-2026-O3. The sandbox allows the network by default** (`sandbox.allow_network = true`), so a sandboxed command can send out what it can read: the workspace, and anything not blocked. `allow_network = false` closes it, for work that doesn't need the network.
- **SEC-2026-O4. Hooks, and MCP servers marked unsandboxed, run with the user's rights,** by design: they come from the user's own settings, or a project's only once trusted.
- **SEC-2026-O5. Windows has no OS sandbox:** commands run without it, and skill scripts don't run.

## Adding an entry

Give a fixed problem the next `SEC-<year>-<n>`, an open one `SEC-<year>-O<n>`. Say what it allowed and to whom, in the user's terms; when it came in and was fixed, with the commits; how it was found; and what the fix is, with the test that covers it. Rate it by what an attacker gets, given the [threat model](../architecture/security.md): High for a sandbox escape, code execution or credentials leaving the machine without a prompt injection's help beyond the usual; Medium where it takes more, or exposes Blitz's own sign-ins; Low for hardening and limits that didn't hold. Move an open item into the table when it's fixed.
