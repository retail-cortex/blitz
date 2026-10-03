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

package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// A path-scoped rule arrives with the first successful tool call on a
// matching file, once per session, also when the workspace is reached
// through a symlink.
func TestScopedRulesArriveWithTheFirstMatchingFile(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "ws")
	require.NoError(t, os.Symlink(real, link))
	for name, body := range map[string]string{
		"a.go": "package a", "b.go": "package b", "notes.txt": "notes",
		".blitz/rules/go.md": "---\npaths: [\"**/*.go\"]\n---\nUse tabs in Go files.",
	} {
		p := filepath.Join(real, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	mc := config.MemoryConfig{Enabled: true, RuleDirs: []string{".blitz/rules"}}
	rules := memory.LoadAll(link, mc).Rules
	require.Len(t, rules, 1, "rules %+v", rules)
	read := func(p string) *genai.Content { return toolCall("read_file", map[string]any{"path": p}) }
	f := newEngineWith(t, fixtureOpts{
		cfg:  func(c *config.Config) { c.Tools.WorkspaceDir = link },
		opts: []Option{WithScopedRules(rules)},
	},
		read("notes.txt"), read("missing.go"), toolCall("list_files", map[string]any{}), read("a.go"), read("b.go"), textContent("done"))

	var results []map[string]any
	err := f.eng.Execute(context.Background(), "s", "look", func(ev *session.Event) error {
		if ev.Content != nil {
			for _, p := range ev.Content.Parts {
				if p.FunctionResponse != nil {
					results = append(results, p.FunctionResponse.Response)
				}
			}
		}
		return nil
	})
	require.NoError(t, err)
	require.Len(t, results, 5, "got %d tool results", len(results))
	for i, want := range []bool{false, false, false, true, false} {
		got, _ := results[i][RulesKey].(string)
		assert.Equal(t, want, (got != ""), "result %d: rules %q, want present=%v", i, got, want)
	}
	got, _ := results[3][RulesKey].(string)
	assert.Contains(t, got, "Use tabs in Go files.", "rendered rule %q", got)
	assert.Contains(t, got, "**/*.go", "rendered rule %q", got)
}

func TestPatchPaths(t *testing.T) {
	unified := "--- a/src/x.go\t2026-01-01\n+++ b/src/x.go\n@@ -1 +1 @@\n-a\n+b\n--- /dev/null\n+++ b/new.go\n"
	begin := "*** Begin Patch\n*** Update File: a.go\n*** Move to: b.go\n*** Add File: c.go\n*** Delete File: d.go\n*** End Patch"
	got := strings.Join(patchPaths(unified), ",")
	assert.Equal(t, "src/x.go,src/x.go,new.go", got, "unified %q", got)
	got = strings.Join(patchPaths(begin), ",")
	assert.Equal(t, "a.go,b.go,c.go,d.go", got, "begin patch %q", got)
}

func TestToolPaths(t *testing.T) {
	ws := t.TempDir()
	real, err := filepath.EvalSymlinks(ws)
	require.NoError(t, err)
	tests := []struct {
		tool string
		args map[string]any
		want []string
	}{
		{"read_file", map[string]any{"path": "a.go"}, []string{"a.go"}},
		{"export_pdf", map[string]any{"path": "notes/w1.md"}, []string{"notes/w1.md", "notes/w1.pdf"}},
		{"export_pdf", map[string]any{"path": "w1.md", "output": "out/w1.pdf"}, []string{"w1.md", "out/w1.pdf"}},
		{"generate_audio", map[string]any{"text_path": "s.md", "path": "audio/o.wav"}, []string{"s.md", "audio/o.wav"}},
		{"export_pdf", map[string]any{"markdown": "# x", "output": "summary.pdf"}, []string{"summary.pdf"}},
		{"list_files", map[string]any{"path": "."}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.tool, func(t *testing.T) {
			for _, w := range tc.want { // existing files resolve through symlinks
				require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(ws, w)), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(ws, w), nil, 0o644))
			}
			got := toolPaths(ws, tc.tool, tc.args)
			require.Len(t, got, len(tc.want))
			for i, w := range tc.want {
				assert.Equal(t, filepath.Join(real, w), got[i])
			}
		})
	}
}
