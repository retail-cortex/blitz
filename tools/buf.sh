#!/usr/bin/env bash
# buf (the version rules_buf pins in MODULE.bazel), run in the repository:
#   bazel run //tools:buf -- format --exit-code -d
#   bazel run //tools:buf -- breaking --against '.git#ref=<base>'
set -euo pipefail
buf="$(cd "$(dirname "$0")" && pwd)/$(basename "$0").runfiles/rules_buf++buf+rules_buf_toolchains/buf"
[ -x "$buf" ] || buf="${RUNFILES_DIR:-$0.runfiles}/rules_buf++buf+rules_buf_toolchains/buf"
cd "${BUILD_WORKSPACE_DIRECTORY:?run with bazel run}"
exec "$buf" "$@"
