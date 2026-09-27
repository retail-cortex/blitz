#!/usr/bin/env bash
# Build stamps, for builds with --stamp (--config=release): the version is
# the git tag without its "v" (1.4.0), else the commit ("abc1234-dirty").
v="$(git describe --tags --match 'v*' --always --dirty 2>/dev/null || echo dev)"
echo "STABLE_BLITZ_VERSION ${v#v}"
