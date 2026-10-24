---
title: Blitz 0.6.1
weight: 10
---

**Closing the window, cleanly, on NVIDIA.** Download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.6.1).

**v0.6.1 is a quick fix for the Linux desktop app on NVIDIA graphics cards.** In 0.6.0, closing the Blitz window could crash the part of the app that draws the page, and your desktop's crash reporter (DrKonqi on KDE, Apport on Ubuntu) popped up about every other time you quit. Your work was never at risk, because the crash came after the window had already closed. Now Blitz closes cleanly every time.

**Who should update:** anyone using the Linux desktop app with an NVIDIA card. Nothing changes on macOS, on other Linux graphics cards, or in the command-line tools, so updating there is optional.

**See it in a minute:**

1. Install the `.deb` or the Arch Linux package from the release, and start Blitz.
2. Close the window, then start Blitz again. Do this a few times.
3. No crash reports appear, and `coredumpctl list` shows no new `WebKitWebProcess` entries.

### What's fixed

- **Quitting never crashes on NVIDIA.** When you close the window, Blitz now shuts down the web page's process first, before the graphics driver can trip over it on the way out.
- **Unsaved changes still come first.** If the editor has unsaved changes, Blitz asks before closing, just as before. Cancel, and nothing closes.
- **The graphics card stays on.** You don't need to turn off **Use the graphics card** to avoid the crash.

### Good to know

- Your settings and workspaces carry over: install the update over 0.6.0.
- Nothing you type is lost. The page doesn't save anything as it closes, so ending its process early loses nothing.
- Tested on an NVIDIA RTX 4090 (driver 580) with KDE on Wayland: 0.6.0 crashed on 3 of 5 closes, 0.6.1 on none of 14.
