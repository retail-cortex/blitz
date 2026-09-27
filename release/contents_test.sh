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
# third-party notices beside the programs.
set -euo pipefail
fail=0
for archive in "$@"; do
	case "$archive" in *.zip) continue ;; esac # GNU tar can't list zips
	list="$(tar -tzf "$archive")"
	for f in LICENSE NOTICE THIRD_PARTY_NOTICES README.md; do
		grep -q "/$f$" <<<"$list" || { echo "✗ $(basename "$archive") has no $f"; fail=1; }
	done
done
[ $fail -eq 0 ] && echo "✓ every archive has the license files"
exit $fail
