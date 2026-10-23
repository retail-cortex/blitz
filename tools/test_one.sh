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

# Runs the Go tests of one target that match a filter, and fails when none
# matched. bazel test --test_filter on a sharded target passes when the
# filter matches nothing (each shard runs no test), so a typo or a
# renamed test looks green; this runs unsharded and checks.
#   tools/test_one.sh //pkg/engine:engine_test 'TestReflect|TestHarness' [bazel flags…]
set -euo pipefail
if [ $# -lt 2 ]; then
	echo "usage: tools/test_one.sh <target> <filter> [bazel flags…]" >&2
	exit 2
fi
cd "$(dirname "$0")/.."
target=$1 filter=$2
shift 2
out="$(mktemp)"
trap 'rm -f "$out"' EXIT
status=0
bazel test "$target" --test_filter="$filter" --test_sharding_strategy=disabled \
	--test_arg=-test.v --test_output=all --nocache_test_results "$@" >"$out" 2>&1 || status=$?
grep -E '^(=== RUN|--- (PASS|FAIL|SKIP)|    --- (PASS|FAIL|SKIP)|FAIL|ok )' "$out" || true
if ! grep -q '^=== RUN' "$out"; then
	tail -20 "$out" >&2
	echo "✗ no test in $target matched $filter" >&2
	exit 1
fi
exit "$status"
