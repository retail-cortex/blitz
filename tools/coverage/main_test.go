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

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A file reported by two tests counts a line covered if either ran it;
// packages come least covered first.
func TestParse(t *testing.T) {
	lcov := strings.Join([]string{
		"SF:pkg/a/a.go", "DA:1,1", "DA:2,0", "DA:3,0", "end_of_record",
		"SF:pkg/a/a.go", "DA:2,4", "end_of_record",
		"SF:pkg/a/b.go", "DA:1,0", "end_of_record",
		"SF:pkg/b/b.go", "DA:1,0", "DA:2,0", "DA:3,1", "DA:4,0", "end_of_record",
	}, "\n")
	r, err := parse(strings.NewReader(lcov))
	require.NoError(t, err)
	assert.Equal(t, Counts{Lines: 8, Covered: 3, Percent: 37.5}, r.Total)
	require.Len(t, r.Packages, 2)
	assert.Equal(t, "pkg/b", r.Packages[0].Name, "the least covered package comes first")
	assert.Equal(t, Counts{Lines: 4, Covered: 2, Percent: 50}, r.Packages[1].Counts)
	assert.Equal(t, []File{
		{Name: "a.go", Counts: Counts{Lines: 3, Covered: 2, Percent: 66.6}},
		{Name: "b.go", Counts: Counts{Lines: 1, Covered: 0, Percent: 0}},
	}, r.Packages[1].Files)
}

func TestParseRefuses(t *testing.T) {
	for name, lcov := range map[string]string{
		"empty":           "",
		"a line, no file": "DA:1,1\n",
		"a bad record":    "SF:a.go\nDA:x,1\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parse(strings.NewReader(lcov))
			assert.Error(t, err)
		})
	}
}

func TestReadFloor(t *testing.T) {
	file := filepath.Join(t.TempDir(), "floor.txt")
	require.NoError(t, os.WriteFile(file, []byte("# the floor\n\n61.5\n"), 0o644))
	floor, err := readFloor(file)
	require.NoError(t, err)
	assert.Equal(t, 61.5, floor)
}

func TestWriteMarkdown(t *testing.T) {
	var b bytes.Buffer
	writeMarkdown(&b, Report{Floor: 50, Total: Counts{Lines: 4, Covered: 3, Percent: 75},
		Packages: []Package{{Name: "pkg/a", Counts: Counts{Lines: 4, Covered: 3, Percent: 75}}}})
	assert.Contains(t, b.String(), "### Coverage: 75.0%")
	assert.Contains(t, b.String(), "| `pkg/a` | 75.0% | 3 / 4 |")
}

// A library no test links has no coverage data: it's listed, not left out
// silently (N26 pkg-small-2#13).
func TestUntested(t *testing.T) {
	t.Chdir(t.TempDir())
	for dir, src := range map[string]string{
		"apps/tray":    "package main\n\nfunc main() {}\n",
		"pkg/embedded": "package embedded\n\nimport \"embed\"\n\n//go:embed *.md\nvar FS embed.FS\n",
		"pkg/lit":      "package lit\n\nvar F = func() int { return 1 }\n",
	} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "x.go"), []byte(src), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "x_test.go"), []byte("package x\n\nfunc TestX() {}\n"), 0o644))
	}
	r := Report{Packages: []Package{{Name: "pkg/a"}, {Name: "apps/cli"}}}
	for name, tc := range map[string]struct {
		libraries []string
		want      []string
	}{
		"all tested":            {[]string{"pkg/a", "apps/cli", ""}, nil},
		"one not":               {[]string{"pkg/a", "apps/tray", " apps/cli ", "apps/tray"}, []string{"apps/tray"}},
		"only embeds: no code":  {[]string{"pkg/embedded"}, nil},
		"a function in a value": {[]string{"pkg/lit"}, []string{"pkg/lit"}},
		"can't be read":         {[]string{"pkg/gone"}, []string{"pkg/gone"}},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, untested(r, tc.libraries))
		})
	}
	var b bytes.Buffer
	writeMarkdown(&b, Report{Untested: []string{"apps/tray", "pkg/x"}})
	assert.Contains(t, b.String(), "Not counted (no test links them): `apps/tray`, `pkg/x`.")
}
