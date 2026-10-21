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

# Refreshes third_party/mcp-toolbox, the pruned copy of MCP Toolbox for
# Databases that blitzd runs in-process (spec_databases_046):
#
#   tools/update_toolbox.sh [version]     (default: the one in VERSION)
#
# It downloads the release's Go module, copies only Toolbox's core and the
# databases Blitz offers (Postgres, AlloyDB, SQLite, BigQuery), tests
# left out, moves the config parser into embed/, rewrites the import path
# under Blitz's module (so the copy's internal/ packages are reachable
# from embed/, which lives in the same tree), and applies patches/*.patch,
# Blitz's changes (read-only Postgres and SQLite, sqlite-vec). Blitz's own
# files there (embed/embed.go, patches/, README.md, VERSION) are kept.
# Then: bazel run @rules_go//go -- mod tidy, bazel run //:gazelle, and the
# tests (bazel test //third_party/mcp-toolbox/... //pkg/databases/...).
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
dest="$root/third_party/mcp-toolbox"
version="${1:-$(cat "$dest/VERSION")}"
upstream="github.com/googleapis/mcp-toolbox"
# The module is still served at its old name.
proxied="github.com/googleapis/genai-toolbox"
ours="github.com/retail-cortex/blitz/third_party/mcp-toolbox"

cd "$root"
work="$(mktemp -d)"
trap 'chmod -R u+w "$work" 2>/dev/null; rm -rf "$work"' EXIT
echo "Downloading ${proxied}@v${version}…"
src="$(GOMODCACHE="$work/mod" GOFLAGS=-mod=mod bazel run --ui_event_filters=-info --noshow_progress @rules_go//go -- mod download -json "$proxied@v$version" | sed -n 's/.*"Dir": "\(.*\)",/\1/p')"
[[ -d "$src" ]] || { echo "no module downloaded for v$version" >&2; exit 1; }
grep -q "^module $upstream\$" "$src/go.mod" || { echo "the module isn't $upstream any more: check $src/go.mod" >&2; exit 1; }

# What's copied: the core, the chosen sources and tools.
core=(auth embeddingmodels group log prompts resources server telemetry util)
sources=(postgres alloydbpg bigquery sqlite sqlcommenter dataplex/searchcatalog)
tools=(postgres sqlite bigquery alloydbainl)
skip_tools=(bigquery/bigqueryconversationalanalytics) # its own Gemini client; not offered

chmod -R u+w "$dest" 2>/dev/null || true
rm -rf "$dest/internal" "$dest/embed/config.go" "$dest/LICENSE"
mkdir -p "$dest/internal/sources" "$dest/internal/tools" "$dest/embed"
cp "$src/LICENSE" "$dest/LICENSE"
for d in "${core[@]}"; do cp -R "$src/internal/$d" "$dest/internal/$d"; done
cp "$src"/internal/sources/*.go "$dest/internal/sources/"
for d in "${sources[@]}"; do mkdir -p "$dest/internal/sources/$(dirname "$d")" && cp -R "$src/internal/sources/$d" "$dest/internal/sources/$d"; done
cp "$src"/internal/tools/*.go "$dest/internal/tools/"
for d in "${tools[@]}"; do cp -R "$src/internal/tools/$d" "$dest/internal/tools/$d"; done
cp "$src/cmd/internal/config.go" "$dest/embed/config.go"
chmod -R u+w "$dest" # the module cache's files are read-only
for d in "${skip_tools[@]}"; do rm -rf "$dest/internal/tools/$d"; done
find "$dest/internal" "$dest/embed" -name '*_test.go' -delete
find "$dest/internal" -name testdata -type d -prune -exec rm -rf {} +
sed -i.bak 's/^package internal$/package embed/' "$dest/embed/config.go" && rm "$dest/embed/config.go.bak"

# Under Blitz's module: the copy's packages are Blitz's to import.
find "$dest/internal" "$dest/embed" -name '*.go' -exec perl -pi -e "s#\\Q$upstream\\E/#$ours/#g" {} +

shopt -s nullglob
for p in "$dest"/patches/*.patch; do
	echo "Applying $(basename "$p")"
	patch -s -p1 -d "$dest" < "$p"
done
echo "$version" > "$dest/VERSION"
echo "third_party/mcp-toolbox is Toolbox v$version. Next: bazel run @rules_go//go -- mod tidy && bazel run //:gazelle"
