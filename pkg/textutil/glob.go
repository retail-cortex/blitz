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
// only syntax understood by both Go and the macOS sandbox profile language:
// *, ** and ? as wildcards, and a class, [abc], [a-z] or [!abc] (never a
// slash). Braces aren't alternatives here: ExpandBraces makes one glob of
// each first. Anything else is quoted, so the result always compiles.
func GlobToRegex(glob string) string {
	var sb strings.Builder
	for i := 0; i < len(glob); i++ {
		c := glob[i]
		switch {
		case c == '[':
			if class, n := globClass(glob[i:]); n > 0 {
				sb.WriteString(class)
				i += n - 1
				continue
			}
			sb.WriteString(regexp.QuoteMeta("["))
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

// globClass reads the class glob starts with ("[a-z]", "[!.]") as a regex
// class, and how many bytes it took; 0 when it isn't one (no closing
// bracket, or a range that's no range, as [z-a]), and the [ is literal.
func globClass(glob string) (string, int) {
	start := 1
	negate := len(glob) > 1 && (glob[1] == '!' || glob[1] == '^')
	if negate {
		start++
	}
	if start < len(glob) && glob[start] == ']' { // []a]: the first ] is in the class
		start++
	}
	if start > len(glob) {
		return "", 0
	}
	end := strings.IndexByte(glob[start:], ']')
	if end < 0 {
		return "", 0
	}
	end += start
	first := 1
	if negate {
		first = 2
	}
	body := strings.NewReplacer(`\`, `\\`, "[", `\[`, "]", `\]`, "^", `\^`).Replace(glob[first:end])
	class := "[" + body + "]"
	if negate {
		class = "[^/" + body + "]"
	}
	if _, err := regexp.Compile(class); err != nil {
		return "", 0
	}
	return class, end + 1
}

// maxBraces bounds what ExpandBraces makes of one glob.
const maxBraces = 64

// ExpandBraces makes one glob of each {a,b} alternative ("**/*.{pem,key}":
// "**/*.pem" and "**/*.key"), nested and repeated braces too. A brace
// without a comma, or unclosed, stays as written; more than 64 globs is
// none expanded, the glob as written.
func ExpandBraces(glob string) []string {
	out := expandBraces(glob)
	if len(out) > maxBraces {
		return []string{glob}
	}
	return out
}

func expandBraces(glob string) []string {
	for i := 0; i < len(glob); i++ {
		if glob[i] != '{' {
			continue
		}
		depth, last := 0, i+1
		var alts []string
	scan:
		for j := i; j < len(glob); j++ {
			switch glob[j] {
			case '{':
				depth++
			case ',':
				if depth == 1 {
					alts = append(alts, glob[last:j])
					last = j + 1
				}
			case '}':
				if depth--; depth > 0 {
					continue
				}
				if len(alts) == 0 { // {x}: as written
					break scan
				}
				alts = append(alts, glob[last:j])
				var out []string
				for _, a := range alts {
					for _, g := range expandBraces(glob[:i] + a + glob[j+1:]) {
						if out = append(out, g); len(out) > maxBraces {
							return out
						}
					}
				}
				return out
			}
		}
	}
	return []string{glob}
}
