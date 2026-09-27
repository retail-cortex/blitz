package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/config"
	"google.golang.org/genai"
)

func TestPermissionRulesLiveAndSaved(t *testing.T) {
	create := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "create_file", Args: map[string]any{"path": "docs/a.md", "content": "x"}}}}}
	search := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "list_agents", Args: map[string]any{}}}}}
	w, _ := openTestWith(t, func(c *config.Config) { c.Permissions.Deny = []string{"shell(rm *)"} }, create, text("done"), search, text("done"))
	cfgDir := config.ConfigDir("")
	os.MkdirAll(cfgDir, 0o700)
	os.WriteFile(filepath.Join(cfgDir, ".env.toml"), []byte("# mine\n[permissions]\ndeny = [\"shell(rm *)\"]\n"), 0o600)

	if got := w.ListPermissionRules(); len(got) != 1 || got[0].Rule != "shell(rm *)" || got[0].Source != "config" {
		t.Fatalf("rules %+v", got)
	}
	// A session allow rule applies at once: no approver, yet the write runs.
	res, err := w.AddPermissionRule("allow", "Edit(docs/**)", true)
	if err != nil || res.Rule != "write(docs/**)" || res.Saved.Err != nil {
		t.Fatalf("add: %+v %v", res, err)
	}
	s, _ := w.NewSession()
	w.Run(context.Background(), s.ID, api.Turn{Text: "write"}, func(api.Event) {})
	if _, err := os.Stat(filepath.Join(w.Dir(), "docs", "a.md")); err != nil {
		t.Errorf("allow rule didn't apply: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(cfgDir, ".env.toml"))
	if !strings.Contains(string(data), `allow = ["write(docs/**)"]`) || !strings.Contains(string(data), "# mine") {
		t.Errorf("config file:\n%s", data)
	}
	// Read rules must be saved and apply from the next start.
	if _, err := w.AddPermissionRule("deny", "read(secrets/**)", false); err == nil {
		t.Error("an unsaved read rule was accepted")
	}
	if res, err := w.AddPermissionRule("deny", "read(secrets/**)", true); err != nil || !res.NextStart {
		t.Errorf("saved read rule: %+v %v", res, err)
	}
	if _, err := w.AddPermissionRule("deny", "what(x)", false); !errors.Is(err, api.ErrBadRule) {
		t.Errorf("bad rule: %v", err)
	}
	// Remove, also from the file.
	if res, _ := w.RemovePermissionRule("write(docs/**)", true); res.Removed != 1 {
		t.Errorf("remove: %+v", res)
	}
	data, _ = os.ReadFile(filepath.Join(cfgDir, ".env.toml"))
	if strings.Contains(string(data), "write(docs/**)") {
		t.Errorf("rule still saved:\n%s", data)
	}
}

// A deny rule naming a tool refuses the call before it runs.
func TestToolDenyRuleInTheEngine(t *testing.T) {
	agents := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "list_agents", Args: map[string]any{}}}}}
	w, _ := openTestWith(t, func(c *config.Config) { c.Permissions.Deny = []string{"list_agents"} }, agents, text("ok"))
	s, _ := w.NewSession()
	var result map[string]any
	w.Run(context.Background(), s.ID, api.Turn{Text: "who"}, func(e api.Event) {
		if e.ToolResult != nil {
			result = e.ToolResult.Result
		}
	})
	if msg, _ := result["error"].(string); !strings.Contains(msg, "deny list_agents") {
		t.Errorf("list_agents wasn't refused: %v", result)
	}
}
