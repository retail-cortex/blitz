---
title: Getting started
weight: 10
---

## Install

Download the archive for your platform from the [releases](https://github.com/retail-cortex/blitz/releases). Each holds `blitz`, `blitzd`, the `blz` shortcut (a link to `blitz`, not on Windows), and the license files:

| Platform | Archive |
|---|---|
| macOS, Apple silicon | `blitz_<version>_darwin_arm64.tar.gz` |
| macOS, Intel | `blitz_<version>_darwin_amd64.tar.gz` |
| Linux, x86-64 | `blitz_<version>_linux_amd64.tar.gz` |
| Linux, ARM64 | `blitz_<version>_linux_arm64.tar.gz` |
| Windows, x86-64 | `blitz_<version>_windows_amd64.zip` |

Unpack it and put its folder on your `PATH`. The desktop app comes separately: `Blitz_<version>_macos_universal.dmg`, or `blitz-desktop_<version>_<arch>.deb` for Ubuntu 24.04, Debian 13 and later (`sudo apt install ./blitz-desktop_*.deb`).

The macOS command-line binaries aren't notarized, so a copy downloaded in a browser is quarantined and Gatekeeper won't run it. Clear the flag with `xattr -d com.apple.quarantine blitz blitzd`, or open it once through Finder's context menu.

To build from source instead, see [building](../development/building.md).

### Verify a download

Each release has a `checksums.txt`, signed with cosign (keyless, through GitHub's OIDC), and an SPDX SBOM per archive. Check the signature against this repository's release workflow, then the checksums:

```bash
cosign verify-blob --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/retail-cortex/blitz/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
sha256sum --ignore-missing -c checksums.txt
```

The desktop packages each have their own bundle: `cosign verify-blob --bundle <file>.sigstore.json` with the same flags, then the file.

## Configure

```bash
blitz config init               # writes a commented ~/.blitz/.env.toml (owner-only)
blitz config set-key gemini     # stores the key in the OS keychain
blitz doctor                    # checks the settings, credentials, sandbox, MCP servers and hooks
```

`llm.provider` picks the provider: `gemini` (the default), `anthropic`, `openai` or `ollama`. A key can also come from the environment (`GEMINI_API_KEY`, `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`). Settings are read only from `~/.blitz`, never from the project, so a cloned repository can't redirect your key or turn off approvals. [Configuration](../guide/configuration.md) has the details.

On Linux, install `bubblewrap` for the shell sandbox; `blitz doctor` says whether it works.

## First run

```bash
cd ~/src/project
blitz                                   # an interactive session
blitz "why does the build fail?"        # one prompt, then exit
```

In a session, type a request. Blitz reads and searches the workspace, shows a diff before each edit and asks before commands run; answer `y` (once), `s` (for the session), `a` (always) or `n`. `/undo` puts back what the last turn changed, and `/help` lists the commands.

## Keep the service running (optional)

```bash
blitz service install           # starts blitzd at every login (launchd or systemd)
```

With the service running, `blitz` attaches to it, the desktop app works, and [workers](../products/service.md#workers) run on their schedules.
