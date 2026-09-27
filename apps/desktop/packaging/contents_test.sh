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

# The desktop app's package carries the license, the notice and the
# third-party notices: in Blitz.app's Resources, or in the .deb's
# /usr/share/doc/blitz-desktop (with Debian's copyright file).
set -euo pipefail
pkg="$1"
fail=0
if [ -d "$pkg" ]; then
	for f in LICENSE NOTICE THIRD_PARTY_NOTICES; do
		[ -s "$pkg/Contents/Resources/$f" ] || { echo "✗ Blitz.app has no Contents/Resources/$f"; fail=1; }
	done
else
	list="$(dpkg-deb -c "$pkg")"
	for f in copyright NOTICE THIRD_PARTY_NOTICES; do
		grep -q "usr/share/doc/blitz-desktop/$f$" <<<"$list" || { echo "✗ the .deb has no /usr/share/doc/blitz-desktop/$f"; fail=1; }
	done
fi
[ $fail -eq 0 ] && echo "✓ the package has the license files"
exit $fail
