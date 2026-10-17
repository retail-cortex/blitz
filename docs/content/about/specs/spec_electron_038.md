---
title: "038 · Electron trial"
weight: 38
---

*The desktop app in Electron: one engine on every platform, and a real app on Windows* (`spec_electron_038`)

| | |
|---|---|
| Status | **Shipping** from v0.6.6 (approved 2026-10-16; the owner's decision after trying it on macOS and Windows): Electron replaces the Wails app in releases (EL-80). Done: the shared host and `blitz-tray --serve` ([spec_service_021](spec_service_021.md) SVC-53a), the shell (EL-01–EL-22), the page's routing (EL-40–EL-42), the packages (EL-80). Done also: the updates (EL-90–EL-93) and the web preview with Add to chat (EL-100–EL-105). The Wails app (`apps/desktop`) still builds but isn't released. |
| Source | `apps/electron` (`src/main.ts`, `src/preload.ts`, `src/logic.ts`, `dev.sh`, `defs.bzl`, `BUILD.bazel`), `apps/electron/packaging` (`BUILD.bazel`, `mac.bzl`, `sign_macos.sh`, `entitlements.plist`, `contents_test.sh`), the Electron runtime in `MODULE.bazel`, `pkg/pagehost`, `apps/tray/internal/tray/serve.go` |
| Tests | `apps/electron/src/logic.test.ts` (vitest), `pkg/selfupdate/*_test.go`, `pkg/pagehost/update_test.go`, `apps/tray/internal/tray/apply_test.go`, `apps/desktop/web/src/updates.test.ts`, `apps/desktop/web/src/desktop.test.ts` (the routing), `//apps/electron:typecheck`, `apps/tray/internal/tray/serve_test.go`, `pkg/pagehost/host_test.go` |

## 1. Purpose

The desktop app ([spec_desktop_024](spec_desktop_024.md)) is Wails: each system's own web view, WebKit on macOS and WebKitGTK on Linux. Windows has none. There the tray serves the page to Edge ([spec_windows_036](spec_windows_036.md)), without a folder picker, PDF export, the CLI's installation or menus. WebKitGTK has cost real bugs too (the NVIDIA crashes, #39 and #52).

The trial tries Electron instead: Chromium on macOS, Linux and Windows. It's a new surface beside the Wails app, not a replacement. Whether it replaces Wails is decided from measured numbers (§6).

## 2. How it fits together

- **EL-01** Electron's main process starts `blitz-tray --serve --page <app>/page` (SVC-53a) from the app's `bin/`, reads the link it prints, and opens the window on it. The page, the service's API (`/blitz.v1.*`, to the socket) and the host's calls (`/host/`, `pkg/pagehost`) share that one loopback origin, behind the one-time link and the cookie (WIN-11–13). So the page needs nothing new to reach the service or the host.
- **EL-02** The host stops when its standard input closes, which happens when Electron quits. If it stops on its own, the main process starts it again and points the window at the new link.
- **EL-03** The app (`//apps/electron:app`, `resources/app` once packaged) holds `package.json`, `dist/` (the main process and preload, TypeScript compiled by `tsc`), `page/` (the desktop page, `//apps/desktop/web:page`) and `bin/` (`blitz-tray`, with `blitzd` and `blitz` beside it as in the desktop app's packages, so the host finds them as it does there).
- **EL-04** The Electron runtime comes from Electron's release archives, one per platform (darwin-arm64, darwin-x64, linux-arm64, linux-x64, win32-x64), each pinned by its SHA-256 in `MODULE.bazel` (44.5.1). The `electron` npm package is there for its type definitions only: pnpm runs no install scripts.
- **EL-05** `bazel run //:desktop` (`//apps/electron:dev`) runs it on this machine against its service; `//:desktop-wails` runs the Wails app, which still builds but isn't released. The root's `//:mac-app`, `//:deb`, `//:pacman`, `//:desktop-tar` and `//:windows-zip` are the Electron packages (EL-80). Electron runs from where its archive was unpacked, not from the runfiles' links, so macOS finds its framework. On macOS it runs a copy of Electron's bundle named Blitz (`CFBundleName`, `CFBundleDisplayName`, bundle ID `dev.blitz.electron`, re-signed ad hoc), made once per Electron in `~/Library/Caches/dev.blitz.electron`: macOS names the app in the menu bar, the Dock and the window switcher from the bundle, which says Electron. 

## 3. The window

- **EL-06** The Windows zip's program is Electron's, as `Blitz.exe`, with Blitz's icon (the app's, at 16–256 px) and file details (product and description Blitz, Retail Cortex, the release's version), which Explorer, the taskbar and Task Manager show: `//apps/electron/winexe` rewrites its resources at build time, on any machine (`tc-hib/winres`), keeping the rest (its manifest), and removes Electron's signature, which no longer matches. Elsewhere the window has the icon too (`icon.png` in the app).
- **EL-07** The window draws with the graphics card (Chromium's own choice of renderer) unless `BLITZ_GPU=off`, or the `gpu` preference is `off` (**Use the graphics card** in Settings › Appearance, shown on Linux in either app): then Electron starts with hardware acceleration off (`gpuOff`, read from `~/.blitz/desktop.json` before Electron starts). The Wails app's WebKitGTK workarounds (its NVIDIA renderer, DMA-BUF) don't apply.

- **EL-10** One window: a second start (with a `blitz://` link, on Linux and Windows) goes to the first (`requestSingleInstanceLock`).
- **EL-11** Locked down: the page runs sandboxed, with context isolation and without Node. It sees only the preload's `window.blitzElectron`. The window never leaves the host's origin: other links open in the system's browser or mail program if they're web or mail links (`isExternal`, as the desktop app's `SafeURL`), and nowhere otherwise. New windows are refused. Permissions are granted for notifications, writing to the clipboard and full screen only.
- **EL-12** On macOS the page draws the whole window, title bar included (hidden inset), over the desktop showing through (`vibrancy`). Its title bar moves the window: the page's `.drag-region`s are Electron's drag regions too (`app-region: drag`, beside the Wails app's `--wails-draggable`), with what can be clicked in them excepted; and since Electron drags wherever a region is, even under what's drawn over it, menus, dialogs, the palette, snackbars and the drawer are excepted too (`app-region: no-drag`). The preload says so (`nativeGlass`), for the page's glass ([spec_desktop_024](spec_desktop_024.md) DSK-38). Elsewhere the window is solid.
- **EL-13** macOS's menu bar is the desktop app's (DSK-101): **About Blitz**, **Settings…** (⌘,), Hide, Hide Others, Show All, **Quit Blitz** (⌘Q), then Edit and Window, in the page's language (`SetMenu`). About and Settings tell the page (`menu:about`, `menu:settings`). Elsewhere there's no menu bar.
- **EL-14** Closing the window, or quitting, with unsaved changes in the editor asks first, in the page's words (`SetUnsaved`, as the desktop app's `beforeClose`).

## 4. What the page asks of the window

- **EL-20** `window.blitzElectron` has `methods` (the calls below), `nativeGlass`, `call(method, ...args)` and `on(event, f)` for `menu:about`, `menu:settings`, `deeplink:open` and `notification:open`. The main process answers only calls from the page's own origin, and only these:
  - `ChooseWorkspace(title)`: the system's folder picker (it can make a folder); `""` when cancelled.
  - `Notify(title, body, dir)`: a system notification; a click brings the window back and sends `notification:open` with `dir`.
  - `OpenURL(url)`: web and mail links only, in the system's browser; anything else is an error.
  - `PrintPDF(html, pageSize)`: the page (whole HTML, pictures inline) printed to PDF in a hidden window with its scripts off, on A4 or Letter paper with 15 mm margins, after it has loaded; the bytes as base64, as the desktop app returns them. A minute at most.
  - `SetAppearance(theme)`: the window's theme (`system`, `light` or `dark`).
  - `SetMenu(labels)`, `SetUnsaved(u)`: EL-13 and EL-14.
  - `PendingLinks()`: the `blitz://` links that came before the page was ready.
  - `ToggleFullscreen()`.
- **EL-21** Everything else the page asks of its host (the service's status and controls, preferences in `~/.blitz/desktop.json`, the CLI, the sandbox, files, licenses) goes to `/host/` (WIN-14), as on the Windows tray's page.
- **EL-22** `blitz://open?dir=<folder>&prompt=<text>` links ([spec_parity_027](spec_parity_027.md) PAR-INT-03) reach the page as `deeplink:open`, or wait for `PendingLinks`: the folder must be an absolute path that exists (`~` is the home folder), the prompt at most 20,000 characters. The packaged app registers the `blitz` scheme.

## 5. The page, and what's not done

- **EL-40** The page's `desktop.ts` routes each of the app's calls by capability (`canCall`): the Wails app, else the Electron window (`window.blitzElectron`), else the host (`/host/`), else the browser's stand-in. The window's events (`onAppMenu`, `onDeepLink`, `onNotificationOpen`) come from the Wails runtime or the Electron window alike, the window's glass (`nativeGlass`) from either, and Export as PDF is offered wherever `PrintPDF` is.
- **EL-41** Preferences go wherever `GetPrefs` is answered. Outside the Wails app, the first time the host keeps them and the browser's storage still has some, the browser's move to the host if the host has no workspaces yet, and the browser's copy is forgotten either way (`adoptBrowserPrefs`): the Windows tray's page, which kept them in the browser, doesn't lose its workspaces.
- **EL-42** *Known limit.* The host serves on a new loopback port each time it starts, so the window's origin changes with each launch, and what the page keeps in the browser's storage for itself (where the user guide was left, folded sections) starts afresh each time, as on the Windows tray's page.
- **EL-31** Not yet: Authenticode signing on Windows (SmartScreen asks before the zip's `Blitz.exe` first runs), and a Windows installer.
- **EL-80** *Since v0.6.6* releases carry the Electron app, not the Wails app (`apps/electron/packaging`; [spec_release_025](spec_release_025.md) REL-21–26). Each package's app comes from `electron_app` (`defs.bzl`): `package.json`, `dist/`, `page/`, `icon.png` and `bin/` with that platform's `blitz`, `blitzd` and `blitz-tray`.
  - **macOS:** `Blitz_<arm64|x64>.app` (`mac.bzl`), Electron's bundle copied whole (`ditto`, the framework's links kept), named Blitz with the Wails app's identifier `dev.blitz.desktop` (so permissions and settings carry over), the release's version, the `blitz` URL scheme, the app's icon, the app in `Contents/Resources/app`, Electron's default app removed and its licenses in `Contents/Resources/licenses`, signed ad hoc. `sign_macos.sh` signs it inside out with the hardened runtime and `entitlements.plist` (JIT, unsigned executable memory, library validation off) and makes the disk image, notarised with the signing secrets. One image per Mac: Electron's runtimes are separate.
  - **Windows:** `Blitz_windows_x64.zip` (`windows_zip`, cross-built): Electron's files, `Blitz.exe` (EL-06), the app in `resources\app` and the licenses in `blitz-licenses`.
  - **Linux** (this machine's architecture): Electron in `/usr/lib/blitz-desktop` with its program as `blitz-desktop` (where the Wails app's was, so the link on PATH and the launcher stay) and `chrome-sandbox` setuid root (Electron's sandbox where user namespaces are restricted, as on Ubuntu 24.04), as a `.deb`, an Arch package and a tarball (`linux_tar`, which updates itself; unpacked by a user, its `chrome-sandbox` isn't setuid).
  - `contents_test.sh` checks each package's programs, licenses and identity.

## 6. Updates

- **EL-90** *Since v0.6.6* the app updates itself from the releases (the owner's choice: Blitz's own updater, not Electron's). The host (`pkg/pagehost` `Updater`, in `blitz-tray --serve`) answers:
  - `CheckUpdate(force)`: the latest release and where the update is (`UpdateStatus`: current, latest, available, can and why not, downloading, ready, error, the release's page). The answer of the last check (`~/.blitz/updates/last-check.json`) stands for five days (`CheckEvery`): GitHub is asked again only after that, or when forced (**Check now**).
  - `DownloadUpdate`: downloads the release's package for this machine (`selfupdate.DesktopAsset`: the Mac's disk image, the Windows zip, the Linux tarball) into `~/.blitz/updates/<tag>`, checks it against its own Sigstore bundle (`selfupdate.VerifySignature`, [spec_release_025](spec_release_025.md) REL-13 and REL-24), and unpacks it (a disk image through `hdiutil`, mounted where nothing shows it; archives refusing anything outside their folder). The call returns when it's done: nothing is polled.
  - `UpdateStatus`: where it is, without asking GitHub.
  - `ApplyUpdate`: copies the host out of the app (`~/.blitz/updates/apply-blitz-tray`) and starts it as `--apply-update` with the app's process (the host's parent) to wait for; the page then quits the app through the window (`Quit`, which asks first about unsaved changes).
- **EL-91** `blitz-tray --apply-update --staged … --root … --relaunch … --wait-pid …` (`tray.Update`): waits for the app to quit (2 min at most, else nothing changes), stops the service if it runs from the app (Windows can't replace a program in use), swaps the app (`selfupdate.Apply`: the old one moved aside, the new one moved in, or copied across disks, the old one put back if that fails, then removed), starts the service again, and the app (the new one, or the old one if the swap failed), logging to `~/.blitz/logs/update.log`.
- **EL-92** It doesn't update itself where a package manager installed it (under `/usr` or `/opt`: the `.deb`, the Arch package) or in a development build: `can` is false and `reason` says so, and the page shows the reason.
- **EL-93** The page (`updates.ts`): when the app opens, unless `updates` is `off` in the preferences, it checks (at most every five days), downloads an update it can take, and when it's ready says so with **Restart** (a snackbar); nothing repeats while the app runs. **Settings › About** shows the update's state with **Check now**, **Download**, **Restart to update** and the release's notes, and **Update automatically** (the `updates` preference).

## 7. The web preview

- **EL-100** *Since v0.6.6* web pages open in the editor, in tabs of their own (`web:<id>` beside the files' tabs; `useEditor` `openWeb`, `WebPreview.tsx`), only in the Electron app (`canPreview`: the window answers `PreviewOpen`). Each is a `WebContentsView` over the tab's placeholder, in a session of its own (`persist:preview`, so sign-ins last), sandboxed, with no preload and no Node; downloads and every permission but writing to the clipboard are refused, and a page's new windows open in the same view. Its toolbar has back, forward, reload (stop while loading), the address, **Open in browser** and **Add to chat**; the tab is named after the page's title (`preview:state` events). Closing the tab closes the page. Tabs aren't kept when the app restarts.
- **EL-101** Only web addresses open (`previewURL`): http and https; a bare host gets `http://` for this machine (`localhost:3000`, `127.0.0.1`, `[::1]`, `*.localhost`) and `https://` elsewhere; `file:`, `data:`, `javascript:` and other schemes are refused, typed or followed from a page. A typed address goes through `PreviewOpen`; the toolbar's buttons through `PreviewNavigate`, which takes only back, forward, reload and stop.
- **EL-102** Opening one: **Open web page…** in the command palette, the globe button in the editor's tab bar, and links in the chat (or a document) to a web server on this machine, which open in a web tab instead of the browser; other links still open in the browser.
- **EL-103** The view is drawn above the page, so the page places it (`PreviewBounds`, on resizes and layout changes) and hides it (`PreviewVisible`) while the tab isn't shown, its workspace is hidden, or something overlaps it: a menu, a dialog's scrim, the palette, the drawer, a snackbar, the composer's menus (`covered`, `overlays`).
- **EL-104** **Add to chat**: **The page** and **The selection** (their HTML, read in a world of its own so the page's scripts can't change it, `contentScript`, at most 2 MB), **A screenshot** (`PreviewCapture`, a PNG added as a pasted image is) and **Console errors** (the errors the page's console showed since it opened, the latest 200). Each shows as a chip in the composer, and the turn carries the page, selection and errors as `Turn.context` ([spec_service_021](spec_service_021.md) SVC-13a).
- **EL-105** In Chromium (Electron) `scrollIntoView` returns a promise: an effect must not return it (React takes it for a cleanup and the page fails). The palette's, the search dialog's and the table view's effects are written in braces.

## 8. Findings

*To do (EL-60).* The Electron build beside the Wails app on macOS and Linux, and beside the Edge tray on Windows: download and installed size, memory with one workspace open after five minutes (all processes), cold start until the page is ready, macOS glass, the NVIDIA checks (#39, #52), and on Windows the folder picker, Export as PDF, notifications, the CLI and starting at login. Then a recommendation: replace Wails, keep both, or stop.

First readings (macOS, Apple silicon, 2026-10-16, development build): the page loads from the host and reaches the service; `PrintPDF` returns a PDF; `file:` links and unknown calls are refused; quitting stops the host; with the page routed (EL-40), the window has its glass, a workspace added through the host's preferences opens (saved in `desktop.json`), and Export as PDF writes the PDF beside the file; Electron and the host together hold about 530 MB resident at idle on the welcome screen (resident sizes added up, which counts shared memory more than once: a first number, not the comparison).
