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

# Fails when a Go file isn't gofmt'd, a BUILD file isn't what Gazelle
# would write, or a source file lacks the license header. Uses Bazel's
# own Go.
#   tools/check_format.sh
set -euo pipefail
cd "$(dirname "$0")/.."
goroot="$(bazel run --noshow_progress --ui_event_filters=-info,-stderr @rules_go//go -- env GOROOT)"
bad="$("$goroot/bin/gofmt" -l apps pkg build tools 2>&1 || true)"
if [ -n "$bad" ]; then
	echo "✗ not gofmt'd (run gofmt -w):" >&2
	sed 's/^/    /' <<<"$bad" >&2
	exit 1
fi
echo "✓ Go files are formatted"
bazel run --noshow_progress --ui_event_filters=-info,-stderr //:gazelle -- -mode=diff >/dev/null ||
	{ echo "✗ BUILD files are out of date: run bazel run //:gazelle" >&2; exit 1; }
echo "✓ BUILD files are current"
bazel run --noshow_progress --ui_event_filters=-info,-stderr //tools:license_headers -- --check
