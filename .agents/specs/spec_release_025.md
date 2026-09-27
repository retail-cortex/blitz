# spec_release_025 — Continuous integration and releases

| | |
|---|---|
| Status | Implemented (reverse-engineered from `53f8c53`) |
| Source | `.github/workflows/ci.yml`, `.github/workflows/release.yml`, `.github/dependabot.yml`, `.goreleaser.yaml`, `Makefile`, `scripts/desktop-package.sh` |
| Depends on | [spec_setup_001](spec_setup_001.md), all feature specs |

## 1. Purpose

Every change is checked on macOS and Linux, including the sandboxes; releases are reproducible, signed and come with SBOMs.

## 2. CI (`ci.yml`)

- **REL-01** Triggered on push and pull request; `GOTOOLCHAIN=local`; Go from `go.mod`.
- **REL-02** `test` job on `ubuntu-latest` and `macos-latest`: vet, race tests, lint/vulncheck.
- **REL-03** Linux installs bubblewrap and a pinned gVisor (`runsc`). The OS-sandbox enforcement test (`TestOSSandboxEnforcement`) and gVisor tests (`TestGVisor*`) **must not be skipped** — the job fails if their output contains `--- SKIP`.
- **REL-04** Protos: buf lint, formatting, generated code current (`make proto-check`), and `buf breaking` against the previous commit (full history fetched), unless the commit message declares `Breaking-API: <why>`.
- **REL-05** `desktop` job builds the page and vets/tests the desktop module.
- **REL-06** `reproducible` job: cross-compiled binaries' checksums from the Linux and macOS runners must be identical (`diff sums-Linux.txt sums-macOS.txt`).
- **REL-07** Actions are pinned by commit SHA; Dependabot keeps them current.

## 3. Release (`release.yml`, GoReleaser)

- **REL-10** Tagging `v*` runs GoReleaser on Ubuntu (tag `python-final` ignored). Pre-hooks: `go mod tidy`, `go vet ./...`.
- **REL-11** Builds `./cmd/blitz` with `CGO_ENABLED=0`, `-trimpath`, `-s -w -X main.version=<version>`, `mod_timestamp` = commit time (reproducible), for darwin/linux/windows × amd64/arm64 except windows/arm64.
- **REL-12** Archives `blitz_<version>_<os>_<arch>` (`tar.gz`, `zip` on Windows) containing README, LICENSE and NOTICE; `checksums.txt`; SPDX SBOMs per archive.
- **REL-13** The checksum file is signed keylessly with cosign via GitHub OIDC (`id-token: write`), bundle `checksums.txt.sigstore.json`. Users verify against **this repository's release workflow identity** (`^https://github.com/retail-cortex/blitz/\.github/workflows/release\.yml@refs/tags/v`, issuer `https://token.actions.githubusercontent.com`), then `sha256sum --ignore-missing -c checksums.txt`.
- **REL-14** Releases are drafts; the changelog comes from GitHub, excluding `docs:`, `test:`, `chore:`. The footer explains clearing macOS quarantine (`xattr -d com.apple.quarantine blitz`) since binaries aren't notarised.

## 3a. Desktop release (`release.yml` `desktop` job)

- **REL-20** After GoReleaser's job, a matrix on `macos-latest`, `ubuntu-24.04` and `ubuntu-24.04-arm` runs `scripts/desktop-package.sh <tag>` (also `make desktop-package`, version from `git describe`) and attaches the result to the draft release. The app's version is the tag without its `v`: set in `wails.json` for the build (the page's About and `Info.plist`) and put back afterwards; the bundled CLI gets `-X main.version=<tag>`.
- **REL-21** macOS: `Blitz_<version>_macos_universal.dmg`, holding a universal (arm64 + x86_64) `Blitz.app` with a universal CLI bundled (both checked with `lipo`) and a link to `/Applications`. With a signing identity, the CLI and then the app are signed with the hardened runtime and a secure timestamp (no entitlements), the image is signed, notarised with `notarytool` (App Store Connect API key, waiting up to 30 min), stapled and validated, and `spctl` must accept it. Without one, the app is signed ad hoc and the job warns.
- **REL-22** Signing secrets: `MACOS_CERTIFICATE` (the Developer ID Application certificate and key as a base64 `.p12`), `MACOS_CERTIFICATE_PASSWORD`, `DESKTOP_SIGN_IDENTITY` (`Developer ID Application: <name> (<team>)`), `NOTARY_KEY` (the `.p8` text), `NOTARY_KEY_ID`, `NOTARY_ISSUER`. The certificate goes into a throwaway keychain, deleted at the end of the job; without `MACOS_CERTIFICATE` nothing is signed.
- **REL-23** Linux: `blitz-desktop_<version>_<amd64|arm64>.deb`, built against WebKitGTK 4.1 (`-tags webkit2_41`; Ubuntu 24.04, Debian 13 and later): the app and the CLI in `/usr/lib/blitz-desktop` (the app finds the CLI beside its resolved executable), `/usr/bin/blitz-desktop` linking to it, a `.desktop` launcher and the icon; `Depends: libgtk-3-0t64 | libgtk-3-0, libwebkit2gtk-4.1-0`. The job installs the package with apt and fails if a library isn't found, the CLI doesn't run, or the link is wrong. The CLI isn't put on `PATH` (the CLI archives do that).
- **REL-24** Each package is signed keylessly with cosign like the checksum file (REL-13), with its own bundle `<file>.sigstore.json`, verified against the same workflow identity. The packages aren't in `checksums.txt`.
- **REL-25** Windows has no desktop package: the service installs only on macOS and Linux (launchd, systemd), and Wails's Windows templates were removed.

## 4. Open items
- The first release from `retail-cortex/blitz` still needs verifying against the new identity (MANUAL_VERIFICATION section 18). Releases made before the move (e.g. `v0.1.0`) and the `python-final` tag live in the former repository, `rmcguinness/code_puppy`, whose `go-release.yml` identity they verify against.
- macOS notarisation of the CLI archives. The desktop image is notarised once the signing secrets (REL-22) are set; the first signed release needs checking (MANUAL_VERIFICATION §18).
- Release archives don't include the `blz` symlink.
- Running opt-in real Python install tests (`BLITZ_PYENV_TESTS=1`) in CI.
