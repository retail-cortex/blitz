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
	"os"
	"path/filepath"
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
	_, err = run("", "config", "set-auth", "gemini", "oauth", "--project", "my-project")
	require.NoError(t, err)
	out, _ = run("", "config", "keys")
	assert.Contains(t, out, "gemini     OAuth, your Google account (blitz auth login google), project my-project", "keys:\n%s", out)
	_, err = run("", "config", "set-auth", "bedrock", "oauth", "--profile", "dev")
	require.NoError(t, err, "bedrock signs in with AWS IAM Identity Center")
	_, err = run("", "config", "set-auth", "azure", "oauth")
	require.NoError(t, err, "azure with Entra ID")

	for name, args := range map[string][]string{
		"no key":           {"config", "set-key", "openai"},
		"not keyed":        {"config", "set-key", "ollama"},
		"nothing here":     {"config", "secure-key", "openai"},
		"not OpenAI's":     {"config", "set-auth", "openai", "oauth"},
		"not a provider":   {"config", "set-auth", "ollama", "oauth"},
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

// --workspace needs a workspace: with a --dir that isn't there, every
// key and permissions command is a usage error.
func TestConfigWorkspaceScopeErrors(t *testing.T) {
	isolate(t)
	missing := filepath.Join(t.TempDir(), "missing")
	for _, args := range [][]string{
		{"config", "keys", "-w"}, {"config", "set-key", "-w", "openai"}, {"config", "remove-key", "-w", "openai"},
		{"config", "secure-key", "-w", "openai"}, {"config", "set-auth", "-w", "gemini", "adc"},
		{"config", "permissions", "-w"}, {"config", "permissions", "allow", "-w", "shell(ls)"},
		{"config", "permissions", "remove", "-w", "shell(ls)"}, {"config", "permissions", "defaults", "-w", "on"},
		{"config", "show"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, err := runCLIWithInput(t, "sk-x\n", append([]string{"-d", missing}, args...)...)
			assert.Equal(t, exitUsage, exitCodeFor(err), "%v", err)
		})
	}
}

// Settings that don't parse fail the commands that edit or describe them.
func TestConfigBrokenSettings(t *testing.T) {
	home := isolate(t)
	secrets.SetDefault(&secrets.Memory{})
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".blitz"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".blitz", ".env.toml"), []byte("[llm\n"), 0o600))
	for _, args := range [][]string{
		{"config", "keys"}, {"config", "set-key", "openai"}, {"config", "remove-key", "openai"}, {"config", "secure-key", "openai"},
		{"config", "permissions"}, {"config", "permissions", "remove", "shell(ls)"}, {"config", "permissions", "defaults", "on"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, err := runCLIWithInput(t, "sk-x\n", args...)
			assert.Error(t, err)
		})
	}
}

// Keys written in the settings file are flagged to move, and secure-key
// moves them; a key the store lost is flagged to set again.
func TestConfigKeySources(t *testing.T) {
	home := isolate(t)
	store := &secrets.Memory{}
	secrets.SetDefault(store)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".blitz"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".blitz", ".env.toml"), []byte("[llm.openai]\napi_key = \"sk-plain-1234567890\"\n"), 0o600))
	out, err := runCLI(t, "config", "keys")
	require.NoError(t, err)
	assert.Contains(t, out, "(move it: blitz config secure-key openai)")
	out, err = runCLI(t, "config", "secure-key", "openai")
	require.NoError(t, err, out)
	assert.Contains(t, out, "Updated ")
	out, _ = runCLI(t, "config", "keys")
	assert.Contains(t, out, "openai     keychain")

	require.NoError(t, store.Delete("global/llm.openai.api_key"))
	out, _ = runCLI(t, "config", "keys")
	assert.Contains(t, out, "(missing from the store: set it again)")

	// show masks MCP servers' environment values too.
	_, err = runCLI(t, "mcp", "add", "db", "dbserver", "--env", "TOKEN=supersecretvalue")
	require.NoError(t, err)
	out, err = runCLI(t, "config", "show")
	require.NoError(t, err)
	assert.NotContains(t, out, "supersecretvalue")
	assert.Contains(t, out, "sup…alue")
}

// permissions check describes each form of rule, and says when an allowed
// command still asks because it redirects into a file.
func TestConfigPermissionsCheckForms(t *testing.T) {
	isolate(t)
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"shell(go test *)", "go test ./..."}, `commands matching "go test *" (* is any text)`},
		{[]string{"write(docs/**)"}, `paths matching "docs/**", and what's under them`},
		{[]string{"web(example.com)"}, `web "example.com"`},
		{[]string{"shell(echo)", "echo hi > out.txt"}, "but it writes out.txt through a redirection, so it still asks"},
	} {
		t.Run(tt.args[0], func(t *testing.T) {
			out, err := runCLI(t, append([]string{"config", "permissions", "check"}, tt.args...)...)
			require.NoError(t, err)
			assert.Contains(t, out, tt.want)
		})
	}
	_, err := runCLI(t, "config", "permissions", "check", "shel(ls)")
	assert.Equal(t, exitUsage, exitCodeFor(err))

	out, err := runCLI(t, "config", "permissions", "defaults", "inherit", "-w")
	require.NoError(t, err)
	assert.Contains(t, out, "Updated ")
	out, err = runCLI(t, "config", "permissions")
	require.NoError(t, err)
	assert.Contains(t, out, "global (")
	assert.Contains(t, out, "  none")
}
