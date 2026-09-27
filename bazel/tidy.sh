#!/usr/bin/env bash
# Dependency upkeep, all through Bazel: tidies go.mod/go.sum with Bazel's
# own Go, then MODULE.bazel's repositories to match.
#   bazel run //bazel:tidy
# -e: proto/blitz/v1 has no Go files outside the build (Bazel generates
# them), which tidy would otherwise try to download.
set -euo pipefail
cd "${BUILD_WORKSPACE_DIRECTORY:?run with bazel run}"
bazel run --noshow_progress --ui_event_filters=-info,-stderr @rules_go//go -- mod tidy -e 2>&1 |
	grep -v 'retail-cortex/blitz/proto/blitz/v1' || true
bazel mod tidy
bazel run --noshow_progress --ui_event_filters=-info,-stderr //:gazelle
echo "✓ go.mod, go.sum, MODULE.bazel and the BUILD files are tidy"
