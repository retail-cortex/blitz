package tools

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/internal/config"
	"google.golang.org/adk/v2/tool"
)

func rulesOf(t *testing.T, allow, ask, deny []string) *PermissionRules {
	t.Helper()
	r, err := NewPermissionRules(config.PermissionsConfig{Allow: allow, Ask: ask, Deny: deny}, "config")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestParsePermissionRule(t *testing.T) {
	for text, want := range map[string]string{
		"shell(git *)": "shell(git *)", "Bash(npm run test)": "shell(npm run test)", "Edit(src/**)": "write(src/**)",
		"write( docs/** )": "write(docs/**)", "web(*.go.dev)": "web(*.go.dev)", "mcp(github:create_*)": "mcp(github:create_*)",
		"web_search": "web_search", "shell": "shell(*)", "skill(deploy)": "skill(deploy)", "agent(qa)": "agent(qa)",
	} {
		r, err := ParsePermissionRule(EffectAllow, text, "t")
		if err != nil || r.String() != want {
			t.Errorf("%q = %q %v, want %q", text, r.String(), err, want)
		}
	}
	for _, bad := range []string{"", "nope(x)", "shell()", "shell(x", "1tool", "bad name"} {
		if _, err := ParsePermissionRule(EffectDeny, bad, "t"); !errors.Is(err, api.ErrBadRule) {
			t.Errorf("%q should be invalid: %v", bad, err)
		}
	}
	if _, err := ParsePermissionRule(EffectAllow, "read(src/**)", "t"); err == nil {
		t.Error("allow read(...) should be refused: reading never asks")
	}
	if _, err := ParsePermissionRule("maybe", "shell(x)", "t"); err == nil {
		t.Error("unknown effect accepted")
	}
}

func TestRulesDecide(t *testing.T) {
	r := rulesOf(t, []string{"write(docs/**)", "write(guide/)", "write(README.md)", "web(*.go.dev)"}, []string{"write(docs/secret/**)"}, []string{"write(**/*.pem)", "web(evil.test)"})
	cases := []struct {
		kind    string
		targets []string
		want    Effect
	}{
		{RuleWrite, []string{"docs/a.md"}, EffectAllow},
		{RuleWrite, []string{"docs"}, ""},           // docs/** is what is under docs
		{RuleWrite, []string{"guide"}, EffectAllow}, // guide/ covers the directory too
		{RuleWrite, []string{"guide/x/y.md"}, EffectAllow},
		{RuleWrite, []string{"docs/a.md", "README.md"}, EffectAllow},
		{RuleWrite, []string{"docs/a.md", "src/x.go"}, ""}, // allow needs every target
		{RuleWrite, []string{"docs/secret/k.txt"}, EffectAsk},
		{RuleWrite, []string{"docs/a.md", "keys/x.pem"}, EffectDeny}, // deny needs one
		{RuleWeb, []string{"pkg.go.dev"}, EffectAllow},
		{RuleWeb, []string{"go.dev"}, EffectAllow},
		{RuleWeb, []string{"EVIL.test"}, EffectDeny},
		{RuleWeb, []string{"example.com"}, ""},
		{RuleDelete, []string{"docs/a.md"}, ""}, // kinds don't mix
		{RuleWrite, nil, ""},
	}
	for _, c := range cases {
		if got, _ := r.Decide(c.kind, c.targets); got != c.want {
			t.Errorf("Decide(%s, %v) = %q, want %q", c.kind, c.targets, got, c.want)
		}
	}
	var nilRules *PermissionRules
	if got, _ := nilRules.Decide(RuleWrite, []string{"x"}); got != "" {
		t.Error("nil rules decided something")
	}
}

func TestRulesAtTheGate(t *testing.T) {
	write := func(p string) api.ApprovalRequest {
		return api.ApprovalRequest{Tool: "create_file", Kind: api.ActionWrite, Detail: "Create " + p, Key: "write:/ws", Targets: []string{p}}
	}
	rules := rulesOf(t, []string{"write(docs/**)"}, []string{"write(ci/**)"}, []string{"write(**/*.pem)"})

	// deny wins even in bypass mode, and for unattended runs.
	h := NewHooks(Policy{Mode: api.ModeBypass})
	h.SetRules(rules)
	if err := h.Approve(context.Background(), write("k.pem")); err == nil || !strings.Contains(err.Error(), "deny write(**/*.pem)") {
		t.Errorf("deny in bypass: %v", err)
	}
	permitAll := func(context.Context, api.ApprovalRequest) (api.Decision, error) { return api.DecisionOnce, nil }
	if err := h.Approve(Unattended(context.Background(), permitAll), write("k.pem")); err == nil {
		t.Error("deny rule let an unattended run through")
	}

	// ask asks even in bypass mode and after "always"; in dont-ask it refuses.
	asked := 0
	h.SetApprover(func(context.Context, api.ApprovalRequest) (api.Decision, error) {
		asked++
		return api.DecisionSession, nil
	})
	for range 2 {
		if err := h.Approve(context.Background(), write("ci/deploy.yml")); err != nil {
			t.Fatal(err)
		}
	}
	if asked != 2 {
		t.Errorf("ask rule asked %d times, want 2", asked)
	}
	dontAsk := NewHooks(Policy{Mode: api.ModeDontAsk})
	dontAsk.SetRules(rules)
	if err := dontAsk.Approve(context.Background(), write("ci/x")); err == nil {
		t.Error("dont-ask let an ask-rule action through")
	}
	if err := h.Approve(Unattended(context.Background(), permitAll), write("ci/x")); err == nil {
		t.Error("an unattended run can't be asked, so an ask rule must refuse")
	}

	// allow skips the question in default mode, but never widens an
	// unattended run's own permissions.
	def, reqs := approverHooks(false)
	def.SetRules(rules)
	if err := def.Approve(context.Background(), write("docs/a.md")); err != nil || len(*reqs) != 0 {
		t.Errorf("allow rule: %v, %d prompts", err, len(*reqs))
	}
	denyAll := func(context.Context, api.ApprovalRequest) (api.Decision, error) { return api.DecisionDeny, nil }
	if err := def.Approve(Unattended(context.Background(), denyAll), write("docs/a.md")); err == nil {
		t.Error("an allow rule widened an unattended run")
	}
}

func TestShellRulesInTheCommandPolicy(t *testing.T) {
	p, _ := NewCommandPolicy(CommandPolicyConfig{})
	p.SetRules(rulesOf(t, []string{"shell(git *)", "shell(go test *)"}, []string{"shell(git push *)"}, []string{"shell(rm -rf *)"}))
	for script, want := range map[string]struct {
		v       Verdict
		mustAsk bool
	}{
		"git status":             {VerdictAutoApprove, false},
		"go test ./...":          {VerdictAutoApprove, false},
		"git push origin main":   {VerdictNeedsApproval, true},
		"env GIT_DIR=x git push": {VerdictNeedsApproval, true}, // through a wrapper
		"git status && git push": {VerdictNeedsApproval, true},
		"rm -rf build":           {VerdictDeny, false},
		"bash -c 'rm -rf build'": {VerdictDeny, false},
		"make build":             {VerdictNeedsApproval, false},
		"eval \"$CMD\"":          {VerdictNeedsApproval, true}, // can't rule out an ask rule
	} {
		d := p.Evaluate(script)
		if d.Verdict != want.v || d.MustAsk != want.mustAsk {
			t.Errorf("%q: verdict %v mustAsk %v (%s), want %v %v", script, d.Verdict, d.MustAsk, d.Reason, want.v, want.mustAsk)
		}
	}
}

func TestReadDenyRulesBecomeBlockedPaths(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Tools.ApprovalsFile = ""
	cfg.Images.Enabled = false
	cfg.Permissions.Deny = []string{"read(secrets/**)"}
	os.MkdirAll(filepath.Join(cfg.Tools.WorkspaceDir, "secrets"), 0o755)
	os.WriteFile(filepath.Join(cfg.Tools.WorkspaceDir, "secrets", "db.txt"), []byte("pw"), 0o644)
	reg, err := NewRegistry(cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	if _, err := reg.Workspace().ReadFile("secrets/db.txt"); !errors.Is(err, ErrBlockedPath) {
		t.Errorf("read rule didn't block: %v", err)
	}
	cfg.Permissions.Deny = []string{"nonsense(x)"}
	if _, err := NewRegistry(cfg, nil, nil); err == nil {
		t.Error("a bad rule in config was accepted")
	}
}

func TestWebRules(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
	defer srv.Close()
	ctx := context.Background()
	// A deny rule beats allow_domains; an ask rule asks despite it.
	f := testFetcher(WebFetchConfig{AllowPrivate: true, AllowDomains: []string{"127.0.0.1"}, Rules: rulesOf(t, nil, nil, []string{"web(127.0.0.1)"})})
	if out := f.fetch(ctx, allowAll(), srv.URL); !strings.Contains(out.Error, "denied by the permission rule") {
		t.Errorf("deny: %+v", out)
	}
	f = testFetcher(WebFetchConfig{AllowPrivate: true, AllowDomains: []string{"127.0.0.1"}, Rules: rulesOf(t, nil, []string{"web(127.0.0.1)"}, nil)})
	h, reqs := approverHooks(true)
	if out := f.fetch(ctx, h, srv.URL); out.Content != "ok" || len(*reqs) != 1 || !(*reqs)[0].MustAsk {
		t.Errorf("ask: %+v, prompts %+v", out, *reqs)
	}
}

func TestMCPRulesBeatAutoApprove(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Tools.ApprovalsFile = ""
	cfg.Permissions.Deny = []string{"mcp(gh:delete_*)"}
	reg, err := NewRegistry(cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	reg.mcp = newMCPManagerWithToolsets(map[string]tool.Toolset{"gh": inMemoryMCP(t, "delete_repo")}, map[string]bool{"gh": true}, nil)
	for _, ts := range reg.mcp.Toolsets() {
		ts.Tools(createTestToolContext())
	}
	if err := reg.ApproveMCP(context.Background(), "delete_repo", nil); err == nil || !strings.Contains(err.Error(), "deny mcp(gh:delete_*)") {
		t.Errorf("auto_approve server ignored a deny rule: %v", err)
	}
}

func TestToolDenied(t *testing.T) {
	r := rulesOf(t, nil, nil, []string{"web_search", "agent(helios)", "skill(deploy-*)"})
	for _, c := range []struct {
		tool string
		args map[string]any
		deny bool
	}{
		{"web_search", nil, true},
		{"WEB_SEARCH", nil, true},
		{"web_fetch", nil, false},
		{"invoke_agent", map[string]any{"agent_name": "helios"}, true},
		{"invoke_agent", map[string]any{"agent_name": "qa"}, false},
		{"activate_skill", map[string]any{"skill_name": "deploy-prod"}, true},
		{"run_skill_script", map[string]any{"skill": "deploy-prod"}, true},
		{"run_skill_script", map[string]any{"skill": "lint"}, false},
	} {
		if got := r.ToolDenied(c.tool, c.args) != nil; got != c.deny {
			t.Errorf("%s %v: denied %v, want %v", c.tool, c.args, got, c.deny)
		}
	}
}

func TestRulesAddRemove(t *testing.T) {
	r := rulesOf(t, nil, nil, nil)
	if err := r.Add(EffectAllow, "Bash(go test *)", "session"); err != nil {
		t.Fatal(err)
	}
	r.Add(EffectAllow, "shell(go test *)", "session") // duplicate
	if l := r.List(); len(l) != 1 || l[0].String() != "shell(go test *)" || l[0].Source != "session" {
		t.Errorf("list %+v", l)
	}
	if n := r.Remove("Bash(go test *)", ""); n != 1 || len(r.List()) != 0 {
		t.Errorf("remove: %d, left %+v", n, r.List())
	}
}
