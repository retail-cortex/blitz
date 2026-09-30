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
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/tool"
)

func rulesOf(t *testing.T, allow, ask, deny []string) *PermissionRules {
	t.Helper()
	off := false
	r, err := NewPermissionRules(config.PermissionsConfig{Allow: allow, Ask: ask, Deny: deny, ReadOnlyDefaults: &off}, "config")
	require.NoError(t, err)
	return r
}

// With read_only_defaults on (the default), read-only commands run without
// asking, their writing options ask, and redirections to files ask.
func TestReadOnlyDefaults(t *testing.T) {
	r, err := NewPermissionRules(config.PermissionsConfig{}, SourceConfig)
	require.NoError(t, err)
	p := mustPolicy(t, CommandPolicyConfig{})
	p.SetRules(r)
	for _, tc := range []struct {
		cmd     string
		verdict Verdict
		mustAsk bool
	}{
		{"ls -la", VerdictAutoApprove, false},
		{"git log --oneline -5 | head -3", VerdictAutoApprove, false},
		{"grep -rn TODO pkg", VerdictAutoApprove, false},
		{"git status && git diff", VerdictAutoApprove, false},
		{"git push", VerdictNeedsApproval, false},
		{"rm -rf x", VerdictNeedsApproval, false},
		{"find . -delete", VerdictNeedsApproval, false},
		{"cat a > b", VerdictNeedsApproval, false},
		{"git log --output=log.txt", VerdictNeedsApproval, true},
		{"git diff --ext-diff", VerdictNeedsApproval, true},
		{"rg --pre ./x TODO", VerdictNeedsApproval, true},
	} {
		t.Run(tc.cmd, func(t *testing.T) {
			d := p.Evaluate(tc.cmd)
			assert.Equal(t, tc.verdict, d.Verdict, d.Reason)
			assert.Equal(t, tc.mustAsk, d.MustAsk, d.Reason)
		})
	}
	for _, rule := range r.List() {
		assert.Equal(t, SourceBuiltIn, rule.Source, "%s", rule)
	}

	// Off: none of them.
	off := false
	none, err := NewPermissionRules(config.PermissionsConfig{ReadOnlyDefaults: &off}, SourceConfig)
	require.NoError(t, err)
	assert.Empty(t, none.List())
}

// Settings changed while a session runs replace the configured rules and
// the built-in ones, not the session's.
func TestReplaceConfigured(t *testing.T) {
	r, err := NewPermissionRules(config.PermissionsConfig{Allow: []string{"shell(make)"}}, SourceConfig)
	require.NoError(t, err)
	require.NoError(t, r.Add(EffectDeny, "shell(rm)", SourceSession))
	off := false
	require.NoError(t, r.ReplaceConfigured(config.PermissionsConfig{Ask: []string{"shell(git push)"}, ReadOnlyDefaults: &off}))
	var got []string
	for _, x := range r.List() {
		got = append(got, string(x.Effect)+" "+x.String()+" "+x.Source)
	}
	assert.Equal(t, []string{"deny shell(rm) session", "ask shell(git push) config"}, got)
}

// A rule is checked, and described, before it's saved.
func TestCheckPermissionRule(t *testing.T) {
	for _, tc := range []struct {
		effect  Effect
		rule    string
		sample  string
		want    RuleCheck
		wantErr string
	}{
		{EffectAllow, "Bash(git log)", "", RuleCheck{Rule: "shell(git log)", Kind: RuleShell, Pattern: "git log", Form: FormPrefix}, ""},
		{EffectAllow, "shell(git log)", "git log --oneline | head", RuleCheck{Rule: "shell(git log)", Kind: RuleShell, Pattern: "git log", Form: FormPrefix, Tested: true}, ""},
		{EffectAllow, "shell(git)", "git log --oneline", RuleCheck{Rule: "shell(git)", Kind: RuleShell, Pattern: "git", Form: FormPrefix, Tested: true, Matches: true}, ""},
		{EffectDeny, "shell(git push)", "git fetch && git push", RuleCheck{Rule: "shell(git push)", Kind: RuleShell, Pattern: "git push", Form: FormPrefix, Tested: true, Matches: true}, ""},
		{EffectAllow, "shell(re:go (test|vet)( .*)?)", "go vet ./...", RuleCheck{Rule: "shell(re:go (test|vet)( .*)?)", Kind: RuleShell, Pattern: "re:go (test|vet)( .*)?", Form: FormRegex, Tested: true, Matches: true}, ""},
		{EffectAllow, "shell(git log)", "git log > out.txt", RuleCheck{Rule: "shell(git log)", Kind: RuleShell, Pattern: "git log", Form: FormPrefix, Tested: true, Matches: true, Redirect: "out.txt"}, ""},
		{EffectAllow, "write(docs/**)", "docs/a/b.md", RuleCheck{Rule: "write(docs/**)", Kind: RuleWrite, Pattern: "docs/**", Form: "path", Tested: true, Matches: true}, ""},
		{EffectAllow, "shell(re:()", "", RuleCheck{}, "invalid"},
		{EffectAllow, "shel(ls)", "", RuleCheck{}, "unknown kind"},
		{EffectAllow, "read(x)", "", RuleCheck{}, "only deny"},
	} {
		t.Run(tc.rule+" "+tc.sample, func(t *testing.T) {
			got, err := CheckPermissionRule(tc.effect, tc.rule, tc.sample)
			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestParsePermissionRule(t *testing.T) {
	for text, want := range map[string]string{
		"shell(git *)": "shell(git *)", "Bash(npm run test)": "shell(npm run test)", "Edit(src/**)": "write(src/**)",
		"write( docs/** )": "write(docs/**)", "web(*.go.dev)": "web(*.go.dev)", "mcp(github:create_*)": "mcp(github:create_*)",
		"web_search": "web_search", "shell": "shell(*)", "skill(deploy)": "skill(deploy)", "agent(qa)": "agent(qa)",
	} {
		t.Run(text, func(t *testing.T) {
			r, err := ParsePermissionRule(EffectAllow, text, "t")
			assert.NoError(t, err, "%q = %q %v, want %q", text, r.String(), err, want)
			assert.Equal(t, want, r.String(), "%q = %q %v, want %q", text, r.String(), err, want)
		})
	}
	for _, bad := range []string{"", "nope(x)", "shell()", "shell(x", "1tool", "bad name"} {
		t.Run(bad, func(t *testing.T) {
			_, err := ParsePermissionRule(EffectDeny, bad, "t")
			assert.ErrorIs(t, err, api.ErrBadRule, "%q should be invalid: %v", bad, err)
		})
	}
	_, err := ParsePermissionRule(EffectAllow, "read(src/**)", "t")
	assert.Error(t, err, "allow read(...) should be refused: reading never asks")
	_, err = ParsePermissionRule("maybe", "shell(x)", "t")
	assert.Error(t, err, "unknown effect accepted")
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
		got, _ := r.Decide(c.kind, c.targets)
		assert.Equal(t, c.want, got, "Decide(%s, %v) = %q, want %q", c.kind, c.targets, got, c.want)
	}
	var nilRules *PermissionRules
	got, _ := nilRules.Decide(RuleWrite, []string{"x"})
	assert.Equal(t, Effect(""), got, "nil rules decided something")
}

func TestRulesAtTheGate(t *testing.T) {
	write := func(p string) api.ApprovalRequest {
		return api.ApprovalRequest{Tool: "create_file", Kind: api.ActionWrite, Detail: "Create " + p, Key: "write:/ws", Targets: []string{p}}
	}
	rules := rulesOf(t, []string{"write(docs/**)"}, []string{"write(ci/**)"}, []string{"write(**/*.pem)"})

	// deny wins even in bypass mode, and for unattended runs.
	h := NewHooks(Policy{Mode: api.ModeBypass})
	h.SetRules(rules)
	err := h.Approve(context.Background(), write("k.pem"))
	assert.Error(t, err, "deny in bypass")
	assert.Contains(t, err.Error(), "deny write(**/*.pem)", "deny in bypass: %v", err)
	permitAll := func(context.Context, api.ApprovalRequest) (api.Decision, error) { return api.DecisionOnce, nil }
	assert.Error(t, h.Approve(Unattended(context.Background(), permitAll), write("k.pem")), "deny rule let an unattended run through")

	// ask asks even in bypass mode and after "always"; in dont-ask it refuses.
	asked := 0
	h.SetApprover(func(context.Context, api.ApprovalRequest) (api.Decision, error) {
		asked++
		return api.DecisionSession, nil
	})
	for range 2 {
		require.NoError(t, h.Approve(context.Background(), write("ci/deploy.yml")))
	}
	assert.Equal(t, 2, asked, "ask rule asked %d times, want 2", asked)
	dontAsk := NewHooks(Policy{Mode: api.ModeDontAsk})
	dontAsk.SetRules(rules)
	assert.Error(t, dontAsk.Approve(context.Background(), write("ci/x")), "dont-ask let an ask-rule action through")
	assert.Error(t, h.Approve(Unattended(context.Background(), permitAll), write("ci/x")), "an unattended run can't be asked, so an ask rule must refuse")

	// allow skips the question in default mode, but never widens an
	// unattended run's own permissions.
	def, reqs := approverHooks(false)
	def.SetRules(rules)
	err = def.Approve(context.Background(), write("docs/a.md"))
	assert.NoError(t, err, "allow rule: %v, %d prompts", err, len(*reqs))
	assert.Len(t, *reqs, 0, "allow rule: %v, %d prompts", err, len(*reqs))
	denyAll := func(context.Context, api.ApprovalRequest) (api.Decision, error) { return api.DecisionDeny, nil }
	assert.Error(t, def.Approve(Unattended(context.Background(), denyAll), write("docs/a.md")), "an allow rule widened an unattended run")
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
		t.Run(script, func(t *testing.T) {
			d := p.Evaluate(script)
			assert.Equal(t, want.v, d.Verdict, "%q: verdict %v mustAsk %v (%s), want %v %v", script, d.Verdict, d.MustAsk, d.Reason, want.v, want.mustAsk)
			assert.Equal(t, want.mustAsk, d.MustAsk, "%q: verdict %v mustAsk %v (%s), want %v %v", script, d.Verdict, d.MustAsk, d.Reason, want.v, want.mustAsk)
		})
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
	require.NoError(t, err)
	defer reg.Close()
	_, err = reg.Workspace().ReadFile("secrets/db.txt")
	assert.ErrorIs(t, err, ErrBlockedPath, "read rule didn't block: %v", err)
	cfg.Permissions.Deny = []string{"nonsense(x)"}
	_, err = NewRegistry(cfg, nil, nil)
	assert.Error(t, err, "a bad rule in config was accepted")
}

func TestWebRules(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
	defer srv.Close()
	ctx := context.Background()
	// A deny rule beats allow_domains; an ask rule asks despite it.
	f := testFetcher(WebFetchConfig{AllowPrivate: true, AllowDomains: []string{"127.0.0.1"}, Rules: rulesOf(t, nil, nil, []string{"web(127.0.0.1)"})})
	out := f.fetch(ctx, allowAll(), srv.URL)
	assert.Contains(t, out.Error, "denied by the permission rule", "deny: %+v", out)
	f = testFetcher(WebFetchConfig{AllowPrivate: true, AllowDomains: []string{"127.0.0.1"}, Rules: rulesOf(t, nil, []string{"web(127.0.0.1)"}, nil)})
	h, reqs := approverHooks(true)
	out = f.fetch(ctx, h, srv.URL)
	assert.Equal(t, "ok", out.Content, "ask: %+v, prompts %+v", out, *reqs)
	assert.Len(t, *reqs, 1, "ask: %+v, prompts %+v", out, *reqs)
	assert.True(t, (*reqs)[0].MustAsk, "ask: %+v, prompts %+v", out, *reqs)
}

func TestMCPRulesBeatAutoApprove(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Tools.ApprovalsFile = ""
	cfg.Permissions.Deny = []string{"mcp(gh:delete_*)"}
	reg, err := NewRegistry(cfg, nil, nil)
	require.NoError(t, err)
	defer reg.Close()
	reg.mcp = newMCPManagerWithToolsets(map[string]tool.Toolset{"gh": inMemoryMCP(t, "delete_repo")}, map[string]bool{"gh": true}, nil)
	for _, ts := range reg.mcp.Toolsets() {
		ts.Tools(createTestToolContext())
	}
	err = reg.ApproveMCP(context.Background(), "delete_repo", nil)
	assert.Error(t, err, "auto_approve server ignored a deny rule")
	assert.Contains(t, err.Error(), "deny mcp(gh:delete_*)", "auto_approve server ignored a deny rule: %v", err)
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
		got := r.ToolDenied(c.tool, c.args) != nil
		assert.Equal(t, c.deny, got, "%s %v: denied %v, want %v", c.tool, c.args, got, c.deny)
	}
}

func TestRulesAddRemove(t *testing.T) {
	r := rulesOf(t, nil, nil, nil)
	require.NoError(t, r.Add(EffectAllow, "Bash(go test *)", "session"))
	r.Add(EffectAllow, "shell(go test *)", "session") // duplicate
	l := r.List()
	assert.Len(t, l, 1, "list %+v", l)
	assert.Equal(t, "shell(go test *)", l[0].String(), "list %+v", l)
	assert.Equal(t, "session", l[0].Source, "list %+v", l)
	n := r.Remove("Bash(go test *)", "")
	assert.Equal(t, 1, n, "remove: %d, left %+v", n, r.List())
	assert.Len(t, r.List(), 0, "remove: %d, left %+v", n, r.List())
}

// A background task runs what the workspace's rules and mode allow, and
// nobody is asked for the rest.
func TestBackgroundRunsAtTheGate(t *testing.T) {
	write := func(p string) api.ApprovalRequest {
		return api.ApprovalRequest{Tool: "create_file", Kind: api.ActionWrite, Detail: "Create " + p, Key: "write:/ws", Targets: []string{p}}
	}
	rules := rulesOf(t, []string{"write(docs/**)"}, []string{"write(ci/**)"}, []string{"write(**/*.pem)"})
	cases := []struct {
		name    string
		mode    api.PermissionMode
		path    string
		allowed bool
	}{
		{name: "an allow rule", mode: api.ModeDefault, path: "docs/a.md", allowed: true},
		{name: "accept-edits", mode: api.ModeAcceptEdits, path: "src/a.go", allowed: true},
		{name: "would ask", mode: api.ModeDefault, path: "src/a.go"},
		{name: "an ask rule", mode: api.ModeAcceptEdits, path: "ci/deploy.yml"},
		{name: "a deny rule", mode: api.ModeBypass, path: "k.pem"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, reqs := approverHooks(true)
			h.setMode(c.mode)
			h.SetRules(rules)
			err := h.Approve(Background(context.Background()), write(c.path))
			assert.Empty(t, *reqs, "the approver was asked")
			if c.allowed {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, ErrNotApproved)
			}
		})
	}
	assert.True(t, isUnattended(Background(context.Background())), "questions to the user must be refused too")
}
