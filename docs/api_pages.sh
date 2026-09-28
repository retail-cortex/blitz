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

# The API reference pages: proto-gen-md-diagrams' Markdown (tables and
# Mermaid class diagrams) for each proto, made into site pages.
#
#   api_pages.sh <proto-gen-md-diagrams> <out dir> <file.proto>...
#
# Each page gets front matter. The generator's own heading (the site shows
# the title) and the package comment it takes from the file's license
# header are dropped. The generator copies comments into its HTML as they
# are, so a placeholder such as "/plan <goal>" would vanish as a tag:
# outside code blocks, "<" is escaped except in the tags it emits itself
# (div, span, br and HTML comments) and where it's already escaped (\<).
set -euo pipefail

tool="$1" out="$2"
shift 2
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

"$tool" -d "$(dirname "$1")" -o "$tmp" -r=false > /dev/null

# Pages in reading order: the conversation first, then what surrounds it.
order=(session turn workspace file config worker)
mkdir -p "$out"
for proto in "$@"; do
  name="$(basename "$proto" .proto)"
  weight=99
  for i in "${!order[@]}"; do
    if [[ "${order[$i]}" == "$name" ]]; then weight=$((i + 1)); fi
  done
  {
    # "Edit page" opens the proto: that's where a fix to this page belongs.
    printf -- '---\ntitle: "%s.proto"\nweight: %d\ngeekdocEditPath: edit/main\ngeekdocFilePath: proto/blitz/v1/%s.proto\n---\n\n' "$name" "$weight" "$name"
    printf 'Generated from [`proto/blitz/v1/%s.proto`](https://github.com/retail-cortex/blitz/blob/main/proto/blitz/v1/%s.proto) by [proto-gen-md-diagrams](https://github.com/GoogleCloudPlatform/proto-gen-md-diagrams).\n\n' "$name" "$name"
    awk 'NR == 1 && /^# Package:/ { next }
         /^<div class="comment">/ && /Licensed under the Apache License/ { next }
         /^```/ { fence = !fence; print; next }
         fence { print; next }
         {
           gsub(/\\</, "\001")
           gsub(/</, "\\&lt;")
           gsub(/&lt;div/, "<div"); gsub(/&lt;\/div>/, "</div>")
           gsub(/&lt;span>/, "<span>"); gsub(/&lt;\/span>/, "</span>")
           gsub(/&lt;br\/>/, "<br/>"); gsub(/&lt;!--/, "<!--")
           gsub(/\001/, "\\<")
           print
         }' "$tmp/$name.proto.md"
  } > "$out/$name.md"
done
