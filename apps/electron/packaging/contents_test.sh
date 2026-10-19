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

# The desktop app's packages (spec_electron_038 EL-80) carry Electron's
# program as Blitz's, the app with its programs (blitz-tray, blitzd,
# blitz) and the license files: the Windows zip, Blitz.app, the .deb and
# the Arch package (listing it needs zstd, which reads it from stdin).
set -euo pipefail
pkg="$1"
fail=0
no() { echo "✗ $*"; fail=1; }
if [[ "$pkg" == *.zip ]]; then
	list="$(unzip -l "$pkg" | awk '{print $4}')"
	for f in Blitz/Blitz.exe Blitz/LICENSES.chromium.html Blitz/resources/app/package.json Blitz/resources/app/dist/main.js Blitz/resources/app/page/index.html \
		Blitz/resources/app/bin/blitz-tray.exe Blitz/resources/app/bin/blitzd.exe Blitz/resources/app/bin/blitz.exe \
		Blitz/blitz-licenses/LICENSE Blitz/blitz-licenses/NOTICE Blitz/blitz-licenses/THIRD_PARTY_NOTICES; do
		grep -qx "$f" <<<"$list" || no "the zip has no $f"
	done
	grep -q '^Blitz/electron.exe$' <<<"$list" && no "the zip still has electron.exe"
	# Its name has a space, which list (a column) cuts.
	grep -q 'Blitz/Uninstall Blitz.exe$' <<<"$(unzip -l "$pkg")" || no "the zip has no Uninstall Blitz.exe"
elif [[ "$pkg" == *_setup.exe ]]; then
	# The setup program, then the zip, its length and BLITZZIP (pkg/wininstall).
	[ "$(head -c 2 "$pkg")" = "MZ" ] || no "the setup program isn't a Windows program"
	[ "$(tail -c 8 "$pkg")" = "BLITZZIP" ] || no "the setup program carries no zip"
elif [ -d "$pkg" ]; then
	r="$pkg/Contents/Resources"
	for exe in blitz blitzd blitz-tray; do
		[ -x "$r/app/bin/$exe" ] || no "Blitz.app has no Resources/app/bin/$exe"
	done
	for f in LICENSE NOTICE THIRD_PARTY_NOTICES licenses/LICENSES.chromium.html app/dist/main.js app/page/index.html electron.icns; do
		[ -s "$r/$f" ] || no "Blitz.app has no Resources/$f"
	done
	[ "$(plutil -extract CFBundleIdentifier raw "$pkg/Contents/Info.plist")" = dev.blitz.desktop ] || no "Blitz.app isn't dev.blitz.desktop"
	[ "$(plutil -extract CFBundleName raw "$pkg/Contents/Info.plist")" = Blitz ] || no "Blitz.app isn't named Blitz"
	[ -e "$r/default_app.asar" ] && no "Blitz.app still has Electron's default app"
	# The bundle itself, not the test's links to its files, which codesign
	# can't check.
	real="$(dirname "$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$pkg/Contents/Info.plist")")/.."
	codesign --verify --deep --strict "$real" 2>/dev/null || no "Blitz.app's signature doesn't verify"
else
	if [[ "$pkg" == *.pkg.tar.zst ]]; then
		list="$(zstd -dcq <"$pkg" | tar -tvf -)"
		docs=usr/share/licenses/blitz-desktop
		for f in .PKGINFO .MTREE; do grep -q " $f$" <<<"$list" || no "the Arch package has no $f"; done
	else
		list="$(dpkg-deb -c "$pkg")"
		docs=usr/share/doc/blitz-desktop
		dpkg-deb -f "$pkg" Maintainer | grep -q "^Retail Cortex " || no "the .deb's maintainer isn't Retail Cortex"
	fi
	for f in NOTICE THIRD_PARTY_NOTICES; do grep -q "$docs/$f$" <<<"$list" || no "no $docs/$f"; done
	grep -q "usr/share/metainfo/dev.blitz.desktop.metainfo.xml$" <<<"$list" || no "no AppStream metainfo"
	# root by name (the Arch package) or by id (the .deb: rules_pkg drops
	# the names of a merged tar).
	grep -Eq "^-rwxr-xr-x (root/root|0/0) .* \.?/?usr/lib/blitz-desktop/blitz-desktop$" <<<"$list" || no "no /usr/lib/blitz-desktop/blitz-desktop, root's and 0755"
	grep -Eq "^-rwsr-xr-x (root/root|0/0) .* \.?/?usr/lib/blitz-desktop/chrome-sandbox$" <<<"$list" || no "chrome-sandbox isn't setuid root"
	for exe in blitz blitzd blitz-tray; do
		grep -q "usr/lib/blitz-desktop/resources/app/bin/$exe$" <<<"$list" || no "no /usr/lib/blitz-desktop/resources/app/bin/$exe"
	done
	grep -q "usr/bin/blitz-desktop -> ../lib/blitz-desktop/blitz-desktop" <<<"$list" || no "no blitz-desktop on PATH"
fi
[ $fail -eq 0 ] && echo "✓ the package has Blitz's program, the app's programs and the license files"
exit $fail
