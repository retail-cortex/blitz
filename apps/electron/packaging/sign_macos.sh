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

# Signs a Blitz.app Bazel built (//apps/electron/packaging:Blitz_<arch>.app)
# and puts it in a disk image, notarised and stapled when the signing
# variables below are set; otherwise signed ad hoc (and said so).
#
# Usage: apps/electron/packaging/sign_macos.sh <Blitz.app> <version> <out.dmg>
#
# Signing (all or none):
#   DESKTOP_SIGN_IDENTITY  "Developer ID Application: Name (TEAMID)", in the keychain
#   NOTARY_KEY_PATH        an App Store Connect API key (.p8)
#   NOTARY_KEY_ID          its key ID
#   NOTARY_ISSUER          its issuer ID
set -euo pipefail

src="${1:?usage: $0 <Blitz.app> <version> <out.dmg>}"
version="${2:?usage: $0 <Blitz.app> <version> <out.dmg>}"
dmg="${3:?usage: $0 <Blitz.app> <version> <out.dmg>}"
entitlements="$(cd "$(dirname "$0")" && pwd)/entitlements.plist"

# Bazel's outputs are read-only: sign a copy.
stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT
app="$stage/Blitz.app"
ditto "$src" "$app"
chmod -R u+w "$app"

identity="${DESKTOP_SIGN_IDENTITY:--}"
[ "$identity" = - ] && echo "warning: DESKTOP_SIGN_IDENTITY isn't set: signing ad hoc, not notarising" >&2
sign() { # [codesign options] <path>: hardened runtime, and a timestamp with a real identity
	if [ "$identity" = - ]; then
		codesign --force --sign - "$@"
	else
		codesign --force --options runtime --timestamp --sign "$identity" "$@"
	fi
}

# Inside out: Electron's libraries and helpers, its frameworks, its helper
# apps, the Go programs (with their own identifiers: Bazel's links are all
# "a.out"), then the bundle; the entitlements on what runs Electron.
fw="$app/Contents/Frameworks"
find "$fw" -type f \( -name '*.dylib' -o -name '*.so' -o -name chrome_crashpad_handler \) -print0 | while IFS= read -r -d '' f; do sign "$f"; done
for f in "$fw"/*.framework; do sign "$f"; done
for f in "$fw"/*Helper*.app; do sign --entitlements "$entitlements" "$f"; done
bin="$app/Contents/Resources/app/bin"
sign --identifier dev.blitz.cli "$bin/blitz"
sign --identifier dev.blitz.service "$bin/blitzd"
sign --identifier dev.blitz.tray "$bin/blitz-tray"
sign --entitlements "$entitlements" "$app"
codesign --verify --strict --deep --verbose=2 "$app"

# The disk image: the app and a link to /Applications.
ln -s /Applications "$stage/Applications"
rm -f "$dmg"
hdiutil create -volname "Blitz $version" -srcfolder "$stage" -fs HFS+ -format UDZO -ov "$dmg"

if [ "$identity" != - ]; then
	codesign --force --timestamp --sign "$identity" "$dmg"
	# Notarising the image covers the app inside it; the ticket is stapled
	# to the image, so it opens offline too.
	xcrun notarytool submit "$dmg" --key "$NOTARY_KEY_PATH" --key-id "$NOTARY_KEY_ID" --issuer "$NOTARY_ISSUER" --wait --timeout 30m
	xcrun stapler staple "$dmg"
	xcrun stapler validate "$dmg"
	spctl --assess --type open --context context:primary-signature --verbose=2 "$dmg"
fi
echo "✅ $dmg"
