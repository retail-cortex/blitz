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

# The cask names each disk image's checksum, takes sha256sum's output
# with or without folders, and a missing disk image is an error.

set -euo pipefail

script="$1"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
sum() { printf '%s' "$1" | shasum -a 256 2>/dev/null | cut -c1-64 || printf '%s' "$1" | sha256sum | cut -c1-64; }
arm="$(sum arm64)"
x64="$(sum x64)"
printf '%s  dl/Blitz_1.2.3_macos_arm64.dmg\n%s *Blitz_1.2.3_macos_x64.dmg\n' "$arm" "$x64" > "$work/sums.txt"

bash "$script" v1.2.3 "$work/sums.txt" > "$work/blitz.rb"
grep -q '^cask "blitz" do' "$work/blitz.rb" || { echo "FAIL: cask"; exit 1; }
grep -q 'version "1.2.3"' "$work/blitz.rb" || { echo "FAIL: version"; exit 1; }
grep -q "arm:   \"$arm\"" "$work/blitz.rb" || { echo "FAIL: arm64 checksum"; exit 1; }
grep -q "intel: \"$x64\"" "$work/blitz.rb" || { echo "FAIL: x64 checksum"; exit 1; }
grep -qF 'releases/download/v#{version}/Blitz_#{version}_macos_#{arch}.dmg' "$work/blitz.rb" || { echo "FAIL: url"; exit 1; }
grep -q 'auto_updates true' "$work/blitz.rb" || { echo "FAIL: auto_updates"; exit 1; }
grep -q 'launchctl: \["dev.blitz.service", "dev.blitz.tray"\]' "$work/blitz.rb" || { echo "FAIL: launchctl"; exit 1; }
if ruby -c "$work/blitz.rb" >/dev/null 2>&1 || ! command -v ruby >/dev/null; then :; else echo "FAIL: not Ruby"; exit 1; fi

grep -v x64 "$work/sums.txt" > "$work/partial.txt"
if bash "$script" 1.2.3 "$work/partial.txt" > /dev/null 2>&1; then
  echo "FAIL: a missing disk image wasn't an error"; exit 1
fi
echo PASS
