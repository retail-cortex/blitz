---
title: "036 · Windows"
weight: 36
---

*Blitz on Windows: the tray serves the desktop page* (`spec_windows_036`)

| | |
|---|---|
| Status | **Implemented** (2026-10-07); checked on macOS and Linux and cross-built for Windows. Running it on Windows is still to do (WIN-40, [BL-VER-03](spec_backlog_026.md)). |
| Source | `apps/tray` (`main.go`, `page.go`, `icons_windows.go`, `lock_windows.go`), `apps/tray/internal/tray` (`host.go`, `browser.go`, `ico.go`, `detach_windows.go`), `pkg/pageserver`, `pkg/fileview`, `pkg/loginitem/run_windows.go`, the service's `Shutdown` (`proto/blitz/v1/workspace.proto`), the page's `apps/desktop/web/src/desktop.ts`, `release/BUILD.bazel` |
| Tests | `pkg/pageserver/*_test.go`, `pkg/fileview`, `apps/tray/internal/tray/{host,browser,ico}_test.go`, `apps/service/internal/{server/info,daemon/daemon}_test.go` (`Shutdown`), `apps/desktop/web/src/desktop.test.ts`, `release/contents_test.sh` |

## 1. Purpose

Windows has no desktop app ([spec_desktop_024](spec_desktop_024.md) DSK-05), but the page needs no Wails. It asks its own origin for the API, and every one of the app's bindings has a stand-in in a browser. On Windows the tray becomes the launcher. `blitz-tray.exe` is one small pure-Go program that runs the service, serves the page on a loopback port, and opens it in a window of its own. There's no installer and no web view to ship.

## 2. The tray on Windows

- **WIN-01** `blitz-tray.exe` ([spec_service_021](spec_service_021.md) SVC-53) builds for Windows: pure Go (fyne.io/systray's Win32 tray), a GUI program (`-H=windowsgui`, no console), cross-built on any machine (`//apps/tray:blitz-tray_windows_amd64`).
- **WIN-02** Its icons are the bolt in colour, because Windows doesn't tint tray icons: amber while the service runs, grey while it's stopped, each edged dark so it shows on light and dark taskbars (`icons/windows-*.svg`). The PNGs are wrapped as `.ico` when it starts (`tray.ICO`: one PNG-compressed image, which Windows Vista and later read).
- **WIN-03** One tray at a time: it locks `~/.blitz/run/tray.lock` with `LockFileEx` (flock elsewhere). The programs it starts (`blitzd`, Edge) run detached: their own process group, no console.
- **WIN-04** The service has no login item on Windows (`loginitem.Install` is unsupported). The tray starts it: when the tray starts and finds no service, it runs `blitzd.exe` from beside itself or `PATH`.
- **WIN-05** **Start Blitz at login**, a menu item with a tick, toggles the tray's start-at-login entry: the value `Blitz` (the quoted path to `blitz-tray.exe`) under `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`. `blitz-tray --install` writes it and starts the tray; `--uninstall` removes it.
- **WIN-06** **Stop the service** asks the service to stop (`WorkspaceService.Shutdown`, WIN-30), since Windows has no SIGTERM to send. It falls back to the signal where there is one. Every OS does it in this order.
- **WIN-07** **Open the logs** opens the folder in File Explorer.

## 3. The page

- **WIN-10** **Open Blitz** serves the page (embedded in the tray; `pkg/pageserver`) the first time, on `127.0.0.1` and a free port, and opens a new one-time link to it. If Edge is installed (`msedge.exe` under `%ProgramFiles(x86)%`, `%ProgramFiles%` or `%LOCALAPPDATA%`), the link opens there as an app (`--app=…`), a window with no tabs or address bar. Otherwise it opens in the default browser (`rundll32 url.dll,FileProtocolHandler`).
- **WIN-11** The page server lets in only whoever opened a link. `GET /open?t=<ticket>` takes a ticket once, within a minute of its making, and sets a cookie: `blitz_<port>`, a 32-byte secret, HttpOnly and SameSite=Strict. It then redirects (303) to `/`, so neither the address bar nor the history keeps a working link. A used or expired link redirects a browser that already has the cookie, and refuses one that doesn't.
- **WIN-12** Every request needs the cookie and a `Host` that is the server's own `127.0.0.1:<port>` (no DNS rebinding). When a request names an `Origin`, it must be the page's own. Cookies ignore ports, so this also shuts out pages served from other loopback ports. Everything else is refused (403). Responses forbid framing (`X-Frame-Options: DENY`) and send no referrer; `index.html` is never cached.
- **WIN-13** `/blitz.v1.*` goes to the service's socket through the desktop app's own proxy (`pageserver.Proxy`, moved from `apps/desktop`): it streams, adds keep-alives and returns a 503 Connect error when the service is down. The cookie and `Origin` are not forwarded. The service itself still never listens on a port ([spec_service_021](spec_service_021.md) SVC-01): only the tray's server does, and only on loopback, behind the cookie.
- **WIN-14** `/host/` is the tray answering what the desktop app's bindings would (`tray.Host`). The page sends `POST /host/<Method>` with the arguments as a JSON array, and gets the result as JSON, or `{"error": …}` with a 500. `GET /host/info` lists the methods: `Version`, `ServiceStatus`, `InstallService` (on Windows: start at login, and start the service now), `SetTray`, `StopService`, `RestartService`, `ProgramExists`, `OpenFolder`, `OpenDocument`, `RevealPath`, `FileManager` ("File Explorer") and `License`. Folders and documents open through `pkg/fileview`, shared with the desktop app, which opens only folders, viewable documents, and paths shown in their folder, never a program.
- **WIN-15** Before it renders, the page asks `/host/info` (`detectHost`), but not inside the app or an editor. For each binding it uses the app if it's there, else the host if it has the method (`canCall`), else the browser's stand-in. So in the tray's page, **Show in File Explorer**, **Open in viewer**, the logs' **Open folder**, the service's controls and the licenses work. Choosing a workspace remains a prompt for a path, and the app-only features (Export as PDF, the CLI's installation, the sandbox's status) stay hidden.

## 4. The service

- **WIN-30** `WorkspaceService.Shutdown` stops the service as SIGTERM does. It answers, then turns in progress get their few seconds and the socket is removed. A server built without a way to stop (`server.WithShutdown`; tests) answers `FAILED_PRECONDITION`.
- **WIN-31** On Windows the socket's permission bits are not set (Chmod only sets read-only there). The socket is as private as `~/.blitz/run`, whose access control list comes from the user's profile.

## 5. Release

- **WIN-35** The Windows archive (`blitz_<version>_windows_amd64.zip`, [spec_release_025](spec_release_025.md) REL-12) also holds `blitz-tray.exe`. Run it; **Start Blitz at login** keeps it. `release/contents_test.sh` checks the zip.
- **WIN-36** CI builds the zip with the other archives on Linux and macOS, and the reproducible job compares them. The page in the tray is built for the machine building it, so it must also come out the same on both.

## 6. Not yet

- **WIN-40** Running on Windows has yet to be checked by hand ([Manual verification](../../development/manual-verification.md)): the icon, starting the service, the Edge window and a turn in it, Stop and Restart, Start at login across a sign-out, a request without the cookie refused, and that `~/.blitz/run` is only the user's (`icacls`).
- **WIN-41** What Windows lacks in the engine is unchanged ([BL-VER-03](spec_backlog_026.md)): no OS sandbox, no process guard, no skill scripts; shell tools need Git Bash or WSL.
- **WIN-42** A native folder picker (the page asks for a path), notifications from the tray, and a Windows Arm64 build.
