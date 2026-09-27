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

# Dependency upkeep, all through Bazel: tidies go.mod/go.sum with Bazel's
# own Go, then MODULE.bazel's repositories to match.
#   bazel run //tools:tidy
# -e: proto/blitz/v1 has no Go files outside the build (Bazel generates
# them), which tidy would otherwise try to download.
set -euo pipefail
cd "${BUILD_WORKSPACE_DIRECTORY:?run with bazel run}"
bazel run --noshow_progress --ui_event_filters=-info,-stderr @rules_go//go -- mod tidy -e 2>&1 |
	grep -v 'retail-cortex/blitz/proto/blitz/v1' || true
bazel mod tidy
bazel run --noshow_progress --ui_event_filters=-info,-stderr //:gazelle
echo "✓ go.mod, go.sum, MODULE.bazel and the BUILD files are tidy"
