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
		"class":           {glob: "*.[ch]", want: `[^/]*\.[ch]`, match: []string{"a.c", "a.h"}, noMatch: []string{"a.o", "a.[ch]"}},
		"range":           {glob: "v[0-9]", want: `v[0-9]`, match: []string{"v1"}, noMatch: []string{"va"}},
		"negated":         {glob: "[!.]*", want: `[^/.][^/]*`, match: []string{"a"}, noMatch: []string{".env", "/x"}},
		"bracket first":   {glob: "[]a]", want: `[\]a]`, match: []string{"]", "a"}, noMatch: []string{"b"}},
		"unclosed":        {glob: "a[b", want: `a\[b`, match: []string{"a[b"}},
		"no range":        {glob: "[z-a]", want: `\[z-a\]`, match: []string{"[z-a]"}},
		"empty negation":  {glob: "[!]", want: `\[!\]`, match: []string{"[!]"}},
		"braces literal":  {glob: "{a,b}", want: `\{a,b\}`, match: []string{"{a,b}"}},
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

// Each alternative is a glob of its own.
func TestExpandBraces(t *testing.T) {
	for name, tc := range map[string]struct {
		glob string
		want []string
	}{
		"none":     {"*.pem", []string{"*.pem"}},
		"two":      {"**/*.{pem,key}", []string{"**/*.pem", "**/*.key"}},
		"empty":    {"a{,.bak}", []string{"a", "a.bak"}},
		"repeated": {"{a,b}/{c,d}", []string{"a/c", "a/d", "b/c", "b/d"}},
		"nested":   {"{a,b{c,d}}", []string{"a", "bc", "bd"}},
		"one":      {"{a}.{b,c}", []string{"{a}.b", "{a}.c"}},
		"unclosed": {"{a,b", []string{"{a,b"}},
		"too many": {strings.Repeat("{a,b}", 7), []string{strings.Repeat("{a,b}", 7)}},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, ExpandBraces(tc.glob))
		})
	}
}
