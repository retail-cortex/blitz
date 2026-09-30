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

# Writes THIRD_PARTY_NOTICES from what the programs actually link: the Go
# modules in blitz, blitzd and the desktop app (build tools left out), and
# the npm packages bundled into the desktop app's page. --check (CI) fails
# if the committed file is out of date or a license isn't accepted.
#   tools/third_party_notices.sh [--check]
set -euo pipefail
cd "$(dirname "$0")/.."
q=(--noshow_progress --ui_event_filters=-info,-stderr)
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# The page's packages, as rules_js lays them out.
bazel build "${q[@]}" //apps/desktop/web:node_modules
out_base="$(bazel info "${q[@]}" output_base)"
bin="$(bazel info "${q[@]}" bazel-bin)"

# Go modules linked into the three programs; --notool_deps leaves out the
# compilers and code generators that only run during the build.
bazel query "${q[@]}" --notool_deps \
	'kind(go_library, deps(set(//apps/cli:blitz_unsigned //apps/service:blitzd_unsigned //apps/desktop:blitz-desktop_unsigned)))' \
	--output=label |
	sed -nE 's|^@@?(gazelle\+\+go_deps\+)?([^/]+)//.*|\2|p' | sort -u |
	sed "s|^|$out_base/external/gazelle++go_deps+|" >"$tmp/repos"

goroot="$(bazel run "${q[@]}" @rules_go//go -- env GOROOT)"
check=()
[ "${1:-}" = "--check" ] && check=(--check)
bazel run "${q[@]}" //tools/notices -- \
	--go-repos="$tmp/repos" --go-mod="$PWD/go.mod" --goroot="$goroot" --apache="$PWD/LICENSE" \
	--npm-lock="$PWD/pnpm-lock.yaml" --npm-store="$bin/node_modules/.aspect_rules_js" \
	--out="$PWD/THIRD_PARTY_NOTICES" "${check[@]}"
