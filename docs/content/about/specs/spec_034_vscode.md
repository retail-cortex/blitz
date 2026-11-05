---
aliases: ["/about/specs/spec_vscode_034/"]
title: "034 · VS Code extension"
weight: 34
---

*Blitz in VS Code* (`spec_034_vscode`)

| | |
|---|---|
| Status | **Implemented** (2026-09-30). It details [spec_027_parity](spec_027_parity.md) §11 (PAR-INT-02); JetBrains is not built. |
| Source | `apps/vscode/src` (`extension.ts`, `proxy.ts`, `page.ts`, `patch.ts`, `compose.ts`, `socket.ts`), `package.json` (the manifest), `BUILD.bazel`; `tools/vsix`; the page's `apps/desktop/web/src/{host.ts,EditorPanel.tsx,workspaceSettings.ts}`; `pnpm-workspace.yaml` and `pnpm-lock.yaml` at the root |
| Tests | `apps/vscode/src/*.test.ts` (`bazel test //apps/vscode:all`); `tools/vsix/main_test.go` |

## 1. Purpose

The desktop app's conversation, beside the code in VS Code, attached to the same service: sessions, approvals and settings are shared with the terminal and the desktop app. The extension holds no engine and no second chat: it shows the desktop app's page.

## 2. The view

- **VSC-01** A **Blitz** view container in the activity bar (a bolt icon) with one webview view, **Chat** (`blitz.chat`), kept alive while hidden (`retainContextWhenHidden`) so a turn keeps streaming.
- **VSC-02** Its workspace is the folder of the file in front, else the window's first folder; with no folder it says to open one.
- **VSC-02a** *2026-11-05 (scan N26-apps-other#8: in a multi-root window, a chat for one folder opened links, showed diffs and added files of the folder in front later).* The view keeps the folder it opened for: chat links, **Show in the editor** and **Add Selection** / **Add File** use it, whichever file is in front later, and a file of another folder is refused with a warning (`inWorkspace`). The view picks its folder again when it's opened anew (closed, or the window reloaded); a failed start is tried again then too.
- **VSC-03** The view loads the page built for the desktop app (`//apps/desktop/web:page`, packed as `media/page`), its asset paths turned into webview URIs, under a policy that allows only its own files, scripts with the view's nonce, and calls to the proxy (`page.ts`). The settings go first, as `window.blitzEditor` (`dir`, `api`, `token`).
- **VSC-04** Inside an editor the page shows only the conversation (`EditorPanel`): the workspace's settings, the project-trust question and banner, and the chat (`useWorkspaceSettings`, shared with the desktop app's workspace). Its theme follows VS Code's (the body's `vscode-dark` / `vscode-light` classes), its notifications are VS Code's, and links to files in the chat open them in VS Code at their line. Its controls are the desktop app's (one page): the accent's buttons, fields and composer buttons ([spec_024_desktop](spec_024_desktop.md) DSK-35).
- **VSC-04a** *2026-11-05 (scan N26-desktop-1#8: `/diff`, `/processes` and the like did nothing there, without a word).* A command that opens one of the desktop's views says in the chat that the view isn't in the editor's panel.

## 3. Reaching the service

- **VSC-10** A webview can't open a Unix socket, so the extension forwards the page's calls (`/blitz.v1.*`) from `127.0.0.1:<random port>` to the service's socket (`proxy.ts`), streaming responses as they come. The socket is the `blitz.socket` setting, else `$BLITZ_SOCKET`, else `~/.blitz/run/blitz.sock`.
- **VSC-11** Every call carries the view's random token (`x-blitz-token`); one without it is refused (403), as is anything outside the API (404). CORS answers only `vscode-webview://` origins, so no web page can use the port. With the service down, calls get Connect's `unavailable` error and the page says so.

## 4. Editor and chat

- **VSC-20** An approval with a diff has **Show in the editor**: each file it changes opens in VS Code's diff view, the file as it is beside the change applied (`patch.ts`: hunks placed where they say, else the nearest place their context fits, as patch does; a change that no longer fits says so). A new file shows against an empty one.
- **VSC-21** **Blitz: Add Selection to Chat** (⌘⌥B / Ctrl+Alt+B, the editor's menu with a selection) adds to the composer, after what's typed, where the code is from and the code, fenced with its language (for example "`pkg/api/backend.go` lines 3–5:"). **Blitz: Add File to Chat** (the editor's menu without a selection, the Explorer's menu) mentions the file, as the desktop app's Files view does (`@path`); a file outside the workspace is refused with a warning. **Blitz: Open Chat** shows the view.
- **VSC-22** Messages sent before the page listens wait for its `ready` message.
- **VSC-23** *2026-11-05 (scan N26-apps-other#19).* The proposed versions a diff shows are forgotten when their document closes, and only the newest 100 are kept (`ProposedTexts`). The extension's own words (the no-folder notice, a failed start, the diff's title, a file outside the workspace) go through `vscode.l10n.t`, with Spanish and French bundles (`l10n/`).

## 5. Packaging

- **VSC-30** `bazel build //apps/vscode:vsix` compiles the TypeScript (CommonJS, `tsconfig.build.json`) and packs `blitz.vsix` with `tools/vsix`: `extension/` (package.json, `dist`, `media`, README, LICENSE) with the `extension.vsixmanifest` and `[Content_Types].xml` vsce would write, entries dated 1980-01-01 so builds match. `--action_env=VSIX_VERSION=…` stamps a version; releases attach `blitz_<version>_vscode.vsix`, covered by the signed checksums. Install with `code --install-extension blitz.vsix`. It isn't published to the Marketplace.
- **VSC-30a** *2026-11-05 (scan N26-pkg-small-2#15: `[Content_Types].xml` took its types from the build machine's tables, so the .vsix differed between Linux, macOS and a bare container).* `tools/vsix` gives each extension's media type from its own fixed list (`.js`, `.map`, `.md`, `.txt`, `.css`, `.png`, `.svg`, …), `application/octet-stream` for others: the same package on every machine.
- **VSC-31** The npm workspace is at the repository's root (`pnpm-workspace.yaml`: the page and the extension; one `pnpm-lock.yaml`); THIRD_PARTY_NOTICES lists the page's packages, which are what ships.

## 6. Checked by hand

With a service run with a throwaway home and no keys, VS Code 1.139 installed the package and showed the chat for its folder, in its dark theme (2026-09-30).
