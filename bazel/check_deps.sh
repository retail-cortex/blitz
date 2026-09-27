#!/usr/bin/env bash
# The monorepo's dependency rules (AGENTS.md, Layout), checked on the
# build graph. Libraries only: tests may use the engine and servicetest.
#
#   bazel run //bazel:check_deps
set -euo pipefail
cd "${BUILD_WORKSPACE_DIRECTORY:-$(dirname "$0")/..}"

failed=0
# empty <what> <query>: the query must find nothing.
empty() {
	local out
	out="$(bazel query --noshow_progress --output=label "$2" 2>/dev/null)"
	if [ -n "$out" ]; then
		echo "✗ $1:" >&2
		sed 's/^/    /' <<<"$out" >&2
		failed=1
	else
		echo "✓ $1"
	fi
}

libs='kind("go_library", %s)'
# shellcheck disable=SC2059
{
	empty "front ends and the contract don't reach the engine" \
		"$(printf "$libs" 'deps(kind("go_library", //apps/cli/internal/tui/... + //apps/desktop/... + //pkg/api/... + //pkg/client/... + //pkg/socket/...))') intersect //pkg/engine/..."
	empty "shared packages don't reach the apps" \
		"$(printf "$libs" 'deps(kind("go_library", //pkg/...))') intersect //apps/..."
	empty "the CLI doesn't reach the other apps" \
		"$(printf "$libs" 'deps(kind("go_library", //apps/cli/...))') intersect (//apps/service/... + //apps/desktop/...)"
	empty "the service doesn't reach the other apps" \
		"$(printf "$libs" 'deps(kind("go_library", //apps/service/...) except //apps/service/servicetest/...)') intersect (//apps/cli/... + //apps/desktop/...)"
	empty "the desktop app doesn't reach the other apps" \
		"$(printf "$libs" 'deps(kind("go_library", //apps/desktop/...))') intersect (//apps/cli/... + //apps/service/...)"
	empty "no engine library is public" \
		'attr(visibility, "//visibility:public", kind("go_library", //pkg/engine/...))'
}
exit $failed
