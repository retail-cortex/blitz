package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
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
	if got := strings.Join(contents(docs), "|"); got != "shared rules|gemini rules|my own rules" {
		t.Errorf("docs %q", got)
	}
	if !docs[2].Local || docs[0].Local {
		t.Errorf("local flags: %+v", docs)
	}
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
	if len(got) != 3 || !strings.HasPrefix(got[0], "root rules") || got[1] != "style guide\nsee @deep.md" || got[2] != "deep file @style.md" {
		t.Fatalf("imports %q", got)
	}
	if docs[1].ImportedFrom != filepath.Join(repo, "AGENTS.md") || docs[2].ImportedFrom != filepath.Join(repo, "docs", "style.md") {
		t.Errorf("imported-from %+v", docs)
	}
	if r := Render(docs); !strings.Contains(r, "(imported by ") {
		t.Errorf("render doesn't say where imports came from:\n%s", r)
	}
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
	if docs := Load(ws, defaults()); len(docs) != 1+maxImportDepth {
		t.Errorf("loaded %d files, want %d", len(docs), 1+maxImportDepth)
	}
}

func TestRules(t *testing.T) {
	repo := t.TempDir()
	os.Mkdir(filepath.Join(repo, ".git"), 0o755)
	write(t, filepath.Join(repo, ".blitz", "rules", "always.md"), "always applies")
	write(t, filepath.Join(repo, ".agents", "rules", "api.md"), "---\npaths: [\"src/api/**/*.go\", \"cmd/*.go\"]\n---\nAPI rules")
	write(t, filepath.Join(repo, ".claude", "rules", "sub", "tests.md"), "---\npaths: \"**/*_test.go\"\n---\nTest rules")
	write(t, filepath.Join(repo, ".blitz", "rules", "notes.txt"), "not a rule")
	l := LoadAll(repo, defaults())
	if got := contents(l.Docs); len(got) != 1 || got[0] != "always applies" {
		t.Errorf("unscoped rules as docs: %q", got)
	}
	if len(l.Rules) != 2 {
		t.Fatalf("scoped rules %+v", l.Rules)
	}
	real, _ := filepath.EvalSymlinks(repo)
	api, tests := &l.Rules[0], &l.Rules[1]
	if api.Content != "API rules" || tests.Content != "Test rules" {
		t.Errorf("rule bodies %q %q", api.Content, tests.Content)
	}
	for p, want := range map[string]bool{"src/api/v1/h.go": true, "cmd/main.go": true, "cmd/x/main.go": false, "src/web/a.go": false} {
		if got := api.Matches(filepath.Join(real, p)); got != want {
			t.Errorf("api rule matches %s = %v", p, got)
		}
	}
	if !tests.Matches(filepath.Join(real, "a", "b_test.go")) || tests.Matches(filepath.Join(t.TempDir(), "x_test.go")) {
		t.Error("test rule matching")
	}
	if r := Render(l.Docs, true); !strings.Contains(r, "project_rules") {
		t.Error("render should announce scoped rules")
	}
}
