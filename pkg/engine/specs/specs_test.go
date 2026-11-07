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

package specs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A spec as /spec writes it (project-spec step 5).
const auth = "# 004. Authentication\n\nStatus: approved\nDepends on: 001-overview, 003-data-model\n\n## Context\n\nStaff sign in.\n\n## Requirements\n\n- **AUTH-01** Staff sign in with Google (OpenID Connect).\n- **AUTH-02**: Sessions end after 8 hours idle.\n3. AUTH-03 Sign-in is fast and secure.\n- UTF-16 names are kept as typed.\n\n```\nAUTH-99 in a code block isn't one\n```\n\n**Done when:** every item has a passing test.\n\n## Open questions\n\n- Which domain may sign in?\n"

func TestParse(t *testing.T) {
	s := Parse("004-authentication.md", auth)
	assert.Equal(t, 4, s.Number)
	assert.Equal(t, "Authentication", s.Title)
	assert.Equal(t, 1, s.TitleLine)
	assert.Equal(t, "approved", s.Status)
	assert.Equal(t, []int{1, 3}, s.DependsOn)
	require.Len(t, s.Items, 3, "IDs only, not UTF-16, not inside a code block")
	assert.Equal(t, Item{ID: "AUTH-01", Text: "Staff sign in with Google (OpenID Connect).", Line: 12}, s.Items[0])
	assert.Equal(t, "Sessions end after 8 hours idle.", s.Items[1].Text)
	assert.Equal(t, "AUTH-03", s.Items[2].ID)
	assert.True(t, s.DoneWhen)
	assert.Equal(t, []string{"Which domain may sign in?"}, s.OpenQuestions)

	o := Parse("001-overview.md", "# 001. Overview\n**Status:** draft\nKind: software, research\n")
	assert.Equal(t, "draft", o.Status)
	assert.Equal(t, []string{"software", "research"}, o.Kind)
	assert.Equal(t, "Overview", Parse("x.md", "# Overview").Title, "no number in the heading")
}

func codes(fs []Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Code)
	}
	return out
}

// The checks that need no model (SDD-02).
func TestLint(t *testing.T) {
	over := Parse("001-overview.md", "# 001. Overview\nStatus: approved\n- **OV-01** One store, 3 tills.\nDone when: it opens.\n")
	data := Parse("003-data-model.md", "# 003. Data model\nStatus: draft\nDepends on: 004\n- **DATA-01** Orders keep 7 years.\n## Done when\nmigrated\n")
	a := Parse("004-authentication.md", auth)
	all := []Spec{over, data, a}

	assert.Empty(t, Lint(over, all), "a sound spec")
	got := Lint(a, all)
	assert.Contains(t, codes(got), "vague")
	assert.Contains(t, codes(got), "open-questions")
	assert.Contains(t, codes(got), "dependency-cycle", "004 → 003 → 004")
	assert.Contains(t, codes(Lint(data, all)), "later-dependency")

	for _, tc := range []struct {
		name, file, text, want string
	}{
		{"no title", "002-x.md", "Status: draft\n- **X-01** a\nDone when: a\n", "no-title"},
		{"unnumbered", "notes.md", "# Notes\nStatus: draft\n- **N-01** a\nDone when: a\n", "unnumbered"},
		{"no status", "002-x.md", "# 002. X\n- **X-01** a\nDone when: a\n", "no-status"},
		{"unknown status", "002-x.md", "# 002. X\nStatus: wip\n- **X-01** a\nDone when: a\n", "unknown-status"},
		{"unknown dependency", "002-x.md", "# 002. X\nStatus: draft\nDepends on: 042\n- **X-01** a\nDone when: a\n", "unknown-dependency"},
		{"itself", "002-x.md", "# 002. X\nStatus: draft\nDepends on: 002\n- **X-01** a\nDone when: a\n", "depends-on-itself"},
		{"no items", "002-x.md", "# 002. X\nStatus: draft\nDone when: a\n", "no-items"},
		{"duplicate here", "002-x.md", "# 002. X\nStatus: draft\n- **X-01** a\n- **X-01** b\nDone when: a\n", "duplicate-id"},
		{"duplicate elsewhere", "002-x.md", "# 002. X\nStatus: draft\n- **OV-01** a\nDone when: a\n", "duplicate-id"},
		{"empty item", "002-x.md", "# 002. X\nStatus: draft\n- **X-01**\nDone when: a\n", "empty-item"},
		{"no done when", "002-x.md", "# 002. X\nStatus: draft\n- **X-01** a\n", "no-done-when"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Parse(tc.file, tc.text)
			assert.Contains(t, codes(Lint(s, append([]Spec{s}, all...))), tc.want)
		})
	}
	assert.Nil(t, Lint(Parse(Index, "# Specs"), all), "the index has its own check")
}

func TestUnlistedAndSort(t *testing.T) {
	all := []Spec{{File: "002-orders.md", Number: 2}, {File: "notes.md"}, {File: Index}, {File: "001-overview.md", Number: 1}}
	assert.Equal(t, []string{"002-orders.md"}, Unlisted("1. [Overview](001-overview.md)", all))
	Sort(all)
	var files []string
	for _, s := range all {
		files = append(files, s.File)
	}
	assert.Equal(t, []string{Index, "001-overview.md", "002-orders.md", "notes.md"}, files)
}
