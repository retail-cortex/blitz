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

# The Apache 2.0 header on every source file (addlicense, the version
# pinned in go.mod). Adds missing headers; --check only reports them:
#   bazel run //tools:license_headers
#   bazel run //tools:license_headers -- --check
set -euo pipefail
tool="$(cd "$(dirname "$0")" && pwd)/$(basename "$0").runfiles/gazelle++go_deps+com_github_google_addlicense/addlicense_/addlicense"
[ -x "$tool" ] || tool="${RUNFILES_DIR:-$0.runfiles}/gazelle++go_deps+com_github_google_addlicense/addlicense_/addlicense"
cd "${BUILD_WORKSPACE_DIRECTORY:?run with bazel run}"

mode=()
if [ "${1:-}" = "--check" ]; then
	mode=(-check)
fi
# Only tracked files: never Bazel's outputs, node_modules or local files.
# Generated code ("Code generated … DO NOT EDIT") is skipped by addlicense.
git ls-files -z | xargs -0 "$tool" "${mode[@]}" -l apache -c "Retail Cortex" -y 2026 \
	-ignore '**/testdata/**' -ignore '**/*.lock' -ignore '**/pnpm-lock.yaml' \
	>"${TMPDIR:-/tmp}/license_headers.$$" 2>&1 || {
	status=$?
	if [ ${#mode[@]} -gt 0 ]; then
		echo "✗ missing the license header (run: bazel run //tools:license_headers):"
		sed 's/^/    /' "${TMPDIR:-/tmp}/license_headers.$$"
	else
		cat "${TMPDIR:-/tmp}/license_headers.$$" >&2
	fi
	rm -f "${TMPDIR:-/tmp}/license_headers.$$"
	exit $status
}
rm -f "${TMPDIR:-/tmp}/license_headers.$$"
echo "✓ every source file has the license header"
