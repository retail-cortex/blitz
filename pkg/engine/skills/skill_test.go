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

// hostSkill writes a skill named s with a script into a new directory and
// returns it as discovered, with the skill's directory.
func hostSkill(t *testing.T) (*Skill, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "s")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "scripts"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: s\n---\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scripts", "run.py"), []byte("print(1)"), 0o644))
	p, err := NewProvider()
	require.NoError(t, err)
	require.NoError(t, p.DiscoverExternal([]string{filepath.Dir(dir)}))
	s, ok := p.Get("s")
	require.True(t, ok)
	return s, dir
}

// TestParseSkillMDRejects checks each malformed SKILL.md is refused.
func TestParseSkillMDRejects(t *testing.T) {
	cases := map[string]struct{ doc, want string }{
		"no frontmatter":       {"just text", "starting"},
		"no closing delimiter": {"---\nname: x\n", "closing"},
		"bad yaml":             {"---\nname: [x\n---\n", "frontmatter"},
		"no name":              {"---\ndescription: x\n---\n", "name"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseSkillMD([]byte(tc.doc), "SKILL.md")
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

// TestMatches checks matching on name, description and tags, ignoring case.
func TestMatches(t *testing.T) {
	s := &Skill{SkillMetadata: SkillMetadata{Name: "deploy", Description: "Ship containers", Tags: []string{"Docker"}}}
	for q, want := range map[string]bool{"DEP": true, "contain": true, "docker": true, "python": false} {
		t.Run(q, func(t *testing.T) {
			assert.Equal(t, want, s.Matches(q))
		})
	}
}

// TestReadScript checks reading scripts from disk and from built-in skills,
// and refusing paths outside the skill.
func TestReadScript(t *testing.T) {
	s, _ := hostSkill(t)
	b, err := s.ReadScript("scripts/run.py")
	require.NoError(t, err)
	assert.Equal(t, "print(1)", string(b))
	_, err = s.ReadScript("../x.py")
	assert.Error(t, err, "outside the skill")

	p, err := NewProvider()
	require.NoError(t, err)
	builtin, ok := p.Get("code-review")
	require.True(t, ok)
	b, err = builtin.ReadScript("SKILL.md")
	require.NoError(t, err)
	assert.Contains(t, string(b), "name:")
	_, err = builtin.ReadScript("../other/SKILL.md")
	assert.Error(t, err, "outside the built-in skill")
	_, err = (&Skill{}).ReadScript("x")
	assert.Error(t, err, "a skill with no files")
}

// TestSkillFilesErrors checks hashing and copying a skill whose files are
// missing or unreadable.
func TestSkillFilesErrors(t *testing.T) {
	_, err := (&Skill{}).ContentHash()
	assert.Error(t, err, "no files to hash")
	assert.Error(t, (&Skill{}).CopyTo(t.TempDir()), "no files to copy")

	s, dir := hostSkill(t)
	require.NoError(t, os.Symlink("/etc/hosts", filepath.Join(dir, "link")))
	out := t.TempDir()
	require.NoError(t, s.CopyTo(out))
	assert.FileExists(t, filepath.Join(out, "scripts", "run.py"))
	assert.NoFileExists(t, filepath.Join(out, "link"), "symbolic links aren't copied")

	script := filepath.Join(dir, "scripts", "run.py")
	require.NoError(t, os.Chmod(script, 0o000))
	t.Cleanup(func() { _ = os.Chmod(script, 0o644) })
	if _, err := os.ReadFile(script); err == nil {
		t.Skip("running with privileges that ignore file modes")
	}
	_, err = s.ContentHash()
	assert.Error(t, err, "an unreadable file")
	assert.Error(t, s.CopyTo(t.TempDir()), "an unreadable file")

	require.NoError(t, os.RemoveAll(dir))
	_, err = s.ContentHash()
	assert.Error(t, err, "the directory is gone")
	assert.Error(t, s.CopyTo(t.TempDir()), "the directory is gone")
	_, err = s.ScriptPath("scripts/run.py")
	assert.Error(t, err, "the directory is gone")
}

// TestDefinitionDetails checks tier names, the unspecified language, the
// resource category short form, the legacy allowed_tools key and the
// remaining definition problems.
func TestDefinitionDetails(t *testing.T) {
	assert.Equal(t, "TIER_UNSPECIFIED", HITLTier(99).String())
	assert.Equal(t, "document_text", ResourceRequirement{Category: "RESOURCE_CATEGORY_DOCUMENT_TEXT"}.CategoryName())

	s, err := ParseSkillMD([]byte("---\nname: x\nallowed_tools: Read, Bash\nexecution_hints: {timeout_seconds: -5}\nscripts:\n  - language: unspecified\n    inline_code: \"x\"\n---\n"), "")
	require.NoError(t, err)
	assert.Equal(t, []string{"Read", "Bash"}, s.AllowedToolsList(), "the legacy key is read when allowed-tools is unset")
	require.Len(t, s.Scripts, 1)
	assert.Equal(t, LanguageUnspecified, s.Scripts[0].Language)
	assert.Contains(t, s.Problems, "execution_hints: timeout_seconds can't be negative")
	assert.Contains(t, s.Problems, "scripts[0]: needs a name")
}

// TestEvaluateBlocks checks the policy reasons not covered elsewhere: an
// unhashable skill with scripts, an untrusted project root, a denied tool
// without scopes, and trusted hashes when there is no hash.
func TestEvaluateBlocks(t *testing.T) {
	s := skillFrom(t, withScript("tool_requirements:\n  - name: Bash"))
	p := defaultPolicy()
	p.UntrustedRoots = []string{"/elsewhere", filepath.Dir(s.HostDir())}
	p.DenyTools = []string{"bash"}
	ev := Evaluate(s, p)
	all := strings.Join(ev.Blocked, "\n")
	assert.Contains(t, all, "settings aren't trusted")
	assert.Contains(t, all, "needs Bash, denied")

	require.NoError(t, os.RemoveAll(s.HostDir()))
	p = defaultPolicy()
	p.TrustedHashes = []string{"sha256:abc"}
	ev = Evaluate(s, p)
	all = strings.Join(ev.Blocked, "\n")
	assert.Contains(t, all, "content can't be hashed")
	assert.Contains(t, all, "its content (none)")

	assert.Empty(t, untrustedRoot(&Skill{}, []string{"/"}), "built-in skills have no root")
}
