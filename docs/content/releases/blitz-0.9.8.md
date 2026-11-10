---
title: Blitz 0.9.8
weight: 3
---

Blitz on Linux, the way Linux does it: install it once, and `apt upgrade` keeps it current. Updates now bring the Blitz service along on every platform, and Gemini reads your PDFs again.

### Get started

1. **On Ubuntu or Debian**, download `blitz-desktop_0.9.8_<arch>.deb` from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.9.8) and install it once:
   ```sh
   cp blitz-desktop_0.9.8_*.deb /tmp && sudo apt install /tmp/blitz-desktop_0.9.8_*.deb
   ```
   That's the last time. From now on Software Updater, or `sudo apt update && sudo apt upgrade`, brings each new Blitz.
2. **On macOS and Windows**, Blitz updates itself as before. This time it brings the service with it, so there's nothing to restart.
3. Drop a PDF into a chat with a Gemini model and ask about it.

### What's new

**Blitz's own apt repository.**

- The `.deb` adds Blitz's signed apt repository as it installs, so Blitz updates with the rest of your system.
- Ubuntu 24.04, 26.04, 26.10 and Debian 13 all use the same repository.
- Installed from the repository, App Center shows Blitz's license (Apache-2.0) and homepage, instead of "Unknown".
- Rather set it up by hand? The [desktop guide](../products/desktop/_index.md) has the two commands, and the key's fingerprint to check.
- Don't want updates from it? Set `Enabled: no` in `/etc/apt/sources.list.d/blitz-desktop.sources`. Blitz keeps your choice when it updates.

**Coming to the Snap Store.** Each release now comes as a snap as well, built and tested with every release. Once the Snap Store has reviewed it, you'll find Blitz in Ubuntu's App Center, and snapd will keep it up to date.

**Updates bring the service along.**

- When Blitz updates, the service moves to the new version too. Before, the old one could keep running, or not come back.
- Open Blitz after `apt upgrade` and it restarts the service with the new one straight away. It asks first only if something's running.
- The update log (`~/.blitz/logs/update.log`) says when the service is running again, or why it isn't.

### Fixes

- **Gemini reads PDFs again.** A conversation with a PDF on Gemini 3.8 could stop with "Error 400 … The mime type: application/pdf is not supported for code execution". Gemini's code tool can't open PDFs, so Blitz leaves it out when a PDF is in the conversation. The shell, web search and everything else work as before.
- **Restart** for the service now runs this app's own version, even when the service was set up by an older install.

### Good to know

- If you have Blitz 0.9.7 or earlier from a `.deb`, install 0.9.8's `.deb` once by hand. Only from 0.9.8 does the package add the repository.
- apt's notice that the "Download is performed unsandboxed as root" is harmless: it only means apt can't see into your home folder. Copying the file to `/tmp` first, as above, avoids it.
- The snap is a *classic* snap, so it works with your own tools and projects. It sets up a small AppArmor profile so the window's sandbox works on Ubuntu, and removes it again with the snap.
