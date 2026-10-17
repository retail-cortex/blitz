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

"""Blitz.app, made from Electron's bundle."""

_SCRIPT = """
set -euo pipefail
anchor="$1" app="$2" icon="$3" version="$(cat "$4")" out="$5"
shift 5
# Electron's bundle where its archive was unpacked (anchor is its
# Info.plist): copied whole with ditto, which keeps the framework's links.
src="$(cd "$(dirname "$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$anchor")")/.." && pwd -P)"
rm -rf "$out"
ditto "$src" "$out"
chmod -R u+w "$out"
plist="$out/Contents/Info.plist"
plutil -replace CFBundleName -string Blitz "$plist"
plutil -replace CFBundleDisplayName -string Blitz "$plist"
plutil -replace CFBundleIdentifier -string dev.blitz.desktop "$plist"
plutil -replace CFBundleShortVersionString -string "$version" "$plist"
plutil -replace CFBundleVersion -string "$version" "$plist"
plutil -replace NSHumanReadableCopyright -string "Copyright 2026 Retail Cortex. Apache License 2.0." "$plist"
plutil -replace CFBundleURLTypes -json '[{"CFBundleURLName":"Blitz link","CFBundleURLSchemes":["blitz"]}]' "$plist"
cp "$icon" "$out/Contents/Resources/electron.icns"
rm -f "$out/Contents/Resources/default_app.asar"
ditto "$app" "$out/Contents/Resources/app"
mkdir -p "$out/Contents/Resources/licenses"
cp "$src/../LICENSE" "$out/Contents/Resources/licenses/Electron-LICENSE"
cp "$src/../LICENSES.chromium.html" "$out/Contents/Resources/licenses/"
for f in "$@"; do cp "$f" "$out/Contents/Resources/"; done
# Ad hoc, so macOS knows it as dev.blitz.desktop; releases sign it again
# with a Developer ID (sign_macos.sh).
codesign --force --deep --sign - "$out"
"""

def _mac_app_impl(ctx):
    out = ctx.actions.declare_directory(ctx.attr.dir + "/Blitz.app")
    ctx.actions.run_shell(
        inputs = ctx.files.electron + ctx.files.licenses + [ctx.file.anchor, ctx.file.app, ctx.file.icon, ctx.file.version],
        outputs = [out],
        command = _SCRIPT,
        arguments = [ctx.file.anchor.path, ctx.file.app.path, ctx.file.icon.path, ctx.file.version.path, out.path] + [f.path for f in ctx.files.licenses],
        # ditto reads Electron's bundle where it was unpacked, links and all.
        execution_requirements = {"no-remote": "1", "no-sandbox": "1"},
        mnemonic = "ElectronMacApp",
        progress_message = "Making %{label}",
    )
    return [DefaultInfo(files = depset([out]))]

mac_app = rule(
    implementation = _mac_app_impl,
    doc = "Blitz.app: Electron's bundle named Blitz (dev.blitz.desktop), with the app in Contents/Resources/app, signed ad hoc.",
    attrs = {
        "anchor": attr.label(mandatory = True, allow_single_file = True, doc = "Electron.app/Contents/Info.plist in Electron's archive"),
        "app": attr.label(mandatory = True, allow_single_file = True, doc = "the app (electron_app)"),
        "dir": attr.string(mandatory = True, doc = "where Blitz.app goes in this package"),
        "electron": attr.label(mandatory = True, doc = "Electron's archive's files"),
        "icon": attr.label(mandatory = True, allow_single_file = True, doc = "the .icns"),
        "licenses": attr.label_list(allow_files = True, doc = "files for Contents/Resources"),
        "version": attr.label(mandatory = True, allow_single_file = True, doc = "//build:version"),
    },
)
