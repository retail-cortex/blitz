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

# Measures the Go tests' coverage of apps/ and pkg/ (not tools, generated
# code or dependencies), writes the summary for the docs site's coverage
# page (docs/data/coverage.json, not committed), and fails when the total
# is below tools/coverage/floor.txt.
#
#   tools/coverage.sh [bazel flags…] [-- //tools/coverage flags…]
#
# CI: tools/coverage.sh --config=race -- --markdown="$GITHUB_STEP_SUMMARY"
set -euo pipefail
cd "$(dirname "$0")/.."

bazel_flags=()
while (($#)) && [[ "$1" != "--" ]]; do
  bazel_flags+=("$1")
  shift
done
[[ "${1:-}" == "--" ]] && shift

bazel coverage --build_tests_only --combined_report=lcov --instrumentation_filter='^//(apps|pkg)[/:]' ${bazel_flags[@]+"${bazel_flags[@]}"} //...
bazel run //tools/coverage -- \
  --lcov=bazel-out/_coverage/_coverage_report.dat \
  --json=docs/data/coverage.json \
  --commit="$(git rev-parse --short HEAD)" \
  --date="$(date -u +%Y-%m-%d)" \
  "$@"
