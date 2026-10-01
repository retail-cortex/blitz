---
title: "sandboxsetup"
weight: 13
---

Linux's OS sandbox, from outside the engine. Source: [`pkg/sandboxsetup`](https://github.com/retail-cortex/blitz/tree/main/pkg/sandboxsetup).

`pkg/sandboxsetup` says whether bubblewrap, the sandbox for the agent's commands, runs here, and where AppArmor's restriction on unprivileged user namespaces stops it (Ubuntu 24.04 and later), installs an AppArmor profile that lets bwrap alone have them: `/etc/apparmor.d/blitz-bwrap`, `userns` for bwrap's real path, unconfined otherwise, as Ubuntu does for other programs that need them. Installing it takes one password prompt: `sudo` in a terminal, `pkexec` (the system's dialog) in the desktop app. Everything else keeps the restriction, unlike `sysctl kernel.apparmor_restrict_unprivileged_userns=0`.

## API

| Name | |
|---|---|
| `Check(ctx)` | The `Status`: `Ready`, `NoBwrap`, `Restricted` (what `Fix` fixes), `Broken`, or `Unsupported` (not Linux) |
| `Fix(ctx, Elevation)` | Install and load the profile, then check again; `ErrCantFix`, `ErrNoElevation` |
| `Profile(bwrap)`, `Commands(Status)` | The profile, and the commands to install it by hand |

## Used by

apps/cli (`blitz security`), apps/desktop (Settings › Service).

Spec: [shell](../about/specs/spec_shell_007.md) SH-20b.
