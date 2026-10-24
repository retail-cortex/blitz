---
title: "049 · Browser profiles"
weight: 49
---

*Browser profiles: the sign-ins (OAuth, single sign-on, a site's own) the agents browse with, one chosen per workspace* (`spec_browser_profiles_049`)

| | |
|---|---|
| Status | **Built** (v0.8.4, the owner's request: "research if we can expose the profile features of Chromium, since we want to take advantage of OAuth and other web protocols and authentications"; one profile per workspace, the owner's choice 2026-10-23) |
| Source | `pkg/config/browserprofiles.go`, `pkg/engine/browser/browser.go` (`Options.ProfileDir`), `pkg/engine/tools/browser.go` (`syncProfile`, `webRules`), `browserProfile` in `pkg/engine/workspace.go`, `apps/service/internal/server/browserprofiles.go`, `ConfigService.*BrowserProfile*` in `proto/blitz/v1/config.proto`; the page's `BrowserSettings.tsx` and `WorkspaceSettingsForm.tsx`; the Electron app's `SignInWindow` (`apps/electron/src/main.ts`) |
| Tests | `pkg/config/browserprofiles_test.go`, `pkg/engine/browser/launch_test.go` (`TestLaunchWithAKeptProfile`), `pkg/engine/tools/browser_test.go` (`TestBrowserToolUsesTheWorkspacesProfile`), `apps/service/internal/server/browserprofiles_test.go`, `apps/desktop/web/src/BrowserSettings.test.ts`, `apps/electron/src/logic.test.ts` |
| Depends on | [spec_web_008](spec_web_008.md) (the browser, WEB-30–38), [spec_electron_038](spec_electron_038.md) (the window), [spec_project_config_031](spec_project_config_031.md) (trust) |

## 1. Purpose and what the research found

The agent's browser had a throwaway profile, deleted on close (WEB-30), so it was signed in nowhere, and the user's own sign-ins (a mailbox, a wiki behind SSO, a dashboard) were out of its reach.

- Chromium keeps sign-ins in a profile: cookies, local storage, IndexedDB. A `--user-data-dir` kept between runs keeps them for the system's Chrome; Electron's `session.fromPartition("persist:<name>")` is the same for the app's own views.
- The user's everyday Chrome profile is out of reach: Chrome 136 and later refuse `--remote-debugging-port` on the default profile, and its cookies are encrypted with the OS keychain. Profiles are Blitz's own, signed in to by hand.
- Google (and some others) refuse sign-in in embedded or automated browsers ("This browser or app may not be secure"); a profile doesn't change that. Blitz's own OAuth (MCP servers, `pkg/mcpauth`) stays as RFC 8252 says: the system browser and a loopback redirect.
- Driving the user's own Chrome through an extension and native messaging (as Claude in Chrome does) was left out of v0.8.4 (the owner, 2026-10-23; `.agents/TODO.md`).

## 2. Profiles

- **BP-01** `config.BrowserProfiles`: `browser-profiles.json` in the settings directory lists the profiles (name: lowercase letters, digits, `-` and `_`, up to 32; when made; the sites it browses without asking) and each workspace's choice, by its folder with symbolic links resolved. Each profile's data is `browser/profiles/<name>` beside it, 0700. A profile is the person's: no project setting names one (WEB-38).
- **BP-02** `ConfigService`: `ListBrowserProfiles` (with a workspace's choice), `CreateBrowserProfile`, `DeleteBrowserProfile` (its data and the workspaces' choice too), `ClearBrowserProfile` (signed out of everything, the profile kept), `SetBrowserProfileDomains`, `SetWorkspaceBrowserProfile` (`""`: none), and `OpenBrowserProfile`, which starts the system's Chrome (or Chromium, Edge, Brave) on the profile's folder at a web page, on the service's machine, to sign in by hand (`NO_BROWSER` without one). A name that's gone is `NOT_FOUND`, `NO_BROWSER_PROFILE`.

## 3. Browsing with one

- **BP-03** The browser tool takes the workspace's profile before each action (`syncProfile`): Chrome is launched with its folder (`Options.ProfileDir`; the last run's `DevToolsActivePort` is removed first, and `Close` keeps the folder), so the sign-ins last; a change of profile closes the browser, and the next action starts one with the new one. None when the workspace has none, or its project settings are new, changed or declined (a project the person hasn't trusted could steer the agent into their sign-ins): a throwaway profile, as before.
- **BP-04** With a profile, the browser's approvals use the profile's own sites in place of `web.allow_domains` (its sign-ins go with every request): those go without asking, every other site asks, deny rules still refuse.
- **BP-05** In the desktop app, the agent's in-app browsing (spec_agent_browser_050) uses the profile's Electron partition, `persist:profile-<name>`; **Sign in…** opens a window on it (`SignInWindow`, EL-16), whose popups (OAuth's) open in the same session. The two stores are apart: Chrome's folder for the browsing done without the app (jobs, the CLI), the partition for the app's; the user signs in to each they'll use, and Settings says so.

## 4. The window

- **BP-06** Settings › Browser (advanced): each profile with **Sign in…** (in the app), **Sign in with Chrome…**, the site to open, its sites (one a line, saved as the field is left), **Sign out of everything** and **Delete** (asked); a field to make one. A workspace's settings › **Browser** › **Browser profile** chooses one, or none (signed out).
