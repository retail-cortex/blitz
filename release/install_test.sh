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

# install.sh from a fake release: it installs a good archive, and refuses a
# tampered one, or one it can't check the signature of when told to insist.

set -euo pipefail

script="$1"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

case "$(uname -s)" in Darwin) os=darwin ;; *) os=linux ;; esac
case "$(uname -m)" in x86_64 | amd64) arch=amd64 ;; *) arch=arm64 ;; esac
rel="$work/release"
mkdir -p "$rel/blitz_${os}_${arch}"
for p in blitz blitzd; do
  printf '#!/bin/sh\necho %s 9.9.9\n' "$p" > "$rel/blitz_${os}_${arch}/$p"
  chmod +x "$rel/blitz_${os}_${arch}/$p"
done
archive="blitz_9.9.9_${os}_${arch}.tar.gz"
tar -czf "$rel/$archive" -C "$rel" "blitz_${os}_${arch}"
sum() { if command -v sha256sum >/dev/null; then sha256sum "$1"; else shasum -a 256 "$1"; fi | awk '{ print $1 }'; }
echo "$(sum "$rel/$archive")  $archive" > "$rel/checksums.txt"
echo '{}' > "$rel/checksums.txt.sigstore.json"

run() {
  env BLITZ_VERSION=v9.9.9 BLITZ_DOWNLOAD_URL="file://$rel" BLITZ_COSIGN=/no/cosign BLITZ_BIN="$work/bin" "$@" sh "$script"
}

run >"$work/out" 2>&1 || { cat "$work/out"; echo "FAIL: a good release didn't install"; exit 1; }
[ "$("$work/bin/blitz")" = "blitz 9.9.9" ] || { echo "FAIL: blitz not installed"; exit 1; }
[ -x "$work/bin/blitzd" ] && [ -L "$work/bin/blz" ] || { echo "FAIL: blitzd or blz missing"; exit 1; }
grep -q "signature isn't checked" "$work/out" || { echo "FAIL: no word about the unchecked signature"; exit 1; }

if run BLITZ_REQUIRE_SIGNATURE=1 >"$work/out" 2>&1; then
  echo "FAIL: installed without the signature it was told to require"; exit 1
fi
grep -q "cosign isn't installed" "$work/out" || { cat "$work/out"; exit 1; }

echo "0000000000000000000000000000000000000000000000000000000000000000  $archive" > "$rel/checksums.txt"
if run >"$work/out" 2>&1; then
  echo "FAIL: installed a tampered archive"; exit 1
fi
grep -q "checksum doesn't match" "$work/out" || { cat "$work/out"; exit 1; }
echo PASS
