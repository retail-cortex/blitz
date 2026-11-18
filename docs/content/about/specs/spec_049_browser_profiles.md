---
aliases: ["/about/specs/spec_browser_profiles_049/"]
title: "049 · Browser profiles"
weight: 49
---

*Browser profiles: the sign-ins (OAuth, single sign-on, a site's own) the agents browse with, one chosen per workspace* (`spec_049_browser_profiles`)

| | |
|---|---|
| Status | **Built** (v0.8.4, the owner's request: "research if we can expose the profile features of Chromium, since we want to take advantage of OAuth and other web protocols and authentications"; one profile per workspace, the owner's choice 2026-10-23) |
| Source | `pkg/config/browserprofiles.go`, `pkg/engine/browser/browser.go` (`Options.ProfileDir`), `pkg/engine/tools/browser.go` (`syncProfile`, `webRules`), `browserProfile` in `pkg/engine/workspace.go`, `pkg/service/server/browserprofiles.go`, `ConfigService.*BrowserProfile*` in `proto/blitz/v1/config.proto`; the page's `BrowserSettings.tsx` and `WorkspaceSettingsForm.tsx`; the Electron app's `SignInWindow` (`apps/electron/src/main.ts`) |
| Tests | `pkg/config/browserprofiles_test.go`, `pkg/engine/browser/launch_test.go` (`TestLaunchWithAKeptProfile`), `pkg/engine/tools/browser_test.go` (`TestBrowserToolUsesTheWorkspacesProfile`), `pkg/service/server/browserprofiles_test.go`, `apps/desktop/web/src/BrowserSettings.test.ts`, `apps/electron/src/logic.test.ts` |
| Depends on | [spec_008_web](spec_008_web.md) (the browser, WEB-30–38), [spec_038_electron](spec_038_electron.md) (the window), [spec_031_project_config](spec_031_project_config.md) (trust) |

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
- **BP-03a** *2026-11-05 (scan N26-engine-top-2#20: the profile followed trust as the workspace opened, and the language servers trust as it is now, so forgetting trust kept the profile and `--trust-project` withheld both).* The browser profile and the language servers ask one question (`Workspace.projectTrusted`): no project files, or trusted now, or this run trusts them (`--trust-project`, its files loaded). Trust forgotten while the workspace is open withholds both from the next action.
- **BP-03b** *Built 2026-11-18 (the owner: "I can create a profile … log into sites with that profile. What I can't do is drive that profile from the agent, which is the entire reason for having the profiles").* The agent may browse with **any** of the person's profiles: the browser tool's `profile` names one (`none`: signed out), kept for the chat (`browserTool.chosen`). The workspace's own goes without asking; any other, or the workspace's own while it's withheld (BP-03a), is asked first (`browser:profile:<name>`, a network approval naming its sites), and refused is an error. A name that isn't one is an error listing the person's profiles (`Registry.SetBrowserProfiles`: the workspace's, all, and the withheld one).
- **BP-03c** *Built 2026-11-18.* The agent is told, once a chat, in a navigate's note: browsing signed out while the person has profiles, which ones with their sites, and how to use one; the workspace's own withheld until its project settings are trusted; and browsing in Chrome's copy of a profile (BP-05: no app window), whose sign-ins made with **Sign in…** aren't in it. The tool's description says it browses signed in with a profile, for anything behind a sign-in.
- **BP-04** With a profile, the browser's approvals use the profile's own sites in place of `web.allow_domains` (its sign-ins go with every request): those go without asking, every other site asks, deny rules still refuse.
- **BP-05** In the desktop app, the agent's in-app browsing (spec_050_agent_browser) uses the profile's Electron partition, `persist:profile-<name>`; **Sign in…** opens a window on it (`SignInWindow`, EL-16), whose popups (OAuth's) open in the same session. The two stores are apart: Chrome's folder for the browsing done without the app (jobs, the CLI), the partition for the app's; the user signs in to each they'll use, and Settings says so.

## 4. The window

- **BP-06** Settings › Browser: each profile with **Sign in…** (in the app), **Sign in with Chrome…**, the site to open, its sites (one a line, saved as the field is left), **Sign out of everything** and **Delete** (asked); a field to make one. A workspace's settings › **Browser** › **Browser profile** chooses one, or none (signed out), in simple mode too (*2026-11-18*: it was under Advanced, so a profile made in Settings was never used).
- **BP-07** *Built 2026-11-18.* `web_fetch` of a page whose site is one of the profile in use's sites loads it in the browser, signed in (`webFetcher.signedIn`; `Via: browser`, a note naming the profile), under the browser's approvals (BP-04), not as a plain request, which carries no sign-ins and would get the site's login page. The browser failing falls back to the plain request.
- **BP-08** *Built 2026-11-18 (the owner's session: "open the internal browser using the work profile" went to `open_page`, which couldn't).* `open_page` shows a page signed in with a profile: its `profile` names one (asked first unless it's the workspace's own, BP-03b; `none`: signed out), else the one the chat browses with. The page command carries it (`AgentBrowserCommand.profile`), and the app's web tab opens in the profile's partition (`PreviewOpen`'s profile, `persist:profile-<name>`) rather than the previews' own. The system's browser has its own sign-ins: the result's note says so.
