#!/usr/bin/env bash
# gopls's view of the code through Bazel, so editors see the generated
# packages (proto/blitz/v1) and the build's exact dependencies. Point
# your editor's gopls at it: GOPACKAGESDRIVER=<repo>/bazel/gopackagesdriver.sh
exec bazel run --tool_tag=gopackagesdriver -- @rules_go//go/tools/gopackagesdriver "$@"
