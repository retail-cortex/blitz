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
# /usr/share/doc/blitz-desktop (with Debian's copyright file). On Linux
# the app is built for WebKitGTK 4.1 (webkit2_41), whose web view passes
# the page's API requests to the service whole.
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
	# Wails's legacy web view (no webkit2_41 tag) never reads a request's
	# body: the page's every API call would fail.
	dpkg-deb --fsys-tarfile "$pkg" | tar -xO --wildcards "*usr/lib/blitz-desktop/blitz-desktop" | grep -ac webkit_uri_scheme_request_get_http_body >/dev/null ||
		{ echo "✗ the .deb's app wasn't built for WebKitGTK 4.1 (the webkit2_41 tag)"; fail=1; }
fi
[ $fail -eq 0 ] && echo "✓ the package has the license files and the right web view"
exit $fail
