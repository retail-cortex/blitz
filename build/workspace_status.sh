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

# Build stamps, for builds with --stamp (--config=release): the version is
# the git tag without its "v" (1.4.0), else the commit ("abc1234-dirty").
v="$(git describe --tags --match 'v*' --always --dirty 2>/dev/null || echo dev)"
echo "STABLE_BLITZ_VERSION ${v#v}"
