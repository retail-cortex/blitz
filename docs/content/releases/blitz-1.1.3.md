---
title: Blitz 1.1.3
weight: 1
---

Your profile's intranet sites open for the agent. Sites you list in a browser profile can now be on your company's private network, reached over a VPN or through your company's DNS. The agent browses to them signed in, as it already did for public sites.

### Get started

1. Update Blitz. It updates itself, or run `sudo apt upgrade` or `brew upgrade --cask blitz`, or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v1.1.3).
2. In **Settings › Browser**, add your internal sites to a profile's sites (`jira.example.com`, or `*.example.com` for all of them), and **Sign in…** to them.
3. Ask the agent to do something on one of those sites.

### Fixed

- **The agent can reach your profile's internal sites.** A site among a browser profile's own sites that resolves to a private address (an intranet over a VPN, or a name your company's DNS answers privately) used to fail with `net::ERR_BLOCKED_BY_CLIENT`, while the same profile's public sites loaded. No approval was asked, because the site was already the profile's. The browser's network filter refused it anyway, since it only let private addresses through with `browser.allow_local`. Now a profile's own sites may be on your private network, both in the desktop app's browser and in Chrome. Other sites are still held to public addresses. `localhost` and loopback still need `browser.allow_local`, and cloud metadata addresses are never reached.
