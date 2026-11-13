---
title: Blitz 0.9.13
weight: 1
---

Blitz now speaks Bazel. Open a Bazel workspace and Go, TypeScript and Python get completion, hover, problems and go-to-definition, even when `go.mod`, `node_modules` and your Python packages live where Bazel puts them rather than beside your code. And the agent's commands finally reach your system's certificates, so `pip install` works in the sandbox.

### Get started

1. Update Blitz. It updates itself, or run `sudo apt upgrade`, or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.9.13).
2. Open your Bazel workspace (the folder with `MODULE.bazel`) and open a Go, TypeScript or Python file.
3. Hover a name. The first Go file can take a minute while Bazel loads the packages; after that it's instant.

### New

- **Language servers in Bazel workspaces.** Go code with no `go.mod` above it is served by rules_go's package driver (yours, if the workspace has `tools/gopackagesdriver.sh`). It runs with its own Bazel output folder, so it never gets in the way of your own builds. TypeScript comes from what rules_js installed in `bazel-bin`, and Python resolves the packages rules_python's pip installed, with imports starting at the workspace like Bazel's.
- **Keys and certificates: usable, never read.** Files such as `*.pem`, `*.key` and `.env` are now *hidden* rather than *blocked*: the agent's file tools still never read them, but the commands it runs use them like any other program does. So `pip` finds your system's CA bundle, Bazel can unpack archives that hold a `.env`, and your app's tests can read their `.env`. If a command prints a private key, it's masked before the model sees it.
- **Your project's settings files are yours.** A project can have its own `.env.dev.toml` and the like; only Blitz's own settings in `~/.blitz` stay off limits.

### Fixed

- **Counting desktop users behind company firewalls.** The desktop app's daily "this install was used" message now goes through your system's network settings, including company proxies, PAC files and certificates, so it arrives. It says whether a proxy carried it (yes or no, never which). `blitz` commands the agent runs no longer count as you using the CLI.

### Good to know

- Your own settings that list `blocked_paths` keep that list as written. To get the new behaviour, take `*.pem`, `*.key` and `.env` out of it; the new `hidden_paths` setting covers them.
- The Go package driver's Bazel server stops after 30 minutes without use.
