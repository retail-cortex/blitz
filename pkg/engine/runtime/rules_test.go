package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/memory"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// A path-scoped rule arrives with the first successful tool call on a
// matching file, once per session, also when the workspace is reached
// through a symlink.
func TestScopedRulesArriveWithTheFirstMatchingFile(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "ws")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
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
	if len(rules) != 1 {
		t.Fatalf("rules %+v", rules)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 5 {
		t.Fatalf("got %d tool results", len(results))
	}
	for i, want := range []bool{false, false, false, true, false} {
		got, _ := results[i][RulesKey].(string)
		if (got != "") != want {
			t.Errorf("result %d: rules %q, want present=%v", i, got, want)
		}
	}
	if got, _ := results[3][RulesKey].(string); !strings.Contains(got, "Use tabs in Go files.") || !strings.Contains(got, "**/*.go") {
		t.Errorf("rendered rule %q", got)
	}
}

func TestPatchPaths(t *testing.T) {
	unified := "--- a/src/x.go\t2026-01-01\n+++ b/src/x.go\n@@ -1 +1 @@\n-a\n+b\n--- /dev/null\n+++ b/new.go\n"
	begin := "*** Begin Patch\n*** Update File: a.go\n*** Move to: b.go\n*** Add File: c.go\n*** Delete File: d.go\n*** End Patch"
	if got := strings.Join(patchPaths(unified), ","); got != "src/x.go,src/x.go,new.go" {
		t.Errorf("unified %q", got)
	}
	if got := strings.Join(patchPaths(begin), ","); got != "a.go,b.go,c.go,d.go" {
		t.Errorf("begin patch %q", got)
	}
}
