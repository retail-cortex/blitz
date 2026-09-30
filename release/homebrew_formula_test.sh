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

# The formula names each platform's archive with its checksum, and a
# missing archive is an error.

set -euo pipefail

script="$1"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
for p in darwin_arm64 darwin_amd64 linux_arm64 linux_amd64; do
  echo "$(printf '%s' "$p" | shasum -a 256 2>/dev/null | cut -c1-64 || printf '%s' "$p" | sha256sum | cut -c1-64)  blitz_1.2.3_$p.tar.gz"
done > "$work/checksums.txt"

bash "$script" v1.2.3 "$work/checksums.txt" > "$work/blitz.rb"
grep -q 'version "1.2.3"' "$work/blitz.rb" || { echo "FAIL: version"; exit 1; }
for p in darwin_arm64 darwin_amd64 linux_arm64 linux_amd64; do
  grep -q "releases/download/v1.2.3/blitz_1.2.3_$p.tar.gz" "$work/blitz.rb" || { echo "FAIL: $p url"; exit 1; }
  sum="$(awk -v f="blitz_1.2.3_$p.tar.gz" '$2 == f { print $1 }' "$work/checksums.txt")"
  grep -q "sha256 \"$sum\"" "$work/blitz.rb" || { echo "FAIL: $p checksum"; exit 1; }
done
if ruby -c "$work/blitz.rb" >/dev/null 2>&1 || ! command -v ruby >/dev/null; then :; else echo "FAIL: not Ruby"; exit 1; fi

grep -v linux_amd64 "$work/checksums.txt" > "$work/partial.txt"
if bash "$script" 1.2.3 "$work/partial.txt" > /dev/null 2>&1; then
  echo "FAIL: a missing archive wasn't an error"; exit 1
fi
echo PASS
