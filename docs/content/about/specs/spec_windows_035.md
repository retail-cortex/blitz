---
title: "035 · Windows"
weight: 35
---

*The desktop app on Windows, with the service in WSL* (`spec_windows_035`)

| | |
|---|---|
| Status | **Planned** (2026-10-03). Not started: this is the plan to pick up on a Windows machine. |
| Source | `pkg/socket`, `pkg/wsl` (planned), `apps/service/internal/daemon`, `apps/service/internal/server`, `apps/desktop` (`main.go`, `app.go`, `proxy.go`, `prefs.go`, `folder.go`, `deeplink.go`, a Windows `service_windows.go`), `apps/desktop/packaging/windows` (planned), `release/`, `.github/workflows/release.yml` |
| Depends on | [spec_service_021](spec_service_021.md), [spec_desktop_024](spec_desktop_024.md), [spec_monorepo_028](spec_monorepo_028.md), [spec_release_025](spec_release_025.md) |

## 1. Purpose

Blitz on Windows as a native window: the desktop app built for Windows (Wails with WebView2), driving a `blitzd` that runs inside WSL2. The agent and everything it runs stay on Linux, so the OS sandbox (bubblewrap), the shell tools, gVisor and the file tools work exactly as they do on Linux; Windows only shows the window.

Chosen over the alternatives (2026-10-03):
- **Everything inside WSL** (the `.deb` under WSLg) is less work, but it's a Linux window on Windows.
- **A native Windows service** has no OS sandbox: AppContainer is the closest equivalent, and that's a project of its own.

Known gaps, accepted: no tray icon, the CLI lives in WSL, and workspaces should be in the Linux filesystem (§6).

## 2. What's there today (2026-10-03)

- **Builds and protocol:**
  - `blitz.exe` and `blitzd.exe` already cross-compile (`release/BUILD.bazel` `blitz_windows_amd64`).
  - The page needs no change: it calls the app's own origin, and the Go proxy (`apps/desktop/proxy.go`) forwards `/blitz.v1.*` to the service.
  - Wails v2.14 and its WebView2 backend (`go-webview2`) are in `go.mod`. Wails on Windows needs no cgo.
- **Gaps, each owned by a requirement below:**
  - The service listens only on a Unix socket, with no authentication: the 0700 directory and 0600 socket are the access control (`pkg/socket`, `daemon.go`, `server/listen.go` takes any `net.Listener`). WIN-01, WIN-02.
  - The service accepts only absolute Linux paths as workspaces (`server.go` `canonical`): `C:\…` and `\\wsl$\…` are `INVALID_WORKSPACE`. WIN-20.
  - Service management is launchd or systemd, local (`pkg/loginitem`, `app.go` Install/Stop/Restart, SIGTERM by pid). WIN-11–WIN-15.
  - The desktop's preferences drop workspace paths that aren't `filepath.IsAbs`, and `/home/…` isn't absolute on Windows (`prefs.go` `normalize`). WIN-21.
  - The desktop's folder dialog, Open folder, Reveal and Open document stat paths locally (`folder.go`, `app.go`). WIN-22.
  - No Windows desktop binary, packaging (icon, manifest, installer, `blitz://` registration), runner or artifact. WIN-50–WIN-55.
  - Export as PDF's printer has no Windows implementation (`printpdf/print_other.go`). WIN-30.
  - The tray, Install CLI and Fix sandbox are macOS and Linux only. WIN-33–WIN-35.

## 3. The service over localhost TCP (shared work, also useful without Windows)

- **WIN-01** `blitzd --listen tcp:127.0.0.1:<port>` (port 0 picks a free one) serves the same handler over TCP, beside or instead of the socket (`--socket` keeps working; both may be given). It binds loopback only: refused for any other address.
- **WIN-02** Every TCP request must carry `x-blitz-token: <token>`, the same header the VS Code extension's proxy uses (`apps/vscode/src/proxy.ts`). The token is 32 random bytes, hex, compared in constant time. A missing or wrong token gets `401`, logged without the token.
- **WIN-02a** DNS-rebinding and browser defences: a request whose `Host` isn't `127.0.0.1:<port>` or `localhost:<port>`, or which has an `Origin` header (no allowed origin; the Windows app's proxy sends none), is refused.
- **WIN-03** On listening, the service writes `~/.blitz/run/blitz.tcp.json` (owner-only, written atomically, removed on exit): `{"address": "127.0.0.1:41234", "token": "…", "pid": 1234}`. The Windows app reads it (WIN-12).
- **WIN-04** `pkg/socket` grows an endpoint: `socket.Endpoint{Network: "unix"|"tcp", Address, Token}`, with `Client(e)` adding the header and `Running(e)` probing either. `serviceProxy` takes an endpoint (`apps/desktop/proxy.go`), as do the CLI's client and the VS Code extension's, which keep using the socket.
- **WIN-05** Tests:
  - the TCP listener, token and origin refusals (`server`, `daemon`);
  - the endpoint file (contents, mode, removal);
  - the proxy over TCP with a token (`apps/desktop/proxy_test.go` against `servicetest` on TCP).

## 4. Finding, starting and stopping the service in WSL (Windows build only)

- **WIN-10** `pkg/wsl` (planned) (Windows only; pure Go, `os/exec` on `wsl.exe`):
  - `Distros()` from `wsl.exe -l -q`, whose output is UTF-16LE: decode it;
  - the default distro (`wsl.exe -l -v` marks it, or the registry's `Lxss` `DefaultDistribution`);
  - `Run(distro, args…)`, using `wsl.exe -d <distro> --exec …`;
  - `HomeUNC(distro, user)`, `\\wsl.localhost\<distro>\home\<user>`.

  Errors say what to do: "WSL isn't installed: `wsl --install`", "no Linux distribution".
- **WIN-11** The app's distro: chosen in Settings (default: WSL's default distro), and saved in `desktop.json` as `wsl_distro`. `ServiceStatus` (`app.go`) on Windows reports:
  - the distro;
  - whether `blitzd` is installed there (`command -v blitzd` through `sh -lc`, or `~/.local/bin/blitzd`);
  - whether it runs: its endpoint file exists and a request with its token succeeds.
- **WIN-12** Connecting: the app reads `blitz.tcp.json` through `\\wsl.localhost\<distro>\home\<user>\.blitz\run\` (or `wsl.exe --exec cat`), then proxies to that address with that token. With WSL2's localhost forwarding, a port bound to 127.0.0.1 inside WSL is reachable at `127.0.0.1` on Windows. Mirrored networking works too; check both on the test machine. If the address doesn't answer, `ServiceStatus` says so and the docs explain `localhostForwarding`.
- **WIN-13** Installing: when `blitzd` isn't in the distro, **Install** runs the release's `install.sh` there (`wsl.exe -d <distro> -- sh -c "curl -fsSL …/install.sh | sh"`, the same script the docs give for Linux), then starts it. It never installs WSL or a distro: it says how (`wsl --install -d Ubuntu-24.04`).
- **WIN-14** Starting: if the distro has systemd (`/run/systemd/system` exists), the existing Linux user unit is installed there (`pkg/loginitem` through a `blitzd --install-login-item` flag, or the CLI's equivalent), with `--listen tcp:127.0.0.1:0` added to its arguments. It then runs whenever WSL starts, and the app starts WSL by connecting. Without systemd, the app starts `blitzd` itself (`wsl.exe -d <distro> --exec nohup blitzd --listen …`, detached) each time it opens. Either way, a running `blitzd` keeps the WSL VM up.
- **WIN-15** Stopping and restarting: through the unit (`systemctl --user stop/restart blitz` in the distro), else `wsl.exe -d <distro> --exec kill -TERM <pid>`, using the pid from the endpoint file or `GetServiceInfo`. Never `os.FindProcess` on Windows. `ProgramExists(info.executable)` (service version checks) runs `test -x` in the distro.
- **WIN-16** Version match: as today (`serviceVersion.ts`), the page compares the service's version with the app's and offers **Update** (WIN-13's install again).

## 5. Paths: Windows in the window, Linux in the service

- **WIN-20** Workspaces are always the distro's Linux paths: the service, sessions, prefs and the page see `/home/ryan/src/linkr`, never a Windows path.
- **WIN-21** `prefs.go` `normalize` keeps workspace dirs that are absolute in the slash form (`path.IsAbs`) on every OS, instead of `filepath.IsAbs`. Existing macOS and Linux prefs are unchanged.
- **WIN-22** Translation at the edges, in the Windows app (`pkg/wsl` (planned): `ToLinux`, `ToWindows`; `wslpath -u` and `-w` as the fallback):
  - Folder dialog → Linux:
    - `\\wsl.localhost\<distro>\…` or `\\wsl$\<distro>\…` → `/…`;
    - `C:\…` → `/mnt/c/…` (allowed, with a one-time warning that the Windows filesystem is slow from WSL and its permissions approximate);
    - another distro's path is refused.
  - Linux → Windows, for **Open folder**, **Reveal in File Explorer** and **Open in the system viewer**: `/…` → `\\wsl.localhost\<distro>\…`, and `/mnt/c/…` → `C:\…`. `folder.go`'s Windows cases (`explorer`, `explorer /select,`) already exist; they get the translated path. Stats go through the UNC path.
  - Deep links (`blitz://open?dir=`) take either form, translated before use.
- **WIN-23** The service's own paths sent to the page (its log dir in `LogViewer`, its executable) are Linux paths. The app translates them only where it opens them.

## 6. Features on Windows

- **WIN-30** Export as PDF: `PrintPDF` on Windows uses WebView2's own print-to-PDF (`ICoreWebView2_7::PrintToPdf`) on a hidden second WebView2, through `go-webview2`'s COM bindings, paginated to A4 or Letter. If that's out of reach for v1, the button asks the service to typeset it with `export_pdf`'s engine: a new `FileService.ExportPDF{workspace, path}` around `mdpdf` (no Mermaid drawing). Decide on the Windows machine.
- **WIN-31** Notifications: Wails v2 on Windows shows toasts. Check that `Notify`, and a click opening the workspace, behave; the app needs an AppUserModelID, which the installer sets.
- **WIN-32** Single instance (`dev.blitz.desktop`) and window state: as today.
- **WIN-33** Tray: not on Windows in v1 (`SetTray` refuses; Settings hides it).
- **WIN-34** Install CLI: on Windows, it installs the CLI in the distro (WIN-13 already puts `blitz` there) and says so ("open your WSL terminal and run `blitz`"). It never writes Windows shell profiles.
- **WIN-35** OS sandbox status and **Fix**: reported from the service (a new `GetServiceInfo` field with `sandboxsetup.Check` run in the service's environment), so the window shows the distro's bubblewrap state. Fix runs `pkexec` in the distro through `wsl.exe`, or shows the commands to run.
- **WIN-36** Settings shows a **WSL** section on Windows: the distro, the service's state, Install/Update, Restart, and the endpoint (address only, never the token).
- **WIN-37** The documentation says to keep workspaces in the Linux home (`\\wsl.localhost\<distro>\home\<you>`, which VS Code's WSL extension and Explorer open too), and why: speed, permissions and links.

## 7. Build and packaging

- **WIN-50** Bazel: `//apps/desktop:blitz-desktop_windows_amd64`, a pure-Go cross-compile from any host (`goos = "windows"`, `goarch = "amd64"`, gotags `desktop,production`). cgo files are already OS-specific (`link_darwin.go`, `printpdf`'s darwin and linux files). `-H windowsgui` keeps a console window from opening. The Info.plist cc_library and `macho_uuid_linkopts` are selected out on Windows.
- **WIN-51** Windows resources: the icon (`appicon.png` made into `.ico`), an application manifest (per-monitor DPI awareness, Common Controls v6, `asInvoker`) and version info, compiled into a `.syso` that the Go build picks up. Generate it with `go-winres` or `rsrc` under Bazel, or check in the generated `.syso` with the script that makes it.
- **WIN-52** WebView2 runtime: present on Windows 11 and on updated Windows 10. When missing, the app says so and links Microsoft's Evergreen bootstrapper (Wails can embed the bootstrapper: decide by size).
- **WIN-53** Installer: per-user, no administrator rights (`%LOCALAPPDATA%\Programs\Blitz`). It adds a Start-menu entry and the AppUserModelID, registers `blitz://` (HKCU `Software\Classes\blitz`, `shell\open\command` → `"…\blitz-desktop.exe" "%1"`), and adds an uninstaller. NSIS, as Wails does, or a WiX MSI; NSIS runs under Bazel through `makensis` on Linux. A plain `.zip` ships first.
- **WIN-54** Signing: Authenticode. Without a certificate, SmartScreen warns on first run; the release notes say so until there is one. Releases are cosign-signed as today either way.
- **WIN-55** Release: `release.yml` builds the Windows desktop archive and installer on the Linux runner (cross-compiled), and a `windows-latest` job smoke-tests it: it starts, shows its window, and its version matches the tag. WSL on GitHub's runners is limited, so the end-to-end check is manual (§9).
- **WIN-56** CI: a Windows job runs `bazel test` for the packages that build on Windows (`pkg/socket`, `pkg/wsl` (planned), `apps/desktop`'s portable tests). It's optional until it's green.

## 8. Order of work

1. **WIN-01–WIN-05** (TCP and token), on macOS or Linux; it's testable without Windows.
2. **WIN-50, WIN-51:** a Windows binary that opens its window, built from macOS by cross-compiling, then run on the Windows machine.
3. **WIN-10–WIN-12:** connect to a hand-started `blitzd --listen` in WSL.
4. **WIN-20–WIN-23:** paths, so opening a workspace, the Files shelf, Reveal and Open work.
5. **WIN-13–WIN-16:** install, start, stop and update from the window.
6. **WIN-30–WIN-37:** features, Settings and docs.
7. **WIN-52–WIN-56:** runtime check, installer, release and CI.

## 9. Picking this up on Windows

Setup:
- Windows 11 (or Windows 10 22H2), with `wsl --install -d Ubuntu-24.04`.
- In Ubuntu: the build dependencies as in CI (`bubblewrap`, `pkg-config`; GTK and WebKitGTK are only for the Linux desktop app), Bazelisk, and this repository cloned in the Linux home.
- Build the Windows app from Ubuntu (`bazel build //apps/desktop:blitz-desktop_windows_amd64` once WIN-50 exists), and copy it to Windows, or run it from `\\wsl.localhost\…`.

Manual checks, each written into `docs/content/development/manual-verification.md` as it's done:
- WSL's networking: whether a port bound to 127.0.0.1 in WSL is reachable from Windows (NAT with localhost forwarding, and mirrored).
- First run with no `blitzd` in WSL: Install, then start, then connected.
- Open a workspace in `~/src` through the folder dialog; the chat; a turn with an approval; Changes; the Files shelf; Reveal; Open in the system viewer.
- Restart WSL (`wsl --shutdown`): the app reconnects or restarts the service.
- A `C:\` workspace: the warning, and it works.
- Export as PDF; notifications; a `blitz://open` link.

## 10. Known gaps (accepted)

- **Linux tools only:** the agent's shell is Linux, so it builds and runs Linux tools; Windows-only toolchains (MSVC, .NET Framework) aren't reachable, except as Windows programs called through `/mnt/c` (WSL interop).
- **Slow Windows drives:** a workspace on a Windows drive works, but slowly.
- **Desktop features:** no tray icon; the CLI lives in WSL.
