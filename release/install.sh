#!/usr/bin/env bash
# Copyright 2026 Retail Cortex
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Installs Blitz from its GitHub release (spec_parity_027 PAR-MOD-06):
#
#   curl -fsSL https://github.com/retail-cortex/blitz/releases/latest/download/install.sh | sh
#
# It downloads the archive for this machine and the release's
# checksums.txt, checks the checksums' signature with cosign (when it's
# installed; BLITZ_REQUIRE_SIGNATURE=1 makes that a must) and the
# archive's checksum, and installs blitz, blitzd and blz into BLITZ_BIN
# (default ~/.local/bin). BLITZ_VERSION picks a release (v0.1.0; default
# the latest). BLITZ_DOWNLOAD_URL and BLITZ_COSIGN are for tests.

set -eu

REPO="retail-cortex/blitz"
IDENTITY='^https://github.com/retail-cortex/blitz/\.github/workflows/release\.yml@refs/tags/v'
ISSUER="https://token.actions.githubusercontent.com"
BIN="${BLITZ_BIN:-$HOME/.local/bin}"

say() { printf '%s\n' "$*" >&2; }
die() { say "blitz install: $*"; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "it needs $1"; }

need curl
need tar

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) die "no release for $(uname -s): download one from https://github.com/$REPO/releases" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) die "no release for $(uname -m)" ;;
esac

tag="${BLITZ_VERSION:-}"
if [ -z "$tag" ]; then
  tag="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)"
  [ -n "$tag" ] || die "couldn't find the latest release"
fi
version="${tag#v}"
archive="blitz_${version}_${os}_${arch}.tar.gz"
base="${BLITZ_DOWNLOAD_URL:-https://github.com/$REPO/releases/download/$tag}"
cosign="${BLITZ_COSIGN:-cosign}"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
say "Downloading Blitz $tag for $os/$arch…"
for f in "$archive" checksums.txt checksums.txt.sigstore.json; do
  curl -fsSL -o "$tmp/$f" "$base/$f" || die "couldn't download $f from $base"
done

if command -v "$cosign" >/dev/null 2>&1; then
  "$cosign" verify-blob --bundle "$tmp/checksums.txt.sigstore.json" \
    --certificate-identity-regexp "$IDENTITY" --certificate-oidc-issuer "$ISSUER" \
    "$tmp/checksums.txt" >/dev/null 2>&1 || die "the checksums' signature doesn't verify: not installing"
  say "The checksums are signed by Blitz's release workflow."
elif [ "${BLITZ_REQUIRE_SIGNATURE:-}" = 1 ]; then
  die "BLITZ_REQUIRE_SIGNATURE=1 and cosign isn't installed"
else
  say "cosign isn't installed, so the checksums' signature isn't checked (install cosign, or set BLITZ_REQUIRE_SIGNATURE=1 to insist)."
fi

want="$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")"
[ -n "$want" ] || die "$archive isn't in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
  got="$(sha256sum "$tmp/$archive" | awk '{ print $1 }')"
else
  got="$(shasum -a 256 "$tmp/$archive" | awk '{ print $1 }')"
fi
[ "$got" = "$want" ] || die "$archive's checksum doesn't match: not installing"

tar -xzf "$tmp/$archive" -C "$tmp"
dir="$tmp/blitz_${os}_${arch}"
mkdir -p "$BIN"
for p in blitz blitzd; do
  install -m 0755 "$dir/$p" "$BIN/$p.new"
  mv -f "$BIN/$p.new" "$BIN/$p"
done
ln -sf blitz "$BIN/blz"
if [ "$os" = darwin ]; then
  xattr -d com.apple.quarantine "$BIN/blitz" "$BIN/blitzd" 2>/dev/null || true
fi

say "Installed Blitz $tag in $BIN (blitz, blitzd, blz)."
case ":$PATH:" in
  *":$BIN:"*) ;;
  *) say "Add $BIN to your PATH, e.g. echo 'export PATH=\"$BIN:\$PATH\"' >> ~/.profile" ;;
esac
say "Next: blitz setup"
