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

# Every release archive carries the license, the notice and the
# third-party notices beside the programs, and blz, a link to blitz (but
# Windows's zip, which has the tray instead).
set -euo pipefail
fail=0
for archive in "$@"; do
	case "$archive" in *.zip) # GNU tar can't list zips
		list="$(unzip -Z1 "$archive")"
		for f in LICENSE NOTICE THIRD_PARTY_NOTICES README.md blitz.exe blitzd.exe blitz-tray.exe; do
			grep -q "/$f$" <<<"$list" || { echo "✗ $(basename "$archive") has no $f"; fail=1; }
		done
		continue
		;;
	esac
	list="$(tar -tzf "$archive")"
	for f in LICENSE NOTICE THIRD_PARTY_NOTICES README.md blitz blitzd blz; do
		grep -q "/$f$" <<<"$list" || { echo "✗ $(basename "$archive") has no $f"; fail=1; }
	done
	link="$(tar -tvzf "$archive" | grep '/blz ' || true)"
	[[ "$link" == l* && "$link" == *"-> blitz" ]] || { echo "✗ $(basename "$archive"): blz isn't a link to blitz: $link"; fail=1; }
done
[ $fail -eq 0 ] && echo "✓ every archive has the programs, blz and the license files"
exit $fail
