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
)

// TestNewCommandPolicyInvalidPatterns checks an invalid pattern in any list
// fails the policy.
func TestNewCommandPolicyInvalidPatterns(t *testing.T) {
	bad := []string{"re:("}
	for name, cfg := range map[string]CommandPolicyConfig{
		"allow": {Allow: bad},
		"deny":  {Deny: bad},
		"auto":  {AutoApprove: bad},
		"empty": {Deny: []string{"re: "}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewCommandPolicy(cfg)
			assert.ErrorContains(t, err, "invalid command pattern")
		})
	}
}

// TestCommandPolicyDescribe checks the summary shown for a policy.
func TestCommandPolicyDescribe(t *testing.T) {
	var nilPolicy *CommandPolicy
	assert.Nil(t, nilPolicy.Describe())
	assert.Equal(t, []string{"allow: any command not denied (with approval)"}, mustPolicy(t, CommandPolicyConfig{}).Describe())
	p := mustPolicy(t, CommandPolicyConfig{Allow: []string{"git *", "ls"}, Deny: []string{"rm"}, AutoApprove: []string{"ls"}})
	assert.Equal(t, []string{"allow only: git *, ls", "deny: rm", "auto-approve: ls"}, p.Describe())
}

// TestCommandPolicyShellForms checks the less common shell forms: question
// mark globs, nested shells, wrapper options, escapes and parse failures.
func TestCommandPolicyShellForms(t *testing.T) {
	p := mustPolicy(t, CommandPolicyConfig{Deny: []string{"rm", "g?t push"}, AutoApprove: []string{"ls", "echo"}})
	for _, tc := range []struct {
		script  string
		verdict Verdict
		reason  string
	}{
		{"git push", VerdictDeny, "deny rule"},
		{"env -- rm x", VerdictDeny, "deny rule"},
		{"env -i rm x", VerdictDeny, "deny rule"},
		{"bash -c", VerdictNeedsApproval, ""},
		{"bash script.sh", VerdictNeedsApproval, ""},
		{`bash -c "echo 'open"`, VerdictNeedsApproval, "could not parse"},
		{`env bash -c "echo 'open"`, VerdictNeedsApproval, "could not parse"},
		{`bash -c "bash -c \"bash -c 'bash -c \\\"bash -c rm\\\"'\""`, VerdictNeedsApproval, "too deep"},
		{"echo $\"hi\"", VerdictNeedsApproval, "runtime expansion"},
		{"echo \"a\\$b\\\\c\\q\"", VerdictAutoApprove, ""},
		{"echo \"a\\\nb\"", VerdictAutoApprove, ""},
		{"ec\\\nho hi", VerdictAutoApprove, ""},
	} {
		t.Run(tc.script, func(t *testing.T) {
			d := p.Evaluate(tc.script)
			assert.Equal(t, tc.verdict, d.Verdict, "reason %q, commands %q", d.Reason, d.Commands)
			if tc.reason != "" {
				assert.Contains(t, d.Reason, tc.reason)
			}
		})
	}
}

// TestCommandPolicyUnparsableWithAskRules checks a script that can't be
// parsed must ask when ask rules exist, since none can be ruled out.
func TestCommandPolicyUnparsableWithAskRules(t *testing.T) {
	p := mustPolicy(t, CommandPolicyConfig{})
	rules := rulesOf(t, nil, []string{"shell(git push)"}, nil)
	p.SetRules(rules)
	d := p.Evaluate("echo 'open")
	assert.Equal(t, VerdictNeedsApproval, d.Verdict)
	assert.True(t, d.MustAsk)
	assert.Contains(t, d.Reason, "could not parse")
}

// TestUnescapeDoubleQuoted checks backslashes inside double quotes keep only
// the escapes the shell honours.
func TestUnescapeDoubleQuoted(t *testing.T) {
	for in, want := range map[string]string{
		`plain`:     "plain",
		`a\$b`:      "a$b",
		`a\qb`:      `a\qb`,
		"a\\\nb":    "ab",
		`a\\b`:      `a\b`,
		`trailing\`: `trailing\`,
	} {
		t.Run(in, func(t *testing.T) {
			require.Equal(t, want, unescapeDoubleQuoted(in))
		})
	}
}
