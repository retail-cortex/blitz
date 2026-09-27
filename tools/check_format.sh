#!/usr/bin/env bash
# Fails when a Go file isn't gofmt'd, or a BUILD file isn't what Gazelle
# would write. Uses Bazel's own Go.
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
