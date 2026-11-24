---
title: Blitz 1.3.2
weight: 1
---

Blitz counts your install again, and your organization's models stop running out after a week. Since 0.8.11, each released Blitz has said once a day that it was in use, and Retail Cortex has turned every one of those messages away over how the version was written. This release fixes that.

### Get started

1. Update Blitz. It updates itself, or run `sudo apt upgrade` or `brew upgrade --cask blitz`, or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v1.3.2).
2. Nothing else. The once-a-day message goes the first time you use Blitz each day, as [Privacy](../guide/privacy.md) describes. It is still off with `[usage] count = false` in `~/.blitz/.env.toml`.

### Fixed

- **Active installs are counted.** The daily message gave Blitz's version as `1.3.1`, and Retail Cortex only took `v1.3.1`. It refused the message, and Blitz never resends a refused message, so released installs weren't counted at all. Now the version goes as `v1.3.1`, a build that isn't a release says `dev`, and Retail Cortex takes either form, so installs still on an earlier version count too. The message itself is the same as before: no new fields, nothing about you.
- **Your organization's models keep working.** Blitz checks your organization's policy every few minutes, but when nothing in the policy had changed for a week, its copy still ran out, and organization models stopped with "your organization's catalog is out of date". Blitz now renews the policy before it runs out, whether it has changed or not. If you're signed out, the message now says to sign in, since nothing renews until you do.
- **A refused message is logged.** If Retail Cortex turns one away, Blitz logs a warning saying so, where before it was quiet.
