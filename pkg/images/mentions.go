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

package images

import (
	"regexp"
	"strings"
)

// mentionRE matches @path or @"path with spaces" at the start of the text or
// after whitespace (so e-mail addresses don't count).
var mentionRE = regexp.MustCompile(`(?:^|\s)@(?:"([^"]+)"|(\S+))`)

// Mentions returns the image files referenced as @path in a prompt, in
// order and without duplicates. Other @paths are left for the model.
func Mentions(prompt string) []string {
	var out []string
	for _, p := range MentionedPaths(prompt) {
		if IsImagePath(p) {
			out = append(out, p)
		}
	}
	return out
}

// MentionedPaths returns every @path in a prompt (images or not), in order
// and without duplicates; trailing punctuation isn't part of a bare path.
func MentionedPaths(prompt string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range mentionRE.FindAllStringSubmatch(prompt, -1) {
		p := m[1]
		if p == "" {
			p = strings.TrimRight(m[2], ".,;:!?)")
		}
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}
