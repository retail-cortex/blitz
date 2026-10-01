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

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Without a rule parser, rules are taken as written (trimmed).
func TestValidRuleUnset(t *testing.T) {
	old := ValidatePermissionRule
	ValidatePermissionRule = nil
	t.Cleanup(func() { ValidatePermissionRule = old })
	got, err := validRule("allow", "  anything  ")
	require.NoError(t, err)
	assert.Equal(t, "anything", got)
}

// Rule edits refuse bad rules and effects, keep a rule that's there
// already, and report a settings file they can't read.
func TestPermissionRuleErrors(t *testing.T) {
	keysEnv(t)
	fakeRuleCheck(t)
	_, _, err := AddPermissionRule("", "", "allow", "bad(x)")
	assert.ErrorContains(t, err, "invalid permission rule")
	_, _, err = AddPermissionRule("", "", "maybe", "shell(x)")
	assert.ErrorContains(t, err, "unknown permission effect")
	_, err = SavePermissionRules(ConfigDir(""), "maybe", nil)
	assert.ErrorContains(t, err, "unknown permission effect")

	_, _, err = AddPermissionRule("", "", "allow", "shell(make)")
	require.NoError(t, err)
	path, canonical, err := AddPermissionRule("", "", "allow", "shell(make)")
	require.NoError(t, err)
	assert.Equal(t, "shell(make)", canonical)
	p, _, err := ScopePermissions("", "")
	require.NoError(t, err)
	assert.Equal(t, []string{"shell(make)"}, p.Allow, "kept once")

	require.NoError(t, os.WriteFile(path, []byte("[[permissions"), 0o600))
	_, _, err = ScopePermissions("", "")
	assert.Error(t, err)
	_, _, err = AddPermissionRule("", "", "allow", "shell(x)")
	assert.Error(t, err)
	_, _, err = RemovePermissionRule("", "", "shell(x)")
	assert.Error(t, err)

	t.Setenv("HOME", "")
	_, _, err = ScopePermissions("", "")
	assert.ErrorContains(t, err, "no settings directory")
}

// editConfigFile refuses no directory, a file it can't read, and a directory it can't make or write.
func TestEditConfigFileErrors(t *testing.T) {
	keep := func(doc string) string { return doc }
	ok := func(map[string]any) error { return nil }
	_, err := editConfigFile("", keep, ok)
	assert.ErrorContains(t, err, "no configuration directory")

	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".env.toml"), 0o700))
	_, err = editConfigFile(dir, keep, ok)
	assert.Error(t, err, "the settings file is a directory")

	if os.Geteuid() != 0 { // root writes anywhere
		ro := filepath.Join(t.TempDir(), "ro")
		require.NoError(t, os.Mkdir(ro, 0o500))
		_, err = editConfigFile(filepath.Join(ro, "sub"), keep, ok)
		assert.Error(t, err, "a directory it can't make")
		_, err = editConfigFile(ro, keep, ok)
		assert.Error(t, err, "a directory it can't write")
	}
}

// A trailing comment is kept when a value is replaced, even after a
// string with an escaped quote.
func TestSetTOMLKeyKeepsComment(t *testing.T) {
	doc := "[ui]\nlocale = \"a\\\"b\" # mine\n"
	assert.Equal(t, "[ui]\nlocale = \"es\"   # mine\n", setTOMLKey(doc, "ui", "locale", `"es"`))
}

// Forgetting an unknown workspace is fine; a decision that can't be saved
// is reported.
func TestTrustStoreErrors(t *testing.T) {
	dir := t.TempDir()
	s := OpenTrustStore(dir)
	assert.NoError(t, s.Forget("/nowhere"))

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "trust.json", "x"), 0o700))
	assert.ErrorContains(t, s.Set("/w", "h", true), "saving the trust decision")

	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	assert.Error(t, OpenTrustStore(filepath.Join(file, "sub")).Set("/w", "h", true), "a directory it can't make")
	if os.Geteuid() != 0 {
		ro := filepath.Join(t.TempDir(), "ro")
		require.NoError(t, os.Mkdir(ro, 0o500))
		assert.Error(t, OpenTrustStore(ro).Set("/w", "h", true), "a directory it can't write")
	}
}

// Small accessors: memory notes default on, hooks describe what they run,
// and the language servers follow the settings.
func TestFeatureAccessors(t *testing.T) {
	off := false
	assert.True(t, MemoryConfig{}.AutoOn())
	assert.False(t, MemoryConfig{Auto: &off}.AutoOn())

	assert.Equal(t, HookCommand, HookConfig{}.Kind())
	assert.Equal(t, "POST https://x", HookConfig{Type: HookHTTP, URL: "https://x"}.Describe())
	assert.Equal(t, "prompt: check", HookConfig{Type: HookPrompt, Prompt: "check"}.Describe())
	assert.Equal(t, "a b", HookConfig{Command: "ignored", Args: []string{"a", "b"}}.Describe())
	assert.Equal(t, "run.sh", HookConfig{Command: "run.sh"}.Describe())
	assert.Len(t, HooksConfig{PreTool: []HookConfig{{}}, Stop: []HookConfig{{}, {}}}.All(), 3)

	cfg := DefaultConfig()
	cfg.LSP = map[string]LSPServerConfig{
		"go":     {Command: []string{"my-gopls"}},
		"python": {Disabled: true},
		"zig":    {Command: []string{"zls"}, Extensions: []string{".zig"}},
		"empty":  {Command: []string{"x"}},
	}
	got := cfg.LSPServers()
	assert.Equal(t, []string{"my-gopls"}, got["go"].Command)
	assert.Equal(t, DefaultLSPServers["go"].Extensions, got["go"].Extensions)
	assert.NotContains(t, got, "python")
	assert.NotContains(t, got, "empty", "no extensions")
	assert.Equal(t, []string{".zig"}, got["zig"].Extensions)

	t.Setenv("HOME", "")
	assert.Equal(t, ".blitz", Dir())
}
