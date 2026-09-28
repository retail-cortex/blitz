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

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func defaults() config.MemoryConfig {
	return config.MemoryConfig{
		Enabled: true, MaxBytes: 32 * 1024,
		Files:      []string{"AGENTS.md", "CLAUDE.md", "GEMINI.md", "BLITZ.md"},
		LocalFiles: []string{"CLAUDE.local.md", "BLITZ.local.md"},
		RuleDirs:   []string{".blitz/rules", ".agents/rules", ".claude/rules"},
	}
}

func contents(docs []Doc) []string {
	var out []string
	for _, d := range docs {
		out = append(out, d.Content)
	}
	return out
}

func TestOtherAgentsFilesAndDuplicates(t *testing.T) {
	ws := t.TempDir()
	write(t, filepath.Join(ws, "AGENTS.md"), "shared rules")
	write(t, filepath.Join(ws, "CLAUDE.md"), "shared rules\n") // a copy: loaded once
	write(t, filepath.Join(ws, "GEMINI.md"), "gemini rules")
	write(t, filepath.Join(ws, "BLITZ.local.md"), "my own rules")
	docs := Load(ws, defaults())
	got := strings.Join(contents(docs), "|")
	assert.Equal(t, "shared rules|gemini rules|my own rules", got, "docs %q", got)
	assert.True(t, docs[2].Local, "local flags: %+v", docs)
	assert.False(t, docs[0].Local, "local flags: %+v", docs)
}

func TestImports(t *testing.T) {
	repo := t.TempDir()
	os.Mkdir(filepath.Join(repo, ".git"), 0o755)
	outside := t.TempDir()
	write(t, filepath.Join(outside, "secret.md"), "outside the repo")
	write(t, filepath.Join(repo, ".env"), "API_KEY=x")
	write(t, filepath.Join(repo, "docs", "style.md"), "style guide\nsee @deep.md")
	write(t, filepath.Join(repo, "docs", "deep.md"), "deep file @style.md") // a cycle back
	write(t, filepath.Join(repo, "AGENTS.md"), strings.Join([]string{
		"root rules; read @docs/style.md.",
		"mail me@example.com and ping @team",
		"not code: `@docs/nope.md`",
		"```",
		"@docs/fenced.md",
		"```",
		"@" + filepath.Join(outside, "secret.md"),
		"@.env",
		"@missing/file.md",
	}, "\n"))
	write(t, filepath.Join(repo, "docs", "nope.md"), "inline code import")
	write(t, filepath.Join(repo, "docs", "fenced.md"), "fenced import")

	blocked := func(p string) bool { return filepath.Base(p) == ".env" }
	docs := Load(repo, defaults(), Options{Blocked: blocked})
	got := contents(docs)
	require.Len(t, got, 3, "imports %q", got)
	require.True(t, strings.HasPrefix(got[0], "root rules"), "imports %q", got)
	require.Equal(t, "style guide\nsee @deep.md", got[1], "imports %q", got)
	require.Equal(t, "deep file @style.md", got[2], "imports %q", got)
	assert.Equal(t, filepath.Join(repo, "AGENTS.md"), docs[1].ImportedFrom, "imported-from %+v", docs)
	assert.Equal(t, filepath.Join(repo, "docs", "style.md"), docs[2].ImportedFrom, "imported-from %+v", docs)
	r := Render(docs)
	assert.Contains(t, r, "(imported by ", "render doesn't say where imports came from:\n%s", r)
}

func TestImportDepthIsBounded(t *testing.T) {
	ws := t.TempDir()
	write(t, filepath.Join(ws, "AGENTS.md"), "level 0 @l1.md")
	for i := 1; i <= 8; i++ {
		next := ""
		if i < 8 {
			next = " @l" + string(rune('0'+i+1)) + ".md"
		}
		write(t, filepath.Join(ws, "l"+string(rune('0'+i))+".md"), "level "+string(rune('0'+i))+next)
	}
	docs := Load(ws, defaults())
	assert.Len(t, docs, 1+maxImportDepth, "loaded %d files, want %d", len(docs), 1+maxImportDepth)
}

func TestRules(t *testing.T) {
	repo := t.TempDir()
	os.Mkdir(filepath.Join(repo, ".git"), 0o755)
	write(t, filepath.Join(repo, ".blitz", "rules", "always.md"), "always applies")
	write(t, filepath.Join(repo, ".agents", "rules", "api.md"), "---\npaths: [\"src/api/**/*.go\", \"cmd/*.go\"]\n---\nAPI rules")
	write(t, filepath.Join(repo, ".claude", "rules", "sub", "tests.md"), "---\npaths: \"**/*_test.go\"\n---\nTest rules")
	write(t, filepath.Join(repo, ".blitz", "rules", "notes.txt"), "not a rule")
	l := LoadAll(repo, defaults())
	got := contents(l.Docs)
	assert.Len(t, got, 1, "unscoped rules as docs: %q", got)
	assert.Equal(t, "always applies", got[0], "unscoped rules as docs: %q", got)
	require.Len(t, l.Rules, 2, "scoped rules %+v", l.Rules)
	real, _ := filepath.EvalSymlinks(repo)
	api, tests := &l.Rules[0], &l.Rules[1]
	assert.Equal(t, "API rules", api.Content, "rule bodies %q %q", api.Content, tests.Content)
	assert.Equal(t, "Test rules", tests.Content, "rule bodies %q %q", api.Content, tests.Content)
	for p, want := range map[string]bool{"src/api/v1/h.go": true, "cmd/main.go": true, "cmd/x/main.go": false, "src/web/a.go": false} {
		t.Run(p, func(t *testing.T) {
			got := api.Matches(filepath.Join(real, p))
			assert.Equal(t, want, got, "api rule matches %s = %v", p, got)
		})
	}
	assert.True(t, tests.Matches(filepath.Join(real, "a", "b_test.go")), "test rule matching")
	assert.False(t, tests.Matches(filepath.Join(t.TempDir(), "x_test.go")), "test rule matching")
	r := Render(l.Docs, true)
	assert.Contains(t, r, "project_rules", "render should announce scoped rules")
}
