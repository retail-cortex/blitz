---
title: Building from source
weight: 10
---

Blitz builds with [Bazel](https://bazel.build) 9.2. Bazel downloads Go, Node, pnpm, buf, Hugo and every dependency at pinned versions, so the machine needs only Bazelisk (which picks the Bazel version in `.bazelversion`), git, and, on macOS, Xcode (on Linux, bubblewrap for the shell sandbox).

## macOS

Tested on macOS 13 and later, Apple silicon and Intel.

1. Install Xcode from the App Store (the builds are tested with Xcode installed) and select it: `sudo xcode-select -s /Applications/Xcode.app`.
2. Install Bazelisk: `brew install bazelisk` (it installs as `bazel`).
3. Clone and build:

   ```bash
   git clone https://github.com/retail-cortex/blitz.git && cd blitz
   bazel build //apps/cli:blitz //apps/service:blitzd
   bazel-bin/apps/cli/blitz --version      # and bazel-bin/apps/service/blitzd
   ```

4. The desktop app, as `Blitz.app` for this Mac (`bazel-bin/apps/electron/packaging/darwin_arm64/Blitz.app`, or `darwin_x64` on Intel; `//apps/electron/packaging:Blitz_x64.app` builds Intel's on Apple silicon too), or run it from the build:

   ```bash
   bazel build //:mac-app
   bazel run //:desktop
   ```

## Linux

Tested on Ubuntu 24.04 (x86-64), Debian 13 and later, and Arch Linux.

1. Install the build dependencies: git, a C compiler (the tray uses cgo), and bubblewrap for the shell sandbox. Everything else, the desktop app's Electron included, Bazel downloads.

   Ubuntu 24.04 or Debian 13 and later:

   ```bash
   sudo apt-get install -y git build-essential bubblewrap
   ```

   Arch Linux:

   ```bash
   sudo pacman -S --needed git base-devel bubblewrap
   ```

2. Install Bazelisk from its [releases](https://github.com/bazelbuild/bazelisk/releases) as `bazel` on your `PATH`, for example:

   ```bash
   sudo curl -fsSLo /usr/local/bin/bazel https://github.com/bazelbuild/bazelisk/releases/latest/download/bazelisk-linux-amd64
   sudo chmod +x /usr/local/bin/bazel
   ```

3. Build as on macOS. The desktop app is a Debian package on Linux:

   ```bash
   bazel build //apps/cli:blitz //apps/service:blitzd
   bazel build //:deb
   ```

   The package is `bazel-bin/apps/electron/packaging/blitz-desktop_amd64.deb` (`_arm64` on ARM). To install it here or on another machine (Ubuntu 24.04, Debian 13 or later, the same architecture), give apt the file's path, with the `./`, or it looks for a package of that name in its repositories: `sudo apt install ./blitz-desktop_amd64.deb`. It installs the programs in `/usr/lib/blitz-desktop/`, with `blitz-desktop` on the `PATH` and a launcher in the applications menu. A local build's version is always `0.0.0` (only `--config=release` stamps one), so apt sees a newer build as the package already installed and does nothing: `sudo apt install --reinstall ./blitz-desktop_amd64.deb` (and `--allow-downgrades` over a release).

   On Arch Linux, build the Arch package instead: `bazel build //:pacman`. It's `bazel-bin/apps/electron/packaging/blitz-desktop.pkg.tar.zst`, which installs the same files, the notices in `/usr/share/licenses/blitz-desktop/`: `sudo pacman -U bazel-bin/apps/electron/packaging/blitz-desktop.pkg.tar.zst`. pacman reinstalls a package at the same version, so a local build (`0.0.0-1`) replaces another; over a release it asks before downgrading. `sudo pacman -R blitz-desktop` removes it.

4. Ubuntu 24.04 restricts unprivileged user namespaces through AppArmor, which bubblewrap and gVisor need, and Blitz requires the sandbox on Linux. For bubblewrap alone, `blitz security fix-apparmor` installs an AppArmor profile (`/etc/apparmor.d/blitz-bwrap`) with your password; that's enough to use Blitz and run its sandbox tests. The gVisor tests need `runsc` to have them too: `sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0` lifts the restriction for every program until the next boot, as CI does.

## Windows

Building on Windows isn't supported. Build in WSL 2 with the Linux steps, or cross-compile: the release archives and the desktop app's zip, Windows included, build on macOS or Linux:

```bash
bazel build //release:archives          # every platform's archive
bazel build //:windows-zip              # the desktop app: bazel-bin/apps/electron/packaging/Blitz_windows_x64.zip
```

On Windows, Blitz runs without the OS sandbox and the process guard; skill scripts don't run.

## Tests and checks

```bash
bazel test //...                        # every test: Go, the desktop page, the protos
bazel test --config=race //...          # the Go tests with the race detector, as CI runs them
tools/check_format.sh                   # formatting, BUILD files and license headers
bazel run //tools:check_deps            # the dependency rules between apps and packages
```

Some tests need more than the build: the gVisor tests need `runsc` (`RUNSC_PATH`), and the sandbox tests on Linux need bubblewrap and user namespaces. They skip without them locally; CI fails if they're skipped. Everything else passes without a working sandbox (the tests run with `BLITZ_SANDBOX_SHELL=auto`, set in `.bazelrc`, since settings default to `required` on Linux): tests that need the agent's tools to run without asking use `configtest.RunTools` (`pkg/config/configtest`), allow rules that work where bypass mode, which needs the sandbox, falls back to asking.

## Release builds

`--config=release` stamps the version from `git describe`:

```bash
bazel build --config=release //release:archives
```

The archives come out byte for byte the same on macOS and Linux; CI builds them on both and compares. The [build architecture](../architecture/build.md) explains how.

## The docs

This site is in `docs/`, built with Hugo through Bazel:

```bash
bazel build //docs:site                 # the site, in bazel-bin/docs/site
bazel run //:docs                       # a local preview at http://localhost:1313
```
