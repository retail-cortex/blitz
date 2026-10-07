---
title: "025 · Release"
weight: 25
---

*Continuous integration and releases* (`spec_release_025`)

| | |
|---|---|
| Status | Implemented; on Bazel since 2026-09-27 ([spec_monorepo_028](spec_monorepo_028.md)) |
| Source | `.github/workflows/ci.yml`, `.github/workflows/release.yml`, `.github/release-footer.md`, `.github/dependabot.yml`, `release/BUILD.bazel`, `apps/desktop/packaging/`, `build/`, `tools/` |
| Depends on | [spec_setup_001](spec_setup_001.md), [spec_monorepo_028](spec_monorepo_028.md), all feature specs |

## 1. Purpose

Every change is checked on macOS and Linux, including the sandboxes; releases are reproducible, signed and come with SBOMs.

## 2. CI (`ci.yml`)

- **REL-01** Triggered on push to `main` and pull requests, except documentation-only changes. The runners' Bazelisk runs the Bazel in `.bazelversion`; `bazel-contrib/setup-bazel` caches Bazelisk, the repository cache and a disk cache per OS, saved only from `main` (another branch's would serve only it, and could evict main's from GitHub's 10 GB). `release.yml` restores main's disk caches (`ci-ubuntu-latest`, `ci-macos-latest`) without saving, so a release builds again only what the version stamp changes; ARM Linux, which CI doesn't build, has none. No other toolchain is installed: Bazel brings Go, Node and the tools.
- **REL-02** `build` job on `ubuntu-latest` and `macos-latest`, every step named: `tools/check_format.sh` (gofmt, BUILD files current, license headers), `bazel test --config=race //...` (vet and staticcheck run in every compile, MR-16; on Linux through `tools/coverage.sh`, which fails below the coverage floor, spec_release_readiness_030 RR-43), `bazel run //tools:check_deps` (MR-02), the third-party notices, specs and code owners checks, govulncheck on the built `blitz` and `blitzd` (fails only on a vulnerability the code reaches).
- **REL-03** Linux installs bubblewrap, GTK and WebKitGTK (for the desktop app's cgo) and a pinned gVisor (`runsc`, checksum-verified). The OS-sandbox enforcement test (`TestOSSandboxEnforcement`) and gVisor tests (`TestGVisor*`) **must not be skipped** — the job fails if their verbose output contains `--- SKIP`.
- **REL-04** Protos (Linux): lint is a test (`blitzv1_proto_lint`); `bazel run //tools:buf -- format --exit-code -d`; and `buf breaking` against the previous commit (full history fetched), unless the commit message declares `Breaking-API: <why>`.
- **REL-05** The desktop app and its page are built and tested by `bazel test //...` like everything else.
- **REL-06** `reproducible` job: the release archives' checksums (`//release:archives`) from the Linux and macOS runners must be identical (MR-25).
- **REL-07** Actions are pinned by commit SHA; Dependabot keeps them current.

## 3. Release (`release.yml`)

- **REL-10** Tagging `v*` runs the release on Ubuntu: `bazel build --config=release //release:archives` (stamped with the tag, MR-20).
- **REL-11** Binaries: `blitz` and `blitzd`, pure Go, for darwin/linux × amd64/arm64 and windows/amd64 (MR-21), built on one machine.
- **REL-12** Archives `blitz_<version>_<os>_<arch>.tar.gz` (`.zip` on Windows), each holding the folder `blitz_<os>_<arch>/` with `blitz`, `blitzd`, `blz` (not on Windows), README, LICENSE and NOTICE (MR-22); an SPDX SBOM per archive (syft, pinned); `checksums.txt`.
- **REL-13** The checksum file is signed keylessly with cosign via GitHub OIDC (`id-token: write`), bundle `checksums.txt.sigstore.json`. Users verify against **this repository's release workflow identity** (`^https://github.com/retail-cortex/blitz/\.github/workflows/release\.yml@refs/tags/v`, issuer `https://token.actions.githubusercontent.com`), then `sha256sum --ignore-missing -c checksums.txt`.
- **REL-14** The release is a draft (`gh release create --draft --verify-tag`), with GitHub's generated notes and the footer in `.github/release-footer.md` (verifying, the desktop packages, clearing macOS quarantine since the CLI binaries aren't notarised).

- **REL-15** Installing and updating (PAR-MOD-06). `release/install.sh` is attached to each release (`releases/latest/download/install.sh`): on macOS and Linux (amd64, arm64) it downloads the archive for the machine, `checksums.txt` and its bundle for the latest release (`BLITZ_VERSION` another), verifies the bundle with cosign against REL-13's identity when cosign is installed (a failure installs nothing; `BLITZ_REQUIRE_SIGNATURE=1` makes cosign a must), checks the archive's SHA-256, and puts `blitz` and `blitzd` in `BLITZ_BIN` (`~/.local/bin`) by renaming over them, with `blz` linked and macOS's quarantine flag cleared, then says whether the folder is on `PATH`. `//release:install_test` runs it against a fake release.
- **REL-16** Homebrew: when a release is published (not a pre-release; or by hand for a tag), `homebrew.yml` downloads its `checksums.txt`, verifies it with cosign, writes `Formula/blitz.rb` with `release/homebrew_formula.sh` (the four macOS and Linux archives with their checksums; installs `blitz`, `blitzd`, `blz` and the license files; tests `blitz --version`) and pushes it to `retail-cortex/homebrew-tap` with the `HOMEBREW_TAP_TOKEN` secret, doing nothing without one. `//release:homebrew_formula_test` checks the formula.
- **REL-17** `blitz update` (`--check`, `--version vX`, `--skip-signature`): the latest release from GitHub's API; a newer one (semantic versions; a pre-release is older than its release) is downloaded for the machine, `checksums.txt` verified with `cosign verify-blob` against REL-13's identity (cosign not installed: a usage error unless `--skip-signature`, which still checks the checksum), the archive's checksum checked, and `blitz` (the running executable, links resolved) and the `blitzd` beside it replaced by renaming new files over them (on Windows the old ones are moved aside). A copy under Homebrew's `Cellar` or `/usr` is refused with how to update it; a development build needs `--version`. Afterwards it says to restart the service if it runs.

## 3a. Desktop release (`release.yml` `desktop` job)

- **REL-20** After the release job, a matrix on `macos-latest`, `ubuntu-24.04` and `ubuntu-24.04-arm` builds the desktop package with `--config=release` and attaches it to the draft release.
- **REL-21** macOS: `bazel build //apps/desktop/packaging:Blitz.app` (universal, MR-23), then `apps/desktop/packaging/sign_macos.sh` makes `Blitz_<version>_macos_universal.dmg` with the app and a link to `/Applications`. With a signing identity, `blitz` and `blitzd` and then the app are signed with the hardened runtime and a secure timestamp (no entitlements), the image is signed, notarised with `notarytool` (App Store Connect API key, waiting up to 30 min), stapled and validated, and `spctl` must accept it. Without one, the app is signed ad hoc and the job warns.
- **REL-22** Signing secrets: `MACOS_CERTIFICATE` (the Developer ID Application certificate and key as a base64 `.p12`), `MACOS_CERTIFICATE_PASSWORD`, `DESKTOP_SIGN_IDENTITY` (`Developer ID Application: <name> (<team>)`), `NOTARY_KEY` (the `.p8` text), `NOTARY_KEY_ID`, `NOTARY_ISSUER`. The certificate goes into a throwaway keychain, deleted at the end of the job; without `MACOS_CERTIFICATE` nothing is signed.
- **REL-23** Linux: `bazel build //apps/desktop/packaging:deb` (MR-24), named `blitz-desktop_<version>_<amd64|arm64>.deb`; `Depends: bubblewrap, libgtk-3-0t64 | libgtk-3-0, libwebkit2gtk-4.1-0`. The job installs the package with apt and fails if a library isn't found, `blitz` or `blitzd` doesn't run, or the link is wrong. The CLI isn't put on `PATH` (the CLI archives do that).
- **REL-24** Each package is signed keylessly with cosign like the checksum file (REL-13), with its own bundle `<file>.sigstore.json`, verified against the same workflow identity. The packages aren't in `checksums.txt`.
- **REL-25** Windows has no desktop package: the service installs only on macOS and Linux (launchd, systemd).
- **REL-26** Arch Linux: `bazel build //apps/desktop/packaging:pacman`, a `.pkg.tar.zst` with the `.deb`'s programs, link, launcher and icons, and the notices in `/usr/share/licenses/blitz-desktop` (`license = Apache-2.0`). rules_pkg has no pacman rule: `tools/pacman` writes the `.PKGINFO` and `.MTREE` makepkg would, owns every entry by root and adds the parent directories, and bazel_lib's zstd compresses it. `depend`: `bubblewrap`, `gtk3`, `webkit2gtk-4.1>=2.40`. It's named `blitz-desktop-<pkgver>-1-<x86_64|aarch64>.pkg.tar.zst`, where `pkgver` is the version without a pre-release's `-` (`0.4.0rc1`, as Arch writes them, which `vercmp` orders before `0.4.0`; with a separator it would come after) and with anything else pacman refuses made `.`. On x86_64 the job installs it with `pacman -U` in an `archlinux` container and fails if a dependency or library isn't found, a program doesn't run, the link is wrong, `pacman -Qkk` finds a file unlike its `.MTREE`, or removing it leaves `/usr/lib/blitz-desktop`. The aarch64 package (Arch Linux ARM) is built but not installed: there's no arm64 `archlinux` image.

## 4. Open items
- The `retail-cortex/homebrew-tap` repository and its `HOMEBREW_TAP_TOKEN` secret don't exist yet: until they do, the Homebrew job does nothing.
- The first release from `retail-cortex/blitz` still needs verifying against the new identity (MANUAL_VERIFICATION section 18). Releases made before the move (e.g. `v0.1.0`) and the `python-final` tag live in the former repository, `rmcguinness/code_puppy`, whose `go-release.yml` identity they verify against.
- macOS notarisation of the CLI archives. The desktop image is notarised once the signing secrets (REL-22) are set; the first signed release needs checking (MANUAL_VERIFICATION §18).
- The Arch package (REL-26) on a real Arch desktop: the launcher, the icon and the tray, which the container smoke test can't show.
- Running opt-in real Python install tests (`BLITZ_PYENV_TESTS=1`) in CI.
- Neither workflow has run on GitHub since the move to Bazel: the first push and the first tag need watching (MANUAL_VERIFICATION §18).
