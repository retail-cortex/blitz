# spec_release_025 — Continuous integration and releases

| | |
|---|---|
| Status | Implemented; on Bazel since 2026-09-27 ([spec_monorepo_028](spec_monorepo_028.md)) |
| Source | `.github/workflows/ci.yml`, `.github/workflows/release.yml`, `.github/release-footer.md`, `.github/dependabot.yml`, `release/BUILD.bazel`, `apps/desktop/packaging/`, `build/`, `tools/` |
| Depends on | [spec_setup_001](spec_setup_001.md), [spec_monorepo_028](spec_monorepo_028.md), all feature specs |

## 1. Purpose

Every change is checked on macOS and Linux, including the sandboxes; releases are reproducible, signed and come with SBOMs.

## 2. CI (`ci.yml`)

- **REL-01** Triggered on push to `main` and pull requests, except documentation-only changes. The runners' Bazelisk runs the Bazel in `.bazelversion`; `bazel-contrib/setup-bazel` caches Bazelisk, the repository cache and a disk cache per OS. No other toolchain is installed: Bazel brings Go, Node and the tools.
- **REL-02** `test` job on `ubuntu-latest` and `macos-latest`: `tools/check_format.sh` (gofmt, BUILD files current), `bazel test --config=race //...` (vet and staticcheck run in every compile, MR-16), `bazel run //tools:check_deps` (MR-02), govulncheck on the built `blitz` and `blitzd` (fails only on a vulnerability the code reaches).
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

## 3a. Desktop release (`release.yml` `desktop` job)

- **REL-20** After the release job, a matrix on `macos-latest`, `ubuntu-24.04` and `ubuntu-24.04-arm` builds the desktop package with `--config=release` and attaches it to the draft release.
- **REL-21** macOS: `bazel build //apps/desktop/packaging:Blitz.app` (universal, MR-23), then `apps/desktop/packaging/sign_macos.sh` makes `Blitz_<version>_macos_universal.dmg` with the app and a link to `/Applications`. With a signing identity, `blitz` and `blitzd` and then the app are signed with the hardened runtime and a secure timestamp (no entitlements), the image is signed, notarised with `notarytool` (App Store Connect API key, waiting up to 30 min), stapled and validated, and `spctl` must accept it. Without one, the app is signed ad hoc and the job warns.
- **REL-22** Signing secrets: `MACOS_CERTIFICATE` (the Developer ID Application certificate and key as a base64 `.p12`), `MACOS_CERTIFICATE_PASSWORD`, `DESKTOP_SIGN_IDENTITY` (`Developer ID Application: <name> (<team>)`), `NOTARY_KEY` (the `.p8` text), `NOTARY_KEY_ID`, `NOTARY_ISSUER`. The certificate goes into a throwaway keychain, deleted at the end of the job; without `MACOS_CERTIFICATE` nothing is signed.
- **REL-23** Linux: `bazel build //apps/desktop/packaging:deb` (MR-24), named `blitz-desktop_<version>_<amd64|arm64>.deb`; `Depends: libgtk-3-0t64 | libgtk-3-0, libwebkit2gtk-4.1-0`. The job installs the package with apt and fails if a library isn't found, `blitz` or `blitzd` doesn't run, or the link is wrong. The CLI isn't put on `PATH` (the CLI archives do that).
- **REL-24** Each package is signed keylessly with cosign like the checksum file (REL-13), with its own bundle `<file>.sigstore.json`, verified against the same workflow identity. The packages aren't in `checksums.txt`.
- **REL-25** Windows has no desktop package: the service installs only on macOS and Linux (launchd, systemd).

## 4. Open items
- The first release from `retail-cortex/blitz` still needs verifying against the new identity (MANUAL_VERIFICATION section 18). Releases made before the move (e.g. `v0.1.0`) and the `python-final` tag live in the former repository, `rmcguinness/code_puppy`, whose `go-release.yml` identity they verify against.
- macOS notarisation of the CLI archives. The desktop image is notarised once the signing secrets (REL-22) are set; the first signed release needs checking (MANUAL_VERIFICATION §18).
- Running opt-in real Python install tests (`BLITZ_PYENV_TESTS=1`) in CI.
- Neither workflow has run on GitHub since the move to Bazel: the first push and the first tag need watching (MANUAL_VERIFICATION §18).
