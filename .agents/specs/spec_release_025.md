# spec_release_025 — Continuous integration and releases

| | |
|---|---|
| Status | Implemented (reverse-engineered from `53f8c53`) |
| Source | `.github/workflows/ci.yml`, `.github/workflows/release.yml`, `.github/dependabot.yml`, `.goreleaser.yaml`, `Makefile` |
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

## 4. Open items
- The first release from `retail-cortex/blitz` still needs verifying against the new identity (MANUAL_VERIFICATION section 18). Releases made before the move (e.g. `v0.1.0`) and the `python-final` tag live in the former repository, `rmcguinness/code_puppy`, whose `go-release.yml` identity they verify against.
- macOS notarisation (needs an Apple Developer account); a desktop release job.
- Release archives don't include the `blz` symlink.
- Running opt-in real Python install tests (`BLITZ_PYENV_TESTS=1`) in CI.
