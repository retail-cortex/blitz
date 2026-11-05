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

package catalog

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOf(t *testing.T) {
	cases := map[string]struct {
		kind, name string
		ok         bool
	}{
		"agents/coder.md":                {Agent, "coder", true},
		"rules/no-prod.md":               {Rule, "no-prod", true},
		"skills/go/SKILL.md":             {Skill, "go", true},
		"skills/go/scripts/lint.sh":      {SkillFile, "go", true},
		"guardrails/secrets.md":          {Guardrail, "secrets", true},
		"commands/review.md":             {Command, "review", true},
		"mcp/github.toml":                {MCP, "github", true},
		"connectors/kinds/bigquery.yaml": {Connector, "bigquery", true},
		"connectors/warehouse/sales.yml": {Connector, "sales", true},
		"README.md":                      {"", "", false},
		"policy.toml":                    {"", "", false},
		"agents/nested/coder.md":         {"", "", false},
		"agents/coder.txt":               {"", "", false},
		"skills/go":                      {"", "", false},
	}
	for p, c := range cases {
		kind, name, ok := Of(p)
		assert.Equal(t, []any{c.kind, c.name, c.ok}, []any{kind, name, ok}, p)
	}
}

func TestCheck(t *testing.T) {
	good := map[string]string{
		"agents/coder.md":          "---\nname: coder\ndescription: Writes code\nlock: locked\ngroups: [eng]\n---\nYou write Go.",
		"rules/no-prod.md":         "---\nwhen: before_agent\naction: block\n---\nNever touch production.",
		"guardrails/files.md":      "---\nstage: action\naction: block\nlock: default\n---\nNo files outside the repo.",
		"commands/review.md":       "---\ndescription: Review the diff\n---\nReview $ARGUMENTS.",
		"skills/go/SKILL.md":       "---\nname: go\ndescription: Go conventions\n---\nUse gofmt.",
		"skills/go/notes.txt":      "anything",
		"mcp/github.toml":          "url = \"https://api.githubcopilot.com/mcp/\"\nlock = \"locked\"\n[headers]\nAuthorization = \"{secrets:github_token}\"\n",
		"mcp/local.toml":           "command = \"npx\"\nargs = [\"-y\", \"server\"]\n",
		"connectors/kinds/bq.yaml": "name: bq\nlock: locked\n",
	}
	for p, data := range good {
		it, problems := Check(p, []byte(data))
		assert.Empty(t, problems, p)
		assert.Equal(t, p, it.Path)
	}
	it, _ := Check("agents/coder.md", []byte(good["agents/coder.md"]))
	assert.Equal(t, Item{Kind: Agent, Name: "coder", Path: "agents/coder.md", Lock: Locked, Groups: []string{"eng"}}, it)
	it, _ = Check("mcp/github.toml", []byte(good["mcp/github.toml"]))
	assert.Equal(t, Locked, it.Lock)
	it, _ = Check("commands/review.md", []byte(good["commands/review.md"]))
	assert.Equal(t, Default, it.Lock, "a default unless it says")

	bad := map[string]struct{ data, says string }{
		"agents/broken.md":     {"no front matter", "frontmatter"},
		"agents/hot.md":        {"---\nname: hot\ntemperature: 9\n---\nx", "temperature"},
		"rules/odd.md":         {"---\nwhen: sometimes\n---\nx", "before_agent or after_agent"},
		"guardrails/odd.md":    {"---\nstage: nowhere\n---\nx", "nowhere"},
		"guardrails/limits.md": {"---\nstage: action\nlock: locked\n---\nDropping a table.", "add builtin: limits"},
		"commands/open.md":     {"---\ndescription: x\nno end", "frontmatter"},
		"skills/go/SKILL.md":   {"---\nname: rust\ndescription: d\n---\nx", "isn't its folder's"},
		"skills/py/SKILL.md":   {"---\ndescription: d\n---\nx", "must specify a name"},
		"mcp/both.toml":        {"command = \"x\"\nurl = \"https://x\"\n", "not both"},
		"mcp/none.toml":        {"args = []\n", "a command"},
		"mcp/typo.toml":        {"comand = \"x\"\n", "unknown setting"},
		"mcp/leak.toml":        {"command = \"x\"\n[env]\nTOKEN = \"ghp_" + strings.Repeat("a", 36) + "\"\n", "holds a secret"},
		"mcp/half.toml":        {"command = \"x\"\n[env]\nTOKEN = \"Bearer {secrets:t}\"\n", "whole value"},
		"mcp/notoml.toml":      {"= x", "toml"},
		// N26 safety-enterprise#15: a secret anywhere in what every
		// person gets.
		"mcp/arg.toml":            {"command = \"x\"\nargs = [\"--api-key\", \"opaque-value-123\"]\n", "args[1], after --api-key, holds a secret"},
		"mcp/argeq.toml":          {"command = \"x\"\nargs = [\"--token=glpat-" + strings.Repeat("a1", 12) + "\"]\n", "holds a secret"},
		"mcp/userinfo.toml":       {"url = \"https://ana:hunter22@mcp.example.com/\"\n", "url holds a password"},
		"mcp/query.toml":          {"url = \"https://mcp.example.com/?token=abc123\"\n", "url's token holds a secret"},
		"connectors/db/prod.yaml": {"kind: source\nname: prod\npassword: Zq8" + "rT4vL2mN7pX1wK9s\n", "line 3 holds what looks like a secret"},
		"agents/leaky.md":         {"---\nname: leaky\n---\nUse AKIA" + "Q3EXAMPLEZ7Y4WXN for S3.", "line 4 holds what looks like a secret (AWS access key"},
		"connectors/x/bad.yaml":   {"a: [", "connectors/x/bad.yaml"},
		"agents/locky.md":         {"---\nname: locky\nlock: maybe\n---\nx", "locked or default"},
		"agents/bad name!.md":     {"---\nname: x\n---\nx", "the name"},
		"agents/badfm.md":         {"---\nname: [\n---\nx", "yaml"},
		"policy.toml":             {"schema = 1", "not a catalog item"},
	}
	for _, ok := range []struct{ path, data string }{
		{"mcp/ref.toml", "command = \"x\"\nargs = [\"--api-key\", \"{secrets:key}\"]\n"},
		{"mcp/qref.toml", "url = \"https://mcp.example.com/?token={secrets:t}\"\n"},
		{"connectors/db/ok.yaml", "kind: source\nname: ok\npassword: \"{secrets:db_password_2}\"\n"},
	} {
		_, problems := Check(ok.path, []byte(ok.data))
		assert.Empty(t, problems, ok.path)
	}
	for p, c := range bad {
		_, problems := Check(p, []byte(c.data))
		if assert.NotEmpty(t, problems, p) {
			assert.Contains(t, strings.ToLower(strings.Join(problems, "\n")), strings.ToLower(c.says), p)
		}
	}
}

func TestFor(t *testing.T) {
	assert.True(t, Item{}.For(nil), "everyone")
	assert.True(t, Item{Groups: []string{"eng"}}.For([]string{"ops", "eng"}))
	assert.False(t, Item{Groups: []string{"eng"}}.For([]string{"ops"}))
}
