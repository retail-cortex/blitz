// Copyright 2026 Retail Cortex
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package textutil

import (
	"regexp"
	"strings"
)

// GlobToRegex converts a slash-separated glob to an unanchored regex body using
// only syntax understood by both Go and the macOS sandbox profile language.
func GlobToRegex(glob string) string {
	var sb strings.Builder
	for i := 0; i < len(glob); i++ {
		c := glob[i]
		switch {
		case c == '*' && i+1 < len(glob) && glob[i+1] == '*':
			i++
			if i+1 < len(glob) && glob[i+1] == '/' {
				i++
				sb.WriteString("(.*/)?")
			} else {
				sb.WriteString(".*")
			}
		case c == '*':
			sb.WriteString("[^/]*")
		case c == '?':
			sb.WriteString("[^/]")
		default:
			sb.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return sb.String()
}
