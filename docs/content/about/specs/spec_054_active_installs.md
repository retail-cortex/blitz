---
aliases: ["/about/specs/spec_active_installs_054/"]
title: "054 · Active installs"
weight: 54
---

*Once a day, on a day someone uses Blitz, it tells Retail Cortex that one install was in use, and through which front ends: daily, weekly and monthly active installs, by version and platform, without knowing who anyone is* (`spec_054_active_installs`)

| | |
|---|---|
| Status | **Built** (2026-11-01: replaces the update-check counting this spec first described, 2026-10-30, before it shipped. The owner's request: "I'd like to get a call home once a day for active installs", for numbers an investor would ask for. On by default, a random install ID) |
| Source | `pkg/activity` (`Recorder`: `Mark`, `Flush`, `Use`; `InstallID`), `pkg/socket` (`Front`, `ClientHeader`: each request names its front end), `pkg/service/server/use.go` (`WithUse`, the personal calls, `RetryPings`), `pkg/service` (the service's recorder, retried hourly), `apps/cli/activity.go` (each command but `--version` and help), `pkg/pagehost` (`Used`) and `apps/desktop/web/src/activity.ts` (`useCountUse`: the window opened or brought forward), `apps/vscode/src/proxy.ts` (`Blitz-Client: vscode`), `pkg/config` (`[usage] count`, `CountsUsage`), `docs/content/guide/privacy.md`; the license server's `POST /v1/blitz/active` (license-server `spec_server_001` LS-24–LS-28) |
| Tests | `pkg/activity/activity_test.go` (`TestOncePerDayPerFront`: the fields; `TestOff`; `TestRetries`; `TestUseDoesntWait`; `TestActivityURL`; `TestInstallID`), `pkg/socket` (`TestClientNamesTheFrontEnd`), `pkg/service/server/use_test.go` (`TestCountUse`, `TestRetryPings`), `apps/cli/activity_test.go` (`TestCommandsCountUse`), `pkg/pagehost` (`TestUsed`), `apps/desktop/web/src/activity.test.ts`, `apps/vscode/src/proxy.test.ts`, `pkg/config` (`TestCountsUsage`) |
| Depends on | [spec_021_service](spec_021_service.md) (blitzd, which front ends attach to), [spec_020_cli](spec_020_cli.md) (the CLI, attached or in-process), [spec_002_config](spec_002_config.md) (settings) |

## 1. Purpose

Retail Cortex can't tell how many people use Blitz, how often, or whether they come back. Release downloads count downloads, not use; the update check (EL-90) runs at most every five days and only when the desktop app opens, so it can't give a daily number or show retention. A daily active ping can: the numbers anyone judging Blitz's traction asks for (daily, weekly and monthly active installs, how many of a month's come back each day, how many of a week's new installs are still in use weeks later) come from one small message a day.

What it must never become: a way to know who someone is, what they work on, what they ask, which models or providers they use, or anything about their machine beyond its operating system and processor.

## 2. What counts as use

- **AI-01** An install is **active** on a day (UTC) when a person used it through a front end that day:
  - **desktop**: the desktop app's window opened or brought forward;
  - **cli**: a `blitz` command run, other than `--version`, `--help` and `help`;
  - **vscode**: a session started or a prompt sent from the VS Code extension.

  Not use: the service running with no one using it, scheduled workers, status polls and health checks, the tray menu, or an update check.
- **AI-02** Each front end names itself on every request to the service (spec_021_service) with the header `Blitz-Client: desktop | cli | vscode`: the CLI and the desktop app's host through `pkg/socket`, the VS Code extension through its proxy. The service marks the day's use only on the calls a person makes (a turn, a steer, a session started or opened, an approval, an answer), never on polling, and only for a front end it knows by that header.
- **AI-03** Each front end also marks its own use, whether or not the service runs: the CLI on each command (attached or in-process, spec_020_cli), the desktop app's host when the page says its window opened or came forward (`Used`, at most every ten minutes).
- **AI-04** Marking is local and cheap: the day and the front ends used are kept in `~/.blitz/activity.json` (mode 0600), shared by the service and the CLI, written atomically. Nothing about what was done is kept.

## 3. The ping

- **AI-10** The first time a front end is used on a day, whoever marked it (the service, the CLI, or the desktop app's host) sends `POST https://license.retailcortex.com/v1/blitz/active` with exactly this JSON, and nothing else:
  - `install`: the install's ID (AI-20);
  - `day`: the day, `2006-01-02`, UTC;
  - `front`: `desktop`, `cli` or `vscode`;
  - `version`: Blitz's version (`v0.9.0`, or `dev`);
  - `os` and `arch`: Go's `GOOS` and `GOARCH`.

  So an install sends at most one ping a day for each front end it uses: one, for most people. No cookies, no headers beyond Go's own, no account, path, workspace, provider or model.
- **AI-11** It waits at most 5 seconds and never holds anything up: a person's request is answered first, and the ping goes after, on its own.
- **AI-12** A ping that fails (offline, the server down) is kept and tried again at the next use, or by the service within the hour, for up to 3 days; then it's dropped. One Retail Cortex refuses as not counted (`400`) isn't sent again. A day that was used but never reached Retail Cortex is lost, not guessed. The CLI waits at most a second for its ping before it exits; one not sent by then goes next time.
- **AI-13** `BLITZ_ACTIVITY_URL` replaces the address, for tests and a local license server; empty, the default.

## 4. The install's ID

- **AI-20** The install's ID is 16 random bytes in hex, made the first time it's needed and kept in `~/.blitz/install-id` (mode 0600). It's made from nothing: not the machine, the user, a network address or an account. Deleting the file makes a new one.
- **AI-21** One person's desktop app, CLI and VS Code extension share `~/.blitz`, so they're one install, active once a day whichever front ends they used.
- **AI-22** The ID isn't sent anywhere else, and nothing else Blitz sends (to a model provider, an MCP server, GitHub, a Blitz Enterprise service) carries it.

## 5. Switching it off

- **AI-30** It's on by default (the owner's choice, 2026-10-30, kept: "On by default, off by opting out only with a manual switch in the config"). The one switch is in the settings file, `~/.blitz/.env.toml`:

  ```toml
  [usage]
  count = false
  ```

  The service reads it at each use and the CLI at each command, so a change applies at once. Off, nothing is marked or sent, and the ID file isn't made or read. There's no switch in the window and no environment variable to turn it on; the Privacy page (AI-50) and `blitz config`'s template show it. It's a global setting: a workspace's settings can't change it.
- **AI-31** *To come with spec_053_enterprise.* A Blitz Enterprise service may switch it off for its organization's people (its policy); their own service then counts their use for their license instead (Blitz Enterprise's heartbeat).

## 6. The update check

- **AI-40** The update check counts nothing: it asks GitHub's API for the latest release, as before this spec, with no ID or parameters (EL-90, unchanged). What 054 first built (`selfupdate.Count`, the license server's `GET /v1/{product}/latest`) is removed before it ships.

## 7. What Retail Cortex keeps

The license server's side is in license-server's `spec_server_001` (LS-24–LS-28, LS-76). In short, so this repository's promise is checkable:

- **AI-50** The site's **Privacy** page (Guide › Privacy) says what Blitz sends and to whom: this ping, in plain words, with its fields and AI-30's switch; the model providers you choose; web search and MCP servers you add; OpenTelemetry if you turn it on; GitHub for updates; and nothing else. The release notes of the version that brings it say so too.
- **AI-51** Each ping is kept as the install's ID, the day, the front end, version, `os` and `arch`. Not the network address, the user agent or the request's other headers: Cloud Run's request log, which has them, isn't kept for this route.
- **AI-52** What's used from it is counts only: active installs by day, week and month, the ratio of daily to monthly, by front end, version and platform, new installs by week and how many are still active in the weeks after.
- **AI-53** It's kept for 13 months after an install's last ping, then deleted.
- **AI-54** The counts are indicative: anyone can send pings with made-up IDs. They show a trend, not an audited number.
