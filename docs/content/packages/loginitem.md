---
title: "loginitem"
weight: 12
---

Starting the service at login. Source: [`pkg/loginitem`](https://github.com/retail-cortex/blitz/tree/main/pkg/loginitem).

`pkg/loginitem` installs `blitzd` as a login item and controls it: a launchd agent on macOS, a systemd user unit on Linux. The CLI (`blitz service …`) and the desktop app both use it, so the app needs no CLI to install, restart or stop the service. It finds `blitzd` beside the real (symlinks resolved) executable, and on Linux a reinstall restarts a running unit so the new program runs.

## API

| Name | |
|---|---|
| `Install(blitzd)`, `Uninstall`, `Stop`, `Installed` | The login item |
| `FindService`, `Beside` | Where `blitzd` is |
| `Path`, `LogFile` | The item's file and the service's log |

## Used by

apps/cli, apps/desktop.

Spec: [service](../about/specs/spec_service_021.md).
