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

# Runs the Electron trial on this machine (bazel run //apps/electron:dev):
# Electron, from its archive, on the app (dist/, page/, bin/). Electron
# runs from where the archive was unpacked, not from its runfiles' links,
# so macOS finds the framework beside it.
#   $1: the app's directory; $2: Electron's program inside its archive.
set -euo pipefail
app="$(cd "$1" && pwd -P)"
runfiles="${RUNFILES_DIR:-$0.runfiles}"
link=""
for d in "$runfiles"/*electron_*; do
	if [[ -e "$d/$2" ]]; then
		link="$d/$2"
		break
	fi
done
if [[ -z "$link" ]]; then
	echo "dev.sh: no $2 under $runfiles" >&2
	exit 1
fi
electron="$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$link" 2>/dev/null || readlink -f "$link")"

# macOS names the app (menu bar, Dock, window switcher) from its bundle's
# Info.plist, which says Electron: run a copy of the bundle named Blitz,
# made once per Electron and kept in the user's caches, re-signed (ad hoc)
# since its Info.plist changed.
if [[ "$(uname)" == "Darwin" && "$2" == Electron.app/* ]]; then
	bundle="$(cd "$(dirname "$electron")/../.." && pwd -P)"
	key="$(printf '%s' "$bundle" | shasum | cut -c1-12)"
	cache="$HOME/Library/Caches/dev.blitz.electron/$key"
	named="$cache/Blitz.app"
	if [[ ! -e "$cache/ready" ]]; then
		mkdir -p "$cache"
		ditto "$bundle" "$named"
		plist="$named/Contents/Info.plist"
		plutil -replace CFBundleName -string Blitz "$plist"
		plutil -replace CFBundleDisplayName -string Blitz "$plist"
		plutil -replace CFBundleIdentifier -string dev.blitz.electron "$plist"
		codesign --force --deep --sign - "$named" >/dev/null 2>&1
		touch "$cache/ready"
	fi
	electron="$named/Contents/MacOS/Electron"
fi
exec "$electron" "$app" "${@:3}"
