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

package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func defaultPolicy() config.SkillPolicy { return config.DefaultConfig().Skills.Policy }

// skillFrom loads a skill from a SKILL.md in its own directory, so it has
// files to hash.
func skillFrom(t *testing.T, doc string) *Skill {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(doc), 0o644))
	p, _ := NewProvider()
	require.NoError(t, p.DiscoverExternal([]string{dir}))
	for _, s := range p.List() {
		if s.Path == filepath.Join(dir, "SKILL.md") {
			return s
		}
	}
	t.Fatal("skill not loaded")
	return nil
}

func withScript(extra string) string {
	return "---\nname: s\n" + extra + "\nscripts:\n  - name: run\n    language: python\n    inline_code: \"print(1)\"\n---\n"
}

func TestTierIsTheStricterOfSkillAndPolicy(t *testing.T) {
	cases := []struct {
		hints       string
		minTier     int
		allowBypass bool
		want        HITLTier
		wantBypass  bool
	}{
		{"", 2, false, Tier2AuditedWrite, false}, // unspecified -> the policy's minimum
		{"execution_hints: {hitl_tier: TIER_1_AUTO_READ}", 2, false, Tier2AuditedWrite, false},
		{"execution_hints: {hitl_tier: TIER_3_MANDATORY_APPROVAL}", 2, false, Tier3MandatoryApproval, false},
		{"execution_hints: {hitl_tier: TIER_1_AUTO_READ}", 0, false, Tier1AutoRead, false}, // min 0 never means bypass
		{"execution_hints: {requires_human_approval: true, hitl_tier: TIER_1_AUTO_READ}", 1, false, Tier3MandatoryApproval, false},
		// Tier 0 needs both the skill's and the policy's consent.
		{"execution_hints: {hitl_tier: TIER_0_BYPASS_ALL}", 2, true, Tier3MandatoryApproval, false},
		{"execution_hints: {hitl_tier: TIER_0_BYPASS_ALL, allow_hitl_bypass: true}", 2, false, Tier3MandatoryApproval, false},
		{"execution_hints: {hitl_tier: TIER_0_BYPASS_ALL, allow_hitl_bypass: true}", 2, true, Tier0BypassAll, true},
		// The compiled reference's tier applies when the hints give none.
		{"compiled_reference: {hitl_tier: TIER_3_MANDATORY_APPROVAL}", 1, false, Tier3MandatoryApproval, false},
	}
	for _, c := range cases {
		p := defaultPolicy()
		p.MinHITLTier, p.AllowHITLBypass = c.minTier, c.allowBypass
		ev := Evaluate(skillFrom(t, withScript(c.hints)), p)
		assert.Equal(t, c.want, ev.Tier, "%q (min %d, bypass %v): got %v/%v, want %v/%v", c.hints, c.minTier, c.allowBypass, ev.Tier, ev.Bypass, c.want, c.wantBypass)
		assert.Equal(t, c.wantBypass, ev.Bypass, "%q (min %d, bypass %v): got %v/%v, want %v/%v", c.hints, c.minTier, c.allowBypass, ev.Tier, ev.Bypass, c.want, c.wantBypass)
	}
}

func TestNetworkEnvAndTimeout(t *testing.T) {
	s := skillFrom(t, withScript("execution_hints:\n  custom_hints: {network: required}\n  environment_variables: [GITHUB_TOKEN, AWS_SECRET_ACCESS_KEY]\n  timeout_seconds: 900"))
	p := defaultPolicy()
	ev := Evaluate(s, p)
	require.False(t, ev.Network, "network denied by default: %+v", ev)
	require.False(t, ev.Runnable(), "network denied by default: %+v", ev)
	require.Contains(t, strings.Join(ev.Blocked, " "), "needs the network", "network denied by default: %+v", ev)
	p.Network, p.NetworkAllow = "allowlist", []string{"s"}
	p.EnvPassthrough = []string{"GITHUB_*"}
	ev = Evaluate(s, p)
	require.True(t, ev.Network, "allowlisted: %+v", ev)
	require.True(t, ev.Runnable(), "allowlisted: %+v", ev)
	require.Equal(t, []string{"GITHUB_TOKEN"}, ev.Env, "env %v withheld %v", ev.Env, ev.Withheld)
	require.Equal(t, []string{"AWS_SECRET_ACCESS_KEY"}, ev.Withheld, "env %v withheld %v", ev.Env, ev.Withheld)
	require.Equal(t, 300, ev.Scripts[0].TimeoutSeconds, "timeout %d, want the policy's 300", ev.Scripts[0].TimeoutSeconds)
	p.MaxTimeoutSeconds = 0
	got := Evaluate(s, p).Scripts[0].TimeoutSeconds
	require.Equal(t, 900, got, "uncapped timeout %d", got)
}

func TestLanguagesToolsAndHashes(t *testing.T) {
	ts := skillFrom(t, "---\nname: t\nscripts:\n  - name: run\n    language: typescript\n    inline_code: x\n---\n")
	v := Evaluate(ts, defaultPolicy()).Scripts[0]
	require.False(t, v.Allowed, "typescript: %+v", v)
	require.Contains(t, v.Reasons[0], `language "typescript"`, "typescript: %+v", v)

	tools := skillFrom(t, withScript("tool_requirements:\n  - name: Bash\n    scopes: [\"sudo:*\", \"git:*\"]"))
	p := defaultPolicy()
	p.DenyTools = []string{"bash:sudo*"}
	ev := Evaluate(tools, p)
	require.False(t, ev.Runnable(), "deny_tools: %+v", ev.Blocked)
	require.Contains(t, strings.Join(ev.Blocked, " "), "Bash:sudo:*", "deny_tools: %+v", ev.Blocked)

	s := skillFrom(t, withScript(""))
	p = defaultPolicy()
	p.TrustedHashes = []string{"sha256:other"}
	ev = Evaluate(s, p)
	require.False(t, ev.Runnable(), "untrusted: %+v", ev.Blocked)
	require.Contains(t, ev.Blocked[0], "trusted_hashes", "untrusted: %+v", ev.Blocked)
	p.TrustedHashes = []string{Evaluate(s, defaultPolicy()).Hash}
	require.True(t, Evaluate(s, p).Runnable(), "trusted hash refused")

	bad := skillFrom(t, "---\nname: b\nscripts:\n  - name: run\n    language: python\n    relative_path: ../x.py\n---\n")
	ev = Evaluate(bad, defaultPolicy())
	require.False(t, ev.Runnable(), "definition problems: %+v", ev.Blocked)
	require.Contains(t, ev.Blocked[0], "definition:", "definition problems: %+v", ev.Blocked)
}

func TestDependencies(t *testing.T) {
	p := defaultPolicy().Packages
	for dep, want := range map[string]string{
		"requests":                         "",
		"requests>=2.31.0":                 "",
		"Rich[jupyter] ==13.7.1":           "",
		"numpy>=1.26,<3":                   "",
		"pywin32; sys_platform == 'win32'": "",
		"git+https://example.com/x.git":    "isn't a plain package requirement",
		"-e .":                             "isn't a plain package requirement",
		"--index-url=https://evil.example": "isn't a plain package requirement",
		"./local.whl":                      "isn't a plain package requirement",
		"pkg @ https://example.com/p.whl":  "isn't a plain package requirement",
	} {
		t.Run(dep, func(t *testing.T) {
			got := checkDependency(dep, p)
			assert.Equal(t, (got == ""), (want == ""), "%q: %q", dep, got)
			assert.Contains(t, got, want, "%q: %q", dep, got)
		})
	}
	p.Deny = []string{"*-Nightly", "evil_pkg"}
	r := checkDependency("torch-nightly==2.0", p)
	assert.Contains(t, r, "denied", "deny glob: %q", r)
	r = checkDependency("Evil.Pkg", p)
	assert.Contains(t, r, "denied", "deny by normalized name: %q", r)
	p.Allow = []string{"requests", "rich"}
	r = checkDependency("numpy", p)
	assert.Contains(t, r, "isn't in skills.policy.packages.allow", "allow list: %q", r)
	p.RequireHashes = true
	for dep, ok := range map[string]bool{"requests==2.32.3": true, "requests>=2": false, "requests==2.*": false, "rich": false} {
		t.Run(dep, func(t *testing.T) {
			assert.Equal(t, ok, (checkDependency(dep, p) == ""), "require_hashes %q: %q", dep, checkDependency(dep, p))
		})
	}
}
