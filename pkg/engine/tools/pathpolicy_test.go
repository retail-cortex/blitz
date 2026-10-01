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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPathMatcherPatterns matches blocked patterns: bare names anywhere,
// relative paths at each root, and absolute ones under both their given and
// symlink-resolved spellings; empty patterns are ignored.
func TestPathMatcherPatterns(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	require.NoError(t, os.Mkdir(real, 0o755))
	link := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(real, link))
	root := filepath.Join(dir, "root")

	m, err := NewPathMatcher([]string{"  ", "*.pem", "conf/secret", link + "/*.key", "/*.top"}, []string{root})
	require.NoError(t, err)
	assert.Equal(t, []string{"*.pem", "conf/secret", link + "/*.key", "/*.top"}, m.Patterns(), "the blank pattern is dropped")

	cases := map[string]bool{
		filepath.Join(root, "x", "a.pem"):          true,
		filepath.Join(root, "conf", "secret", "f"): true,
		filepath.Join(dir, "conf", "secret"):       false,
		filepath.Join(link, "a.key"):               true,
		filepath.Join(real, "a.key"):               true,
		filepath.Join(real, "a.txt"):               false,
		"/x.top":                                   true,
	}
	for p, want := range cases {
		t.Run(p, func(t *testing.T) {
			_, got := m.Match(p)
			assert.Equal(t, want, got)
		})
	}
	assert.Len(t, m.sbplRegexes(), 5, "the absolute pattern through a symlink has two expressions")
}

// TestPathMatcherNil matches nothing and has no patterns.
func TestPathMatcherNil(t *testing.T) {
	var m *PathMatcher
	_, blocked := m.Match("/x")
	assert.False(t, blocked)
	assert.Nil(t, m.Patterns())
	assert.Nil(t, m.sbplRegexes())
}

// TestPathMatcherCaseInsensitive ignores case where the file system does.
func TestPathMatcherCaseInsensitive(t *testing.T) {
	old := caseInsensitiveFS
	caseInsensitiveFS = true
	t.Cleanup(func() { caseInsensitiveFS = old })
	m, err := NewPathMatcher([]string{"*.PEM"}, nil)
	require.NoError(t, err)
	_, blocked := m.Match("/a/b.pem")
	assert.True(t, blocked)
}

// TestCanonicalDir resolves an existing directory and refuses a file or a
// missing path.
func TestCanonicalDir(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "f")
	require.NoError(t, os.WriteFile(f, nil, 0o644))
	_, err := canonicalDir(f)
	assert.ErrorContains(t, err, "is not a directory")
	_, err = canonicalDir(filepath.Join(dir, "missing"))
	assert.Error(t, err)
	got, err := canonicalDir(dir)
	require.NoError(t, err)
	want, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}
