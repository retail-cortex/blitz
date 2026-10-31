---
title: "054 · Update checks"
weight: 54
---

*Blitz asks Retail Cortex, not GitHub, whether there's a newer release, and those checks count how many installs are in use, by version and platform, without knowing who anyone is* (`spec_update_checks_054`)

| | |
|---|---|
| Status | **Built** (2026-10-30, for the next release; the owner's request: "I'd like to roll this in next to determine adoption"; on by default, a random install ID) |
| Source | `pkg/selfupdate/count.go` (`Count`, `Source.Counting`, `InstallID`, `CheckURL`, `CheckWait`), `selfupdate.go` (`Latest` asks the count first), `pkg/config/features.go` (`UpdatesConfig`, `CountsUpdateChecks`), `apps/tray/main.go` (the desktop app's host counts as `desktop`), `apps/cli/update.go` (`blitz update` as `cli`), `docs/content/guide/privacy.md`; the license server's `GET /v1/latest` (blitz-license-server `spec_server_001` LS-24–LS-27) |
| Tests | `pkg/selfupdate/count_test.go` (`TestCountedLatest`: the five parameters, GitHub when the check is down, wrong or slow; `TestCounting`; `TestInstallID`), `pkg/config/config_test.go` (`TestCountsUpdateChecks`) |
| Depends on | [spec_electron_038](spec_electron_038.md) EL-90–EL-93 (the app's updates), [spec_release_025](spec_release_025.md) (releases and their signatures), [spec_config_002](spec_config_002.md) (settings) |

## 1. Purpose

Blitz sends nothing to Retail Cortex today: the app asks GitHub's API for the latest release, and nobody can tell how many people use Blitz, which versions they run or on what. Release downloads count downloads, not use. The app already checks for updates (EL-90, EL-93), so the check itself is where adoption is counted: Blitz gets an answer it needs, Retail Cortex learns that one more install is in use, and nothing else is sent.

What it must never become: a way to know who someone is, what they work on, or anything about their machine beyond the operating system and processor.

## 2. The check

- **UC-01** `selfupdate.Latest` asks `https://license.retailcortex.com/v1/latest` with these parameters, and nothing else:
  - `install`: the install's ID (UC-10);
  - `version`: Blitz's version (`v0.8.11`, or `dev`);
  - `os` and `arch`: Go's `GOOS` and `GOARCH`;
  - `app`: what asks: `desktop` (the app's host, EL-90) or `cli` (`blitz update`).
  
  The answer is `{"tag": "v0.8.11", "notes": "<the release's page>"}`. No cookies, no other headers than Go's own, no file names, paths, account, provider, model or workspace.
- **UC-02** If it doesn't answer in 5 seconds, or answers with anything but the latest tag, the check asks GitHub's API as before (`releases/latest`), without the parameters. An update never waits on Retail Cortex being up.
- **UC-03** What's downloaded doesn't change: the release's files still come from GitHub and are checked against their Sigstore bundle (EL-90, REL-13). The answer only names a tag; it can't make Blitz install anything it wouldn't from GitHub, and a tag that isn't a published release fails the download as it would now.
- **UC-04** How often doesn't change: the desktop app when it opens and its last check is five days old (`CheckEvery`, EL-90), **Check now**, and `blitz update`. Nothing checks in the background, and the service (`blitzd`) never checks. So an install that's in use is counted about once in five days; one that's never opened isn't counted.
- **UC-05** `BLITZ_UPDATE_URL` (a local release, EL-90) replaces both, as now.

## 3. The install's ID

- **UC-10** The install's ID is 16 random bytes in hex, made the first time it's needed and kept in `~/.blitz/install-id` (mode 0600). It's made from nothing: not the machine, the user, a network address or an account. Deleting the file makes a new one.
- **UC-11** It's what lets a count be of installs, not of checks: one person's desktop app and CLI share `~/.blitz`, so they're one install, and an install checking twice in five days is counted once.
- **UC-12** The ID isn't sent anywhere else, and nothing else Blitz sends (to a model provider, an MCP server, a Blitz Enterprise service) carries it.

## 4. Switching it off

- **UC-20** Counting is on by default (the owner's choice, 2026-10-30: "On by default, off by opting out only with a manual switch in the config"). The one switch is in the settings file, `~/.blitz/.env.toml`:

  ```toml
  [updates]
  count = false
  ```

  Each check reads it (the app's host, the CLI), so a change applies at the next check. Off, every check goes straight to GitHub (UC-02's way) and the ID file isn't made or read. There's no switch in the window and no environment variable; the Privacy page (UC-40) and `blitz config`'s template show it. It's a global setting: a workspace's settings can't change it.
- **UC-21** **Update automatically** (the `updates` preference, EL-93) is unchanged: off, the app never checks by itself, so nothing is counted unless someone checks.
- **UC-22** *To come with spec_enterprise_053.* A Blitz Enterprise service may switch counting off for its organization's people (its policy, spec_enterprise_053); their apps still check for updates.

## 5. What Retail Cortex keeps

The license server's side is in blitz-license-server's `spec_server_001` (LS-24–LS-27). In short, so this repository's promise is checkable:

- **UC-30** Each check is kept as the install's ID, version, `os`, `arch`, `app` and when. Not the network address, the user agent or the request's other headers: Cloud Run's own request log, which has them, isn't kept for this route.
- **UC-31** What's used from it is counts only: installs in use in the last 7 and 30 days, by version, platform and app, and new installs by week.
- **UC-32** It's kept for 13 months, then deleted.
- **UC-33** The counts are indicative: anyone can send checks with made-up IDs. They show adoption's trend, not an audited number.

## 6. Documentation

- **UC-40** The site's **Privacy** page (Guide › Privacy) says what Blitz sends and to whom: the update check (this spec), the model providers you choose, web search and MCP servers you add, OpenTelemetry if you turn it on, and nothing else, with UC-20's switch. The release notes of the version that brings it say so in plain words.
