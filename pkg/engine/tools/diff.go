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

package tools

import (
	"strings"

	"github.com/aymanbagabas/go-udiff"
)

// unifiedDiff renders a unified diff of a file change ("" when unchanged).
func unifiedDiff(path, before, after string) string {
	if before == after {
		return ""
	}
	oldName, newName := "a/"+path, "b/"+path
	if before == "" {
		oldName = "/dev/null"
	}
	if after == "" {
		newName = "/dev/null"
	}
	return udiff.Unified(oldName, newName, before, after)
}

// diffStats counts added and removed lines in a unified diff.
func diffStats(diff string) (added, removed int) {
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
		case strings.HasPrefix(line, "+"):
			added++
		case strings.HasPrefix(line, "-"):
			removed++
		}
	}
	return added, removed
}
