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
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGlobToRegex checks each glob form's regex and that the regex matches
// the paths the glob means and no others.
func TestGlobToRegex(t *testing.T) {
	cases := map[string]struct {
		glob, want string
		match      []string
		noMatch    []string
	}{
		"star":            {glob: "*.go", want: `[^/]*\.go`, match: []string{"a.go"}, noMatch: []string{"a/b.go"}},
		"question":        {glob: "a?.txt", want: `a[^/]\.txt`, match: []string{"ab.txt"}, noMatch: []string{"a/.txt"}},
		"double star":     {glob: "src/**", want: `src/.*`, match: []string{"src/a/b"}},
		"double star dir": {glob: "**/x", want: `(.*/)?x`, match: []string{"x", "a/b/x"}},
		"literal":         {glob: "a+b", want: `a\+b`, match: []string{"a+b"}, noMatch: []string{"aab"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := GlobToRegex(tc.glob)
			assert.Equal(t, tc.want, got)
			re := regexp.MustCompile("^" + got + "$")
			for _, m := range tc.match {
				assert.True(t, re.MatchString(m), "should match %q", m)
			}
			for _, m := range tc.noMatch {
				assert.False(t, re.MatchString(m), "should not match %q", m)
			}
		})
	}
}
