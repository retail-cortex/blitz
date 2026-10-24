---
title: "050 · The agent's browser in the app"
weight: 50
---

*With the desktop app open, the agent browses in a view of the app's own, which you can watch, or silently; without it, headless Chrome as before* (`spec_agent_browser_050`)

| | |
|---|---|
| Status | **Built** (v0.8.4, the owner's request: "I like the idea of the agent driving the built-in browser, but it should offer to do it silently") |
| Source | `pkg/engine/browser` (`Transport`, `AttachTransport`, `Options.KeepViewport`), `pkg/engine/agentbrowser.go` (the windows and the relays), `pkg/engine/tools/browser.go` (`show`, the view or Chrome), `AgentBrowserHost` in `pkg/engine/tools/registry.go`, `WorkspaceService.WatchAgentBrowser` and `AgentBrowserReply` (`apps/service/internal/server/workspace.go`); the page's `agentBrowserRelay.ts`, `Workspace.tsx`, `files/WebPreview.tsx` (the bar), `files/useEditor.ts` (`openAgentWeb`), `BrowserSettings.tsx`; the Electron app's `AgentBrowserOpen`, `AgentBrowserSend`, `AgentBrowserClose`, `AgentBrowserTakeOver` (`apps/electron/src/main.ts`, `logic.ts`) |
| Tests | `pkg/engine/agentbrowser_test.go`, `pkg/engine/tools/browser_test.go` (`TestBrowserToolInTheApp`), `apps/desktop/web/src/agentBrowserRelay.test.ts`, `apps/electron/src/logic.test.ts` (the agent's view), `apps/service/internal/server/browserprofiles_test.go` (`browser.show`, `browser.open`), `pkg/engine/tools/openpage_test.go` (AB-10–AB-12), `pkg/engine/agentbrowser_test.go` (`TestOpenPageInTheWindow`) |
| Depends on | [spec_web_008](spec_web_008.md) (the browser tool, WEB-30–38), [spec_electron_038](spec_electron_038.md) (the window, EL-100 previews), [spec_browser_profiles_049](spec_browser_profiles_049.md) (profiles) |

## 1. Purpose

The `browser` tool drove a headless Chrome of its own: nothing to see, and none of the app's sign-ins. With the app open it should browse where the user can see it, or out of sight when they'd rather.

## 2. How it's carried

- **AB-01** A window of the app, for each workspace it shows, watches `WorkspaceService.WatchAgentBrowser` (a stream: streams over polling, so it works with a service elsewhere) and is a host of the agent's views; the newest window hosts new ones. When the browser tool needs a browser and a window is there, the engine asks it for a view (`open`: watched or silent, the workspace's profile, local addresses allowed or not, `web.deny_domains`), waits up to 15 seconds for its first reply (`{"blitz":"opened"}`), and drives it with the DevTools Protocol over the relay (`browser.AttachTransport`): each command goes down the stream (`message`), and the window sends the view's replies and events back, in order, with `AgentBrowserReply`. In the window, the page passes them to Electron, which runs them on the view's `webContents.debugger`. A window that goes closes its views; no window, a refusal, or no answer: headless Chrome, as before.
- No DevTools port is opened on the app (any local program could then drive its own page). The view refuses `Browser.*`, `Target.*`, `Storage.*`, `SystemInfo.*` and the like (EL-17), and Blitz never closes a browser it didn't start.
- The headless browser's proxy filtering (WEB-32) is the view's session's request filter here: web, WebSocket and data addresses only, no denied domain, no link-local address, loopback and private ones (literal addresses, `localhost`, `.local`, `.internal`) only with `browser.allow_local`. Page loads still go through the engine's navigation checks (WEB-34) and approvals.

## 3. Watching or not

- **AB-02** `browser.show` (your settings; Settings › Browser › **How the agent browses**): `watch`, `silent`, or `ask` (the default), which asks the first time the agent browses in a chat, **Watch it in a tab** or **Run silently**, and keeps the answer for the chat. Where no one can answer (a job's run), silently.
- **AB-03** Watched: the view is a web tab (EL-100) in the workspace's editor, the page's own size (no viewport emulation, `KeepViewport`), with a bar saying Blitz is browsing: **Take over** lets the agent go (its browser closes, the tab stays yours) and **Stop** closes the view. Silent: the view is off screen (offscreen rendering), never shown.
- **AB-04** The view uses the workspace's browser profile's partition (`persist:profile-<name>`, BP-05), so the sign-ins made with **Sign in…** apply; none, a session of its own in memory, gone with the view.

## 3a. Pages you ask to see

*v0.8.4, the owner's request (2026-10-24): "if a user asks to open a site in a browser, it should open it in the embedded browser first, or ask 'Embedded Browser' or 'System Browser'".*

- **AB-10** `open_page(url)` (`pkg/engine/tools/openpage.go`; the blitz agent has it, and its prompt says to use it, never the shell's `open`, when the user asks to open, show or go to a site). It shows the page to the user and doesn't read it. Only `http` and `https` (a bare host gets `https://`), never a host `web.deny_domains` or a `deny web(...)` rule refuses; it needs no approval, since it reads nothing.
- **AB-11** Where: `browser.open` (your settings; Settings › Browser › **Where pages you ask to open show**): `embedded`, a web tab of the app's (EL-100); `system`, the system's browser; or `ask` (the default), which asks the first time in a chat, **Embedded browser** (first) or **System browser**, and keeps the answer for the chat; no answer, the embedded browser.
- **AB-12** It's carried on the same stream as the views (AB-01): the engine sends the newest window `page` (a web tab) or `external` (the system's browser, Electron's `openExternal`) with the URL, and the page opens it (no web tabs outside Electron: the system's browser). No window, or one that can't take it: the system's browser on the service's machine (`open`, `xdg-open`, `url.dll`). A run no one watches (a job's) opens nothing and says so.

## 4. Not in scope

- A chip in the chat that opens a silent view as a tab: the tool calls (and their screenshots) show what it did.
- Driving the user's own Chrome (an extension and native messaging): left out of v0.8.4.
