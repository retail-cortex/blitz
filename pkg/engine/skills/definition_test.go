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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const castorSkill = `---
name: gh-issues
description: Triage GitHub issues
license: Apache-2.0
compatibility: Requires Python 3.11+
allowed-tools: Bash(gh:*) Read
metadata:
  owner: platform
authors:
  - name: Ada
    email: ada@example.com
category: devops
tags: [github, triage]
trigger_phrases: ["triage issues"]
tool_requirements:
  - name: Bash
    scopes: ["gh:*"]
    description: read issues
execution_hints:
  preferred_model: gemini-3.8-flash
  environment_variables: [GITHUB_TOKEN, HOME]
  timeout_seconds: 120
  custom_hints: {network: "true"}
  hitl_tier: HITL_POLICY_TIER_2_AUDITED_WRITE
compiled_reference:
  sha256_hash: abc123
scripts:
  - name: list
    language: SCRIPT_LANGUAGE_PYTHON
    relative_path: scripts/list.py
    entry_point: main
    dependencies: ["requests>=2.31.0", "rich==13.7.1"]
    timeout_seconds: 60
    environment_variables: {PAGE_SIZE: "50"}
skill_id: sk-1
uri: skm://skills/sk-1
source_uri: github://example/skills@main
---
Instructions here.
`

func TestParseCastorFrontmatter(t *testing.T) {
	s, err := ParseSkillMD([]byte(castorSkill), "x/SKILL.md")
	require.NoError(t, err)
	require.Len(t, s.Problems, 0, "problems: %v", s.Problems)
	assert.Equal(t, "Apache-2.0", s.License, "metadata: %+v", s.SkillMetadata)
	assert.NotEqual(t, "", s.Compatibility, "metadata: %+v", s.SkillMetadata)
	assert.Equal(t, "platform", s.Metadata["owner"], "metadata: %+v", s.SkillMetadata)
	assert.Equal(t, "devops", s.Category, "metadata: %+v", s.SkillMetadata)
	assert.Len(t, s.Authors, 1, "metadata: %+v", s.SkillMetadata)
	assert.Equal(t, "ada@example.com", s.Authors[0].Email, "metadata: %+v", s.SkillMetadata)
	assert.Equal(t, "sk-1", s.SkillID, "metadata: %+v", s.SkillMetadata)
	assert.NotEqual(t, "", s.SourceURI, "metadata: %+v", s.SkillMetadata)
	got := s.AllowedToolsList()
	assert.Equal(t, []string{"Bash(gh:*)", "Read"}, got, "allowed-tools: %q", got)
	h := s.ExecutionHints
	require.NotNil(t, h, "hints: %+v", h)
	require.Equal(t, Tier2AuditedWrite, h.HITLTier, "hints: %+v", h)
	require.True(t, h.NeedsNetwork(), "hints: %+v", h)
	require.Equal(t, 120, h.TimeoutSeconds, "hints: %+v", h)
	require.Len(t, h.EnvironmentVariables, 2, "hints: %+v", h)
	sc := s.Scripts[0]
	assert.Equal(t, LanguagePython, sc.Language, "script: %+v", sc)
	assert.Equal(t, "scripts/list.py", sc.RelativePath, "script: %+v", sc)
	assert.Len(t, sc.Dependencies, 2, "script: %+v", sc)
	assert.Equal(t, "50", sc.EnvironmentVariables["PAGE_SIZE"], "script: %+v", sc)
	assert.Equal(t, "abc123", s.CompiledReference.SHA256Hash, "reference/tools: %+v %+v", s.CompiledReference, s.ToolRequirements)
	assert.Equal(t, "gh:*", s.ToolRequirements[0].Scopes[0], "reference/tools: %+v %+v", s.CompiledReference, s.ToolRequirements)
	assert.Equal(t, "Instructions here.", s.Content, "content %q", s.Content)
}

func TestHITLTierNames(t *testing.T) {
	for in, want := range map[string]HITLTier{
		"HITL_POLICY_TIER_0_BYPASS_ALL": Tier0BypassAll,
		"TIER_1_AUTO_READ":              Tier1AutoRead,
		"tier_2":                        Tier2AuditedWrite,
		"TIER_3_MANDATORY_APPROVAL":     Tier3MandatoryApproval,
		"":                              TierUnspecified,
	} {
		got, err := ParseHITLTier(in)
		assert.NoError(t, err, "%q: %v", in, got)
		assert.Equal(t, want, got, "%q: %v %v", in, got, err)
	}
	_, err := ParseHITLTier("TIER_9")
	assert.Error(t, err, "unknown tier accepted")
	// A number is ambiguous (the proto numbers tier 2 as 3), so it's refused.
	_, err = ParseSkillMD([]byte("---\nname: x\nexecution_hints:\n  hitl_tier: 2\n---\n"), "")
	require.Error(t, err, "numeric tier")
	require.Contains(t, err.Error(), "not a number", "numeric tier: %v", err)
	// Omitted means unspecified, never a bypass.
	s, _ := ParseSkillMD([]byte("---\nname: x\n---\n"), "")
	require.Equal(t, TierUnspecified, s.DeclaredTier(), "omitted tier = %v", s.DeclaredTier())
}

func TestValidateScripts(t *testing.T) {
	doc := `---
name: bad
scripts:
  - name: a
    language: python
    relative_path: ../escape.py
  - name: a
    language: python
    inline_code: "print(1)"
    relative_path: x.py
  - name: "no spaces"
    language: python
    storage_uri: gs://bucket/x.py
  - name: c
    inline_code: "x"
    timeout_seconds: -1
    environment_variables: {"BAD-NAME": "1"}
execution_hints:
  environment_variables: ["1BAD"]
tool_requirements:
  - scopes: ["x"]
---
`
	s, err := ParseSkillMD([]byte(doc), "")
	require.NoError(t, err)
	all := strings.Join(s.Problems, "\n")
	for _, want := range []string{
		`script "a": relative_path must stay inside`,
		`script "a": name is used twice`,
		`needs exactly one of inline_code, storage_uri or relative_path`,
		`may only use letters`,
		`storage_uri isn't supported yet`,
		`script "c": needs a language`,
		`timeout_seconds can't be negative`,
		`invalid environment variable name "BAD-NAME"`,
		`execution_hints: invalid environment variable name "1BAD"`,
		`tool_requirements[0]: needs a name`,
	} {
		assert.Contains(t, all, want, "missing problem %q in:\n%s", want, all)
	}
	_, err = ParseSkillMD([]byte("---\nname: x\nscripts:\n  - name: s\n    language: cobol\n---\n"), "")
	assert.Error(t, err, "unknown language accepted")
}

func TestInBundle(t *testing.T) {
	for p, want := range map[string]bool{
		"scripts/a.py": true, "a.py": true, "./a.py": true, "x/../a.py": true,
		"../a.py": false, "/etc/passwd": false, "x/../../a.py": false, `scripts\a.py`: false, ".": false,
	} {
		assert.Equal(t, want, inBundle(p), "inBundle(%q) = %v", p, !want)
	}
}

func TestContentHashCoversEveryFileAndSkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "scripts"), 0o755)
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: h\n---\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "scripts", "a.py"), []byte("print(1)"), 0o644)
	p, _ := NewProvider()
	require.NoError(t, p.DiscoverExternal([]string{dir}))
	s, _ := p.Get("h")
	h1, err := s.ContentHash()
	require.NoError(t, err, "%q", h1)
	require.True(t, strings.HasPrefix(h1, "sha256:"), "%q %v", h1, err)
	h, _ := s.ContentHash()
	require.Equal(t, h1, h, "hash isn't stable")
	os.Symlink("/etc/hosts", filepath.Join(dir, "scripts", "link"))
	h, _ = s.ContentHash()
	require.Equal(t, h1, h, "a symbolic link changed the hash")
	os.WriteFile(filepath.Join(dir, "scripts", "a.py"), []byte("print(2)"), 0o644)
	h, _ = s.ContentHash()
	require.NotEqual(t, h1, h, "changing a script didn't change the hash")
	// Built-in skills hash too.
	b, _ := p.Get("code-review")
	h, err = b.ContentHash()
	require.NoError(t, err, "builtin: %q", h)
	require.NotEqual(t, "", h, "builtin: %q %v", h, err)
}

func TestDiscoveryReportsUnparseableSkills(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "broken"), 0o755)
	os.WriteFile(filepath.Join(dir, "broken", "SKILL.md"), []byte("---\nname: broken\nexecution_hints:\n  hitl_tier: TIER_7\n---\n"), 0o644)
	p, _ := NewProvider()
	err := p.DiscoverExternal([]string{dir})
	require.Error(t, err, "error")
	require.Contains(t, err.Error(), "broken/SKILL.md", "error: %v", err)
	require.Contains(t, err.Error(), "TIER_7", "error: %v", err)
	_, ok := p.Get("broken")
	require.False(t, ok, "an unparseable skill was loaded")
}

func TestScriptPathStaysInsideTheSkill(t *testing.T) {
	dir := t.TempDir()
	skill := filepath.Join(dir, "s")
	os.MkdirAll(filepath.Join(skill, "scripts"), 0o755)
	os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("---\nname: s\n---\n"), 0o644)
	os.WriteFile(filepath.Join(skill, "scripts", "ok.py"), []byte("print(1)"), 0o644)
	os.WriteFile(filepath.Join(dir, "outside.py"), []byte("print('escaped')"), 0o644)
	os.Symlink(filepath.Join(dir, "outside.py"), filepath.Join(skill, "scripts", "link.py"))
	p, _ := NewProvider()
	require.NoError(t, p.DiscoverExternal([]string{dir}))
	s, _ := p.Get("s")
	got, err := s.ScriptPath("scripts/ok.py")
	require.NoError(t, err, "ok: %q", got)
	require.Equal(t, filepath.Join(skill, "scripts", "ok.py"), got, "ok: %q %v", got, err)
	for _, bad := range []string{"scripts/link.py", "../outside.py", "scripts", "missing.py"} {
		got, err := s.ScriptPath(bad)
		assert.Error(t, err, "%s accepted: %s", bad, got)
	}
	b, _ := p.Get("code-review")
	_, err = b.ScriptPath("SKILL.md")
	assert.Error(t, err, "built-in skill gave a host path")
	out := t.TempDir()
	require.NoError(t, b.CopyTo(out))
	_, err = os.Stat(filepath.Join(out, "SKILL.md"))
	require.NoError(t, err, "CopyTo")
}
