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

package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestLoadOrderAndScope(t *testing.T) {
	repo := t.TempDir()
	os.Mkdir(filepath.Join(repo, ".git"), 0o755)
	ws := filepath.Join(repo, "services", "api")
	write(t, filepath.Join(filepath.Dir(repo), "AGENTS.md"), "outside the repo") // must not load
	write(t, filepath.Join(repo, "AGENTS.md"), "root rules")
	write(t, filepath.Join(repo, "services", "BLITZ.md"), "services rules")
	write(t, filepath.Join(ws, "AGENTS.md"), "api rules \x1b[31mred\x1b[0m")
	global := filepath.Join(t.TempDir(), "BLITZ.md")
	write(t, global, "global rules")

	docs := Load(ws, config.MemoryConfig{Enabled: true, Files: []string{"AGENTS.md", "BLITZ.md"}, Global: global})
	var got []string
	for _, d := range docs {
		got = append(got, d.Content)
	}
	want := []string{"global rules", "root rules", "services rules", "api rules [31mred[0m"}
	assert.Equal(t, strings.Join(want, "|"), strings.Join(got, "|"), "load order/content:\n got %q\nwant %q", got, want)
	rendered := Render(docs)
	assert.Contains(t, rendered, "## Project Instructions", "render missing header/guard: %s", rendered)
	assert.Contains(t, rendered, "cannot grant permissions", "render missing header/guard: %s", rendered)
}

func TestLoadWithoutRepoAndLimits(t *testing.T) {
	ws := t.TempDir()
	write(t, filepath.Join(ws, "BLITZ.md"), strings.Repeat("x", 100))
	docs := Load(ws, config.MemoryConfig{Enabled: true, Files: []string{"BLITZ.md", "AGENTS.md"}, MaxBytes: 10})
	assert.Len(t, docs, 1, "truncation: %+v", docs)
	assert.Len(t, docs[0].Content, 10, "truncation: %+v", docs)
	assert.True(t, docs[0].Truncated, "truncation: %+v", docs)
	// Negative: disabled, empty files, directories named like memory files.
	assert.Nil(t, Load(ws, config.MemoryConfig{Enabled: false, Files: []string{"BLITZ.md"}}), "disabled memory loaded files")
	empty := t.TempDir()
	write(t, filepath.Join(empty, "AGENTS.md"), "   \n")
	os.Mkdir(filepath.Join(empty, "BLITZ.md"), 0o755)
	docs = Load(empty, config.MemoryConfig{Enabled: true, Files: []string{"AGENTS.md", "BLITZ.md"}})
	assert.Len(t, docs, 0, "expected nothing, got %+v", docs)
	assert.Equal(t, "", Render(nil), "empty render should be empty")
}

func TestAppend(t *testing.T) {
	ws := t.TempDir()
	path, err := Append(ws, "BLITZ.md", "use tabs")
	require.NoError(t, err)
	Append(ws, "BLITZ.md", "run go vet")
	b, _ := os.ReadFile(path)
	assert.Equal(t, "# Project notes for Blitz\n\n- use tabs\n- run go vet\n", string(b), "append result %q", b)
	_, err = Append(ws, "BLITZ.md", "  ")
	assert.Error(t, err, "expected error for empty note")
}

// Stamp changes with any file LoadAll reads or looks for, and only then.
func TestStamp(t *testing.T) {
	cfg := config.MemoryConfig{Enabled: true, Files: []string{"AGENTS.md", "CLAUDE.md"}, RuleDirs: []string{".agents/rules"}}
	tests := []struct {
		name   string
		change func(t *testing.T, ws string)
		same   bool
	}{
		{"nothing", func(t *testing.T, ws string) {}, true},
		{"a file edited", func(t *testing.T, ws string) { write(t, filepath.Join(ws, "AGENTS.md"), "use tabs, always") }, false},
		{"a file created", func(t *testing.T, ws string) { write(t, filepath.Join(ws, "CLAUDE.md"), "new") }, false},
		{"a rule added", func(t *testing.T, ws string) { write(t, filepath.Join(ws, ".agents", "rules", "new.md"), "rule") }, false},
		{"a rule edited", func(t *testing.T, ws string) {
			write(t, filepath.Join(ws, ".agents", "rules", "style.md"), "no globals, ever")
		}, false},
		{"a rule removed", func(t *testing.T, ws string) {
			require.NoError(t, os.Remove(filepath.Join(ws, ".agents", "rules", "style.md")))
		}, false},
		{"an import edited", func(t *testing.T, ws string) { write(t, filepath.Join(ws, "docs", "more.md"), "more, edited") }, false},
		{"another file", func(t *testing.T, ws string) { write(t, filepath.Join(ws, "README.md"), "not an instruction file") }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws := t.TempDir()
			write(t, filepath.Join(ws, "AGENTS.md"), "use tabs\n@docs/more.md")
			write(t, filepath.Join(ws, "docs", "more.md"), "more")
			write(t, filepath.Join(ws, ".agents", "rules", "style.md"), "no globals")
			// Some file systems keep a second's resolution: date the files back.
			past := time.Now().Add(-time.Hour)
			for _, f := range []string{"AGENTS.md", "docs/more.md", ".agents/rules/style.md"} {
				require.NoError(t, os.Chtimes(filepath.Join(ws, f), past, past))
			}
			loaded := LoadAll(ws, cfg)
			require.Len(t, loaded.Docs, 3, "AGENTS.md, its import and the rule")
			before := Stamp(ws, cfg, loaded)

			tt.change(t, ws)
			if tt.same {
				assert.Equal(t, before, Stamp(ws, cfg, loaded))
			} else {
				assert.NotEqual(t, before, Stamp(ws, cfg, loaded))
			}
		})
	}
	assert.Empty(t, Stamp(t.TempDir(), config.MemoryConfig{}, Loaded{}), "memory off")
}
