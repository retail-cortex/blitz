---
title: Blitz 1.1.1
weight: 2
---

Your agents browse signed in, and your settings take effect. Sign in to a site in one of your browser profiles, and the agent can now actually use it: ask it to check your inbox, pull a report from a dashboard behind single sign-on, or file a ticket, and it browses with your sign-ins.

### Get started

1. Update Blitz. It updates itself, or run `sudo apt upgrade` or `brew upgrade --cask blitz`, or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v1.1.1).
2. In **Settings › Browser**, make a profile and **Sign in…** to the sites you want the agent to use.
3. Ask the agent to do something on one of those sites. With one profile, it's signed in right away; with several, it asks once which one to use.

### Fixed

- **The agent can drive your browser profiles.** A profile made in Settings › Browser used to do nothing until it was chosen in a workspace's advanced settings, and the agent couldn't pick one. Now the agent can browse with any of your profiles: it asks you first, once a chat, naming the profile and its sites. The profile you choose for a workspace it uses there without asking.
- **Your only profile is the default.** With one profile, every workspace browses with it unless you choose **Anonymous** (signed out) for that workspace. With several, the agent offers the one whose sites include the page.
- **Choosing a workspace's profile is easy to find.** It's in the workspace's settings under **Browser**, no longer hidden under Advanced.
- **Reading your signed-in sites.** When the agent reads a page on one of your profile's sites, it opens it in the browser with your sign-ins instead of fetching it signed out and getting the login page.
- **Pages it opens for you are signed in too.** Ask the agent to open a site in Blitz's browser with one of your profiles, and the tab it opens uses that profile's sign-ins.
- **Settings take effect.** The Blitz service reads your settings when it starts, so changes used to wait for a restart nobody asked for. Now, when you close Settings after changing something, Blitz offers to restart the service, and does it in a few seconds.
- **You can see the service restarting.** While it restarts or starts, the window says so with a progress bar, instead of offering a Start button for a service that's already on its way.
- **No more silent signed-out browsing.** The agent is told when it's browsing signed out and which profiles you have, when a workspace's profile waits for you to trust the project's settings, and when it's using Chrome's copy of a profile (from the command line or a job), whose sign-ins are separate from the app's.
