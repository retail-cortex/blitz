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
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDiffStats checks only the header's ---/+++ lines are skipped: in a
// hunk, a removed "-- x" or added "++x" line counts (N26
// engine-tools-1#11).
func TestDiffStats(t *testing.T) {
	for _, tc := range []struct {
		name           string
		before, after  string
		added, removed int
	}{
		{"plain", "a\nb\n", "a\nc\n", 1, 1},
		{"front matter removed", "---\ntitle: x\n---\nbody\n", "title: x\nbody\n", 0, 2},
		{"a SQL comment removed", "-- note\nselect 1;\n", "select 1;\n", 0, 1},
		{"a ++ line added", "x\n", "x\n++y\n", 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			added, removed := diffStats(unifiedDiff("f", tc.before, tc.after))
			assert.Equal(t, tc.added, added, "added")
			assert.Equal(t, tc.removed, removed, "removed")
		})
	}
}
