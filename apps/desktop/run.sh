#!/usr/bin/env bash
# Runs the desktop app from the build, with the blitz CLI and blitzd on
# PATH so it can install and restart the service:
#   bazel run //apps/desktop:run
set -euo pipefail
r="${RUNFILES_DIR:-$0.runfiles}/_main"
export PATH="$r/apps/cli/blitz_:$r/apps/service/blitzd_:$PATH"
exec "$r/apps/desktop/blitz-desktop_/blitz-desktop" "$@"
