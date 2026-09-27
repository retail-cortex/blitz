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

# buf (the version rules_buf pins in MODULE.bazel), run in the repository:
#   bazel run //tools:buf -- format --exit-code -d
#   bazel run //tools:buf -- breaking --against '.git#ref=<base>'
set -euo pipefail
buf="$(cd "$(dirname "$0")" && pwd)/$(basename "$0").runfiles/rules_buf++buf+rules_buf_toolchains/buf"
[ -x "$buf" ] || buf="${RUNFILES_DIR:-$0.runfiles}/rules_buf++buf+rules_buf_toolchains/buf"
cd "${BUILD_WORKSPACE_DIRECTORY:?run with bazel run}"
exec "$buf" "$@"
