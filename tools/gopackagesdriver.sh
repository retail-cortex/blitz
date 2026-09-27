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

# gopls's view of the code through Bazel, so editors see the generated
# packages (proto/blitz/v1) and the build's exact dependencies. Point
# your editor's gopls at it: GOPACKAGESDRIVER=<repo>/bazel/gopackagesdriver.sh
exec bazel run --tool_tag=gopackagesdriver -- @rules_go//go/tools/gopackagesdriver "$@"
