---
title: Getting started
weight: 10
---

## Install

On macOS or Linux, the install script puts `blitz`, `blitzd` and `blz` in `~/.local/bin`, after checking the archive against the release's signed checksums (the signature itself when [cosign](https://docs.sigstore.dev) is installed):

```sh
curl -fsSL https://github.com/retail-cortex/blitz/releases/latest/download/install.sh | sh
```

`BLITZ_BIN` picks another folder, `BLITZ_VERSION=v0.2.0` a release, and `BLITZ_REQUIRE_SIGNATURE=1` refuses to install without checking the signature. With Homebrew: `brew install retail-cortex/tap/blitz`.

Later, `blitz update` installs the latest release over this one (`--check` only says whether there is one). It checks the release's signature with cosign, and its checksum; without cosign it asks for `--skip-signature`. A copy installed by Homebrew is updated with `brew upgrade blitz`.

Or download the archive for your platform from the [releases](https://github.com/retail-cortex/blitz/releases). Each holds `blitz`, `blitzd`, the `blz` shortcut (a link to `blitz`, not on Windows), and the license files; Windows's also has `blitz-tray.exe`, which runs the service and opens the [desktop app's page](../products/desktop.md#on-windows):

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

**On Linux, the shell sandbox is required.** Install bubblewrap (`sudo apt install bubblewrap`) and run `blitz security`: it says whether bubblewrap runs. On Ubuntu 24.04 and later, AppArmor restricts the user namespaces it needs; `blitz security fix-apparmor` (or **Allow bubblewrap** in the desktop app) installs an AppArmor profile that lets bwrap alone have them, asking for your password once. Until the sandbox works, workspaces don't open; to run the agent's commands unsandboxed instead, set `[sandbox] shell = "auto"` in `~/.blitz/.env.toml`.

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
