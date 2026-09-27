package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/internal/config"
	"github.com/retail-cortex/blitz/internal/runtime"
	"github.com/retail-cortex/blitz/internal/tools"
	"google.golang.org/genai"
)

func toolCall(name string, args map[string]any) *genai.Content {
	return &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: name, Args: args}}}}
}

// The agent can write .git/config; showing the diff must not run what it
// names there.
func TestGitDiffRunsNothingFromRepoConfig(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	w := openTest(t)
	dir := w.Dir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	write("f.txt", "before\n")
	git("add", "f.txt")
	git("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "init")

	write(".gitattributes", "*.txt filter=evil diff=evil\n")
	for key, cmd := range map[string]string{
		"core.fsmonitor":      "touch pwned-fsmonitor",
		"diff.external":       "touch pwned-external",
		"diff.evil.textconv":  "touch pwned-textconv; cat",
		"filter.evil.clean":   "touch pwned-clean; cat",
		"filter.evil.process": "touch pwned-process",
	} {
		git("config", key, cmd)
	}
	git("config", "filter.evil.required", "true")
	write("f.txt", "after\n")

	out, err := w.GitDiff(context.Background(), false)
	if err != nil || !strings.Contains(out, "+after") {
		t.Fatalf("diff %v:\n%s", err, out)
	}
	if got, _ := filepath.Glob(filepath.Join(dir, "pwned-*")); len(got) > 0 {
		t.Errorf("git diff ran commands from the repository's config: %v", got)
	}
}

// Checkpoints outlive the process: after reopening the workspace and
// resuming the session, /undo and /diff still work.
func TestUndoAfterResume(t *testing.T) {
	var cfg *config.Config
	w, _ := openTestWith(t, func(c *config.Config) {
		c.Blitz.AutoApprove = false
		cfg = c
	}, toolCall("create_file", map[string]any{"path": "notes.txt", "content": "hi\n"}), text("created"))
	w.SetUI(func(context.Context, tools.ApprovalRequest) (tools.Decision, error) { return tools.DecisionOnce, nil }, nil)
	s, _, err := w.OpenSession("", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Run(context.Background(), s.ID, Turn{Text: "make notes"}, func(Event) {}); err != nil {
		t.Fatal(err)
	}
	cps := w.ListCheckpoints()
	if len(cps) != 1 {
		t.Fatalf("checkpoints %+v", cps)
	}
	w.Close()

	w2, err := Open(context.Background(), cfg, Options{Model: runtime.NewMockLLM("gemini-3.8-flash"), NewModel: mockModels})
	if err != nil {
		t.Fatal(err)
	}
	defer w2.Close()
	if _, _, err := w2.OpenSession(s.ID, false); err != nil {
		t.Fatal(err)
	}
	if d := w2.SessionDiff(); !strings.Contains(d, "+hi") {
		t.Errorf("diff after resuming:\n%s", d)
	}
	if res, err := w2.Undo(false); err != nil || res.Label != "make notes" {
		t.Fatalf("undo after reopening: %+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(cfg.Tools.WorkspaceDir, "notes.txt")); !os.IsNotExist(err) {
		t.Error("notes.txt is still there")
	}
}
