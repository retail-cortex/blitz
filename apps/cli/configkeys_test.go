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

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigKeyCommands(t *testing.T) {
	isolate(t)
	store := &secrets.Memory{}
	secrets.SetDefault(store)
	run := func(stdin string, args ...string) (string, error) {
		t.Helper()
		cmd := newRootCommand()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetIn(strings.NewReader(stdin))
		cmd.SetArgs(args)
		err := cmd.Execute()
		return out.String(), err
	}

	_, err := run("sk-ant-cli\n", "config", "set-key", "anthropic")
	require.NoError(t, err)
	v, _ := store.Get("global/llm.anthropic.api_key")
	assert.Equal(t, "sk-ant-cli", v, "stored %q", v)
	out, err := run("", "config", "keys")
	assert.NoError(t, err, "keys:\n%s", out)
	assert.Contains(t, out, "anthropic  keychain", "keys:\n%s %v", out, err)
	assert.NotContains(t, out, "sk-ant-cli", "keys:\n%s %v", out, err)

	// The workspace's own key, for the current directory.
	_, err = run("AIza-ws\n", "config", "set-key", "--workspace", "gemini")
	require.NoError(t, err)
	out, _ = run("", "config", "keys", "-w")
	assert.Contains(t, out, "gemini     keychain", "workspace keys:\n%s", out)
	assert.Contains(t, out, "anthropic  inherited", "workspace keys:\n%s", out)
	_, err = run("", "config", "remove-key", "-w", "gemini")
	require.NoError(t, err)
	out, _ = run("", "config", "keys", "-w")
	assert.Contains(t, out, "gemini     none", "after remove")

	// Signing in without a key.
	_, err = run("", "config", "set-auth", "gemini", "adc", "--project", "my-project")
	require.NoError(t, err)
	_, err = run("", "config", "set-auth", "anthropic", "OAuth", "--profile", "work")
	require.NoError(t, err)
	out, _ = run("", "config", "keys")
	assert.Contains(t, out, "gemini     Google Cloud ADC, project my-project, location from GOOGLE_CLOUD_LOCATION, else global", "keys:\n%s", out)
	assert.Contains(t, out, "anthropic  OAuth, `ant auth login` profile work (key: keychain, unused)", "keys:\n%s", out)
	_, err = run("", "config", "set-auth", "anthropic", "adc", "--project", "claude-p", "--location", "us-east5")
	require.NoError(t, err)
	out, _ = run("", "config", "keys")
	assert.Contains(t, out, "anthropic  Google Cloud ADC, project claude-p, location us-east5", "keys:\n%s", out)
	_, err = run("", "config", "set-auth", "anthropic", "api_key")
	require.NoError(t, err)
	out, _ = run("", "config", "keys")
	assert.Contains(t, out, "anthropic  keychain\n", "keys:\n%s", out)

	for name, args := range map[string][]string{
		"no key":           {"config", "set-key", "openai"},
		"not keyed":        {"config", "set-key", "ollama"},
		"nothing here":     {"config", "secure-key", "openai"},
		"not Gemini's":     {"config", "set-auth", "gemini", "oauth"},
		"no project flag":  {"config", "set-auth", "anthropic", "adc", "--projet", "p"},
		"no such method":   {"config", "set-auth", "anthropic", "password"},
		"a key only":       {"config", "set-auth", "openai", "adc"},
		"no method at all": {"config", "set-auth", "gemini"},
	} {
		_, err := run("", args...)
		assert.Error(t, err, "%s: no error", name)
	}
}

// blitz config permissions: rules per scope, checked before saving, and the
// built-in read-only rules.
func TestConfigPermissionsCommands(t *testing.T) {
	isolate(t)
	run := func(args ...string) (string, error) {
		t.Helper()
		cmd := newRootCommand()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(args)
		err := cmd.Execute()
		return out.String(), err
	}
	_, err := run("config", "permissions", "deny", "shell(git push)")
	require.NoError(t, err)
	out, err := run("config", "permissions", "allow", "-w", "Bash(make)")
	require.NoError(t, err, out)
	assert.Contains(t, out, "allow shell(make)")

	out, err = run("config", "permissions", "-w")
	require.NoError(t, err)
	assert.Regexp(t, `(?s)workspace \(.*allow shell\(make\).*global \(.*deny  shell\(git push\).*built-in \(read_only_defaults on`, out)
	out, _ = run("config", "permissions")
	assert.NotContains(t, out, "shell(make)", "the workspace's rule is only the workspace's")

	out, err = run("config", "permissions", "check", "shell(git log)", "git log --oneline | head")
	require.NoError(t, err)
	assert.Contains(t, out, `the command "git log" with any arguments`)
	assert.Contains(t, out, "doesn't match", "head isn't covered by shell(git log)")
	out, err = run("config", "permissions", "check", "--effect", "deny", "shell(re:git (push|reset)( .*)?)", "git fetch && git push -f")
	require.NoError(t, err)
	assert.Contains(t, out, "matches")

	_, err = run("config", "permissions", "defaults", "off", "-w")
	require.NoError(t, err)
	out, _ = run("config", "permissions", "-w")
	assert.Contains(t, out, "built-in: off")
	_, err = run("config", "permissions", "remove", "-w", "shell(make)")
	require.NoError(t, err)

	for name, args := range map[string][]string{
		"a bad rule":        {"config", "permissions", "allow", "shel(ls)"},
		"a bad regex":       {"config", "permissions", "deny", "shell(re:()"},
		"read isn't allow":  {"config", "permissions", "allow", "read(x)"},
		"nothing to remove": {"config", "permissions", "remove", "shell(nope)"},
		"defaults maybe":    {"config", "permissions", "defaults", "maybe"},
	} {
		_, err := run(args...)
		assert.Error(t, err, name)
	}
}
