#!/usr/bin/env bash
# Packages the desktop app for a release, into build/desktop/dist:
#
#   macOS: Blitz_<version>_macos_universal.dmg — a universal Blitz.app
#          (arm64 + amd64) with the blitz CLI bundled. Signed with a
#          Developer ID, notarised and stapled when the signing variables
#          below are set; otherwise signed ad hoc (and said so).
#   Linux: blitz-desktop_<version>_<arch>.deb — the app and the CLI in
#          /usr/lib/blitz-desktop, a launcher and an icon. Built against
#          WebKitGTK 4.1 (Ubuntu 24.04, Debian 13 and later).
#
# Usage: scripts/desktop-package.sh <version>   (e.g. 1.4.0; a leading v is dropped)
#
# macOS signing (all or none):
#   DESKTOP_SIGN_IDENTITY  "Developer ID Application: Name (TEAMID)", in the keychain
#   NOTARY_KEY_PATH        an App Store Connect API key (.p8)
#   NOTARY_KEY_ID          its key ID
#   NOTARY_ISSUER          its issuer ID
set -euo pipefail

version="${1:?usage: $0 <version>}"
version="${version#v}"
root="$(cd "$(dirname "$0")/.." && pwd)"
out="$root/build/desktop/dist"
bin="$root/build/desktop/bin"
wails=(go tool -modfile="$root/tools/go.mod" wails)
ldflags="-s -w -X main.version=v$version"
mkdir -p "$out"

# The app's version comes from wails.json (Info.plist, the About page);
# it's set for this build and put back afterwards.
wails_json="$root/cmd/blitz-desktop/wails.json"
cp "$wails_json" "$wails_json.orig"
trap 'mv "$wails_json.orig" "$wails_json"' EXIT
jq --arg v "$version" '.info.productVersion = $v' "$wails_json.orig" >"$wails_json"

(cd "$root/web/desktop" && pnpm install --frozen-lockfile)

cli() { # cli <goos> <goarch> <output>
	(cd "$root" && CGO_ENABLED=0 GOOS="$1" GOARCH="$2" go build -trimpath -buildvcs=false -ldflags="$ldflags" -o "$3" ./cmd/blitz)
}

package_macos() {
	local app="$bin/Blitz.app" dmg="$out/Blitz_${version}_macos_universal.dmg"
	(cd "$root/cmd/blitz-desktop" && CGO_CFLAGS=-mmacosx-version-min=13.0 CGO_LDFLAGS=-mmacosx-version-min=13.0 \
		"${wails[@]}" build -clean -platform darwin/universal -trimpath)

	# The CLI, universal too, beside the app's executable (DSK-04).
	local tmp
	tmp="$(mktemp -d)"
	cli darwin arm64 "$tmp/blitz-arm64"
	cli darwin amd64 "$tmp/blitz-amd64"
	lipo -create -output "$app/Contents/MacOS/blitz" "$tmp/blitz-arm64" "$tmp/blitz-amd64"
	rm -rf "$tmp"
	test -x "$app/Contents/MacOS/blitz-desktop"
	for exe in blitz blitz-desktop; do
		[ "$(lipo -archs "$app/Contents/MacOS/$exe" | tr ' ' '\n' | sort | xargs)" = "arm64 x86_64" ] ||
			{ echo "$exe isn't universal" >&2; exit 1; }
	done

	# Nested code first, then the bundle; hardened runtime and a secure
	# timestamp, which notarisation requires.
	local signed=""
	if [ -n "${DESKTOP_SIGN_IDENTITY:-}" ]; then
		codesign --force --options runtime --timestamp --sign "$DESKTOP_SIGN_IDENTITY" "$app/Contents/MacOS/blitz"
		codesign --force --options runtime --timestamp --sign "$DESKTOP_SIGN_IDENTITY" "$app"
		signed=1
	else
		echo "warning: DESKTOP_SIGN_IDENTITY isn't set: signing ad hoc, not notarising" >&2
		codesign --force --deep --sign - "$app"
	fi
	codesign --verify --strict --deep --verbose=2 "$app"

	# The disk image: the app and a link to /Applications.
	local stage
	stage="$(mktemp -d)"
	ditto "$app" "$stage/Blitz.app"
	ln -s /Applications "$stage/Applications"
	rm -f "$dmg"
	hdiutil create -volname "Blitz $version" -srcfolder "$stage" -fs HFS+ -format UDZO -ov "$dmg"
	rm -rf "$stage"

	if [ -n "$signed" ]; then
		codesign --force --timestamp --sign "$DESKTOP_SIGN_IDENTITY" "$dmg"
		# Notarising the image covers the app inside it; the ticket is
		# stapled to the image, so it opens offline too.
		xcrun notarytool submit "$dmg" --key "$NOTARY_KEY_PATH" --key-id "$NOTARY_KEY_ID" --issuer "$NOTARY_ISSUER" --wait --timeout 30m
		xcrun stapler staple "$dmg"
		xcrun stapler validate "$dmg"
		spctl --assess --type open --context context:primary-signature --verbose=2 "$dmg"
	fi
	echo "✅ $dmg"
}

package_linux() {
	local arch pkg deb lib
	arch="$(dpkg --print-architecture)"
	deb="$out/blitz-desktop_${version}_${arch}.deb"
	(cd "$root/cmd/blitz-desktop" && "${wails[@]}" build -clean -tags webkit2_41 -trimpath)

	pkg="$(mktemp -d)"
	chmod 755 "$pkg" # mktemp makes it private; it becomes the package's root
	lib="$pkg/usr/lib/blitz-desktop"
	install -d "$lib" "$pkg/usr/bin" "$pkg/usr/share/applications" "$pkg/usr/share/icons/hicolor/1024x1024/apps" "$pkg/DEBIAN"
	install -m 755 "$bin/blitz-desktop" "$lib/blitz-desktop"
	cli linux "$(go env GOARCH)" "$lib/blitz"
	# The app finds its CLI beside its resolved executable, so the link works.
	ln -s ../lib/blitz-desktop/blitz-desktop "$pkg/usr/bin/blitz-desktop"
	install -m 644 "$root/build/desktop/appicon.png" "$pkg/usr/share/icons/hicolor/1024x1024/apps/blitz-desktop.png"
	cat >"$pkg/usr/share/applications/blitz-desktop.desktop" <<-EOF
		[Desktop Entry]
		Type=Application
		Name=Blitz
		Comment=A coding agent for your projects
		Exec=blitz-desktop
		Icon=blitz-desktop
		Terminal=false
		Categories=Development;
		StartupWMClass=blitz-desktop
	EOF
	cat >"$pkg/DEBIAN/control" <<-EOF
		Package: blitz-desktop
		Version: $version
		Architecture: $arch
		Maintainer: Blitz <https://github.com/retail-cortex/blitz/issues>
		Depends: libgtk-3-0t64 | libgtk-3-0, libwebkit2gtk-4.1-0
		Section: devel
		Priority: optional
		Homepage: https://github.com/retail-cortex/blitz
		Description: Blitz desktop app
		 A window onto the Blitz service: workspaces, chats with the agent,
		 approvals, changes and workers. Includes the blitz command, which
		 it uses to install and start the service.
	EOF
	dpkg-deb --root-owner-group --build "$pkg" "$deb"
	rm -rf "$pkg"
	echo "✅ $deb"
}

case "$(uname -s)" in
Darwin) package_macos ;;
Linux) package_linux ;;
*)
	echo "the desktop app is packaged on macOS and Linux only" >&2
	exit 1
	;;
esac
