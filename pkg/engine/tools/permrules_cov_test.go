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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
)

// TestSettingsValidatePermissionRules checks the config package's validator
// hook is this package's parser: it canonicalizes good rules and refuses bad.
func TestSettingsValidatePermissionRules(t *testing.T) {
	require.NotNil(t, config.ValidatePermissionRule)
	got, err := config.ValidatePermissionRule("allow", "Bash(go test)")
	require.NoError(t, err)
	assert.Equal(t, "shell(go test)", got)
	_, err = config.ValidatePermissionRule("allow", "nope(x)")
	assert.ErrorContains(t, err, "unknown kind")
}

// TestNameRuleKinds checks the name kinds (search, its alias) and the ? glob.
func TestNameRuleKinds(t *testing.T) {
	for _, tc := range []struct {
		rule, kind, sample string
		matches            bool
	}{
		{"search(go?)", RuleSearch, "gol", true},
		{"websearch(go?)", RuleSearch, "golang", false},
		{"mcp(github:*)", RuleMCP, "github:issues", true},
	} {
		t.Run(tc.rule, func(t *testing.T) {
			c, err := CheckPermissionRule(EffectDeny, tc.rule, tc.sample)
			require.NoError(t, err)
			assert.Equal(t, tc.kind, c.Kind)
			assert.Equal(t, tc.matches, c.Matches)
		})
	}
}

// TestCheckPermissionRuleUnreadableSample checks a sample the shell can't
// parse is reported, not matched.
func TestCheckPermissionRuleUnreadableSample(t *testing.T) {
	_, err := CheckPermissionRule(EffectAllow, "shell(echo)", "echo 'open")
	assert.ErrorContains(t, err, "isn't a command")
}

// TestReplaceProject checks the project files' rules are swapped as a set,
// leaving the others, and that a bad rule is reported.
func TestReplaceProject(t *testing.T) {
	r := rulesOf(t, []string{"shell(ls)"}, nil, nil)
	require.NoError(t, r.ReplaceProject(config.PermissionsConfig{Deny: []string{"write(*.key)"}, Ask: []string{"shell(rm)"}}))
	assert.Len(t, r.List(), 3)
	require.NoError(t, r.ReplaceProject(config.PermissionsConfig{Deny: []string{"write(*.pem)"}}))
	l := r.List()
	require.Len(t, l, 2)
	assert.Equal(t, "write(*.pem)", l[0].String(), "deny first")
	assert.Equal(t, SourceProject, l[0].Source)
	assert.Error(t, r.ReplaceProject(config.PermissionsConfig{Deny: []string{"bogus(x)"}}))
}

// TestRemoveByEffect checks Remove can be limited to one effect and leaves
// other rules.
func TestRemoveByEffect(t *testing.T) {
	r := rulesOf(t, []string{"shell(git)"}, nil, []string{"shell(git)", "shell(rm)"})
	assert.Equal(t, 1, r.Remove("shell(git)", EffectAllow))
	l := r.List()
	require.Len(t, l, 2)
	assert.Equal(t, EffectDeny, l[0].Effect)
	assert.Equal(t, 0, r.Remove("not a rule(", ""), "an unparsable rule removes nothing")

	var nilRules *PermissionRules
	assert.Nil(t, nilRules.List())
	assert.Nil(t, nilRules.ToolDenied("web_search", nil))
}

// TestToolDeniedWithoutAName checks a skill or agent call that names nothing
// isn't denied by name rules.
func TestToolDeniedWithoutAName(t *testing.T) {
	r := rulesOf(t, nil, nil, []string{"skill(*)"})
	assert.Nil(t, r.ToolDenied("activate_skill", map[string]any{"skill_name": ""}))
	assert.NotNil(t, r.ToolDenied("activate_skill", map[string]any{"name": "x"}))
}

// TestRuleKind checks which rule kind governs each approval request.
func TestRuleKind(t *testing.T) {
	for _, tc := range []struct {
		req  api.ApprovalRequest
		want string
	}{
		{api.ApprovalRequest{Kind: api.ActionWrite}, RuleWrite},
		{api.ApprovalRequest{Kind: api.ActionDelete}, RuleDelete},
		{api.ApprovalRequest{Kind: api.ActionMCP}, RuleMCP},
		{api.ApprovalRequest{Kind: api.ActionNetwork, Tool: "web_search"}, RuleSearch},
		{api.ApprovalRequest{Kind: api.ActionNetwork, Tool: "web_fetch"}, RuleWeb},
		{api.ApprovalRequest{Kind: api.ActionCommand}, RuleShell},
		{api.ApprovalRequest{Kind: "other"}, ""},
	} {
		t.Run(string(tc.req.Kind)+tc.req.Tool, func(t *testing.T) {
			assert.Equal(t, tc.want, ruleKind(tc.req))
		})
	}
}
