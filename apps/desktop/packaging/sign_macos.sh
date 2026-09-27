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

# Signs the Blitz.app Bazel built (//apps/desktop/packaging:Blitz.app) and
# puts it in a disk image, notarised and stapled when the signing
# variables below are set; otherwise signed ad hoc (and said so).
#
# Usage: apps/desktop/packaging/sign_macos.sh <Blitz.app> <version> <out.dmg>
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

# Bazel's outputs are read-only: sign a copy.
stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT
app="$stage/Blitz.app"
ditto "$src" "$app"
chmod -R u+w "$app"
chmod a-x "$app/Contents/Info.plist" "$app/Contents/Resources/iconfile.icns"

for exe in blitz blitzd blitz-desktop; do
	[ "$(lipo -archs "$app/Contents/MacOS/$exe" | tr ' ' '\n' | sort | xargs)" = "arm64 x86_64" ] ||
		{ echo "$exe isn't universal" >&2; exit 1; }
done

# Nested code first, then the bundle; hardened runtime and a secure
# timestamp, which notarisation requires. Each program gets a real
# identifier: Bazel's links are all "a.out" (build/macos.bzl).
signed=""
if [ -n "${DESKTOP_SIGN_IDENTITY:-}" ]; then
	codesign --force --options runtime --timestamp --identifier dev.blitz.cli --sign "$DESKTOP_SIGN_IDENTITY" "$app/Contents/MacOS/blitz"
	codesign --force --options runtime --timestamp --identifier dev.blitz.service --sign "$DESKTOP_SIGN_IDENTITY" "$app/Contents/MacOS/blitzd"
	codesign --force --options runtime --timestamp --sign "$DESKTOP_SIGN_IDENTITY" "$app"
	signed=1
else
	echo "warning: DESKTOP_SIGN_IDENTITY isn't set: signing ad hoc, not notarising" >&2
	codesign --force --identifier dev.blitz.cli --sign - "$app/Contents/MacOS/blitz"
	codesign --force --identifier dev.blitz.service --sign - "$app/Contents/MacOS/blitzd"
	codesign --force --sign - "$app"
fi
codesign --verify --strict --deep --verbose=2 "$app"

# The disk image: the app and a link to /Applications.
ln -s /Applications "$stage/Applications"
rm -f "$dmg"
hdiutil create -volname "Blitz $version" -srcfolder "$stage" -fs HFS+ -format UDZO -ov "$dmg"

if [ -n "$signed" ]; then
	codesign --force --timestamp --sign "$DESKTOP_SIGN_IDENTITY" "$dmg"
	# Notarising the image covers the app inside it; the ticket is stapled
	# to the image, so it opens offline too.
	xcrun notarytool submit "$dmg" --key "$NOTARY_KEY_PATH" --key-id "$NOTARY_KEY_ID" --issuer "$NOTARY_ISSUER" --wait --timeout 30m
	xcrun stapler staple "$dmg"
	xcrun stapler validate "$dmg"
	spctl --assess --type open --context context:primary-signature --verbose=2 "$dmg"
fi
echo "✅ $dmg"
