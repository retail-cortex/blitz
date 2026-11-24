---
title: Blitz 1.1.2
weight: 5
---

Blitz 1.1.1 for Linux on ARM. Everything in [1.1.1](blitz-1.1.1.md), with the agents browsing signed in with your browser profiles and settings that take effect when you close Settings, now on ARM Linux machines too: the desktop app's `.deb`, the Arch Linux package and the tarball.

### Get started

1. Update Blitz. It updates itself, or run `sudo apt upgrade` or `brew upgrade --cask blitz`, or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v1.1.2).
2. On Linux on ARM, install `blitz-desktop_*_arm64.deb` (`sudo apt install ./blitz-desktop_*_arm64.deb`), the Arch package, or the tarball.

### Fixed

- **The desktop app for Linux on ARM is back.** 1.1.1's ARM packages didn't build: Ubuntu had replaced a graphics library the build's check installs. The release now refreshes its package lists first, so every platform gets its packages.
