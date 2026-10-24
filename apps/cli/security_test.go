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
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/sandboxsetup"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Each sandbox state says what it is and what to do; only AppArmor's
// restriction points at fix-apparmor.
func TestPrintSandboxStatus(t *testing.T) {
	tests := []struct {
		state      sandboxsetup.State
		want       string
		suggestFix bool
	}{
		{sandboxsetup.Ready, "bubblewrap (/usr/bin/bwrap) runs the agent's commands", false},
		{sandboxsetup.NoBwrap, "sudo apt install bubblewrap", false},
		{sandboxsetup.Restricted, "AppArmor keeps bubblewrap (/usr/bin/bwrap)", true},
		{sandboxsetup.Broken, "sandbox.shell = \"auto\"", false},
		{sandboxsetup.Unsupported, "needs no set-up here", false},
	}
	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			var out bytes.Buffer
			printSandboxStatus(&out, sandboxsetup.Status{State: tt.state, Bwrap: "/usr/bin/bwrap", Detail: "why"})
			assert.Contains(t, out.String(), tt.want)
			assert.Equal(t, tt.suggestFix, bytes.Contains(out.Bytes(), []byte("blitz security fix-apparmor")))
		})
	}
}

// blitz security has the fix-apparmor subcommand and opens no workspace.
func TestSecurityCommand(t *testing.T) {
	cmd := newSecurityCommand()
	fix, _, err := cmd.Find([]string{"fix-apparmor"})
	assert.NoError(t, err)
	assert.Equal(t, "fix-apparmor", fix.Name())
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs(nil)
	assert.NoError(t, cmd.Execute())
	assert.NotEmpty(t, out.String())
}

// blitz security scan-secrets names each file and line with the value
// masked and fails; clean files pass; install-hook adds the hook once.
func TestScanSecretsAndInstallHook(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)                       // outside any repository
	key := "AKIA" + "Q3EXAMPLEZ7Y4WXN" // put together: no key in this file
	leak := filepath.Join(dir, "leak.go")
	clean := filepath.Join(dir, "clean.go")
	require.NoError(t, os.WriteFile(leak, []byte("package a\nconst k = \""+key+"\"\n"), 0o644))
	require.NoError(t, os.WriteFile(clean, []byte("package a\n"), 0o644))
	run := func(args ...string) (string, error) {
		cmd := newSecurityCommand()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(args)
		err := cmd.Execute()
		return out.String(), err
	}
	tests := []struct {
		name    string
		args    []string
		want    string
		failure bool
	}{
		{"a key", []string{"scan-secrets", leak, clean}, "leak.go:2: AWS access key (AKIA••••", true},
		{"clean", []string{"scan-secrets", clean}, "", false},
		{"nothing named", []string{"scan-secrets"}, "name the files", true},
		{"both", []string{"scan-secrets", "--staged", clean}, "name the files", true},
		{"a file that isn't there", []string{"scan-secrets", filepath.Join(dir, "gone.go")}, "gone.go", true},
		{"staged, outside a repository", []string{"scan-secrets", "--staged"}, "not a git repository", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := run(tt.args...)
			if tt.failure {
				require.Error(t, err)
				assert.Contains(t, out+err.Error(), tt.want)
				assert.NotContains(t, out, key, "never the key itself")
			} else {
				assert.NoError(t, err)
			}
		})
	}
	if _, err := exec.LookPath("git"); err != nil {
		return
	}
	repo := t.TempDir()
	init := exec.Command("git", "init", "-q")
	init.Dir = repo
	require.NoError(t, init.Run())
	_, err := run("install-hook", t.TempDir())
	assert.Error(t, err, "not a repository")
	add := exec.Command("git", "add", ".")
	add.Dir = repo
	t.Chdir(repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "leak.go"), []byte("package a\nconst k = \""+key+"\"\n"), 0o644))
	require.NoError(t, add.Run())
	out, err := run("scan-secrets", "--staged")
	require.Error(t, err)
	assert.Contains(t, out, "leak.go:2: AWS access key", "what the commit adds")
	out, err = run("install-hook", repo)
	require.NoError(t, err)
	assert.Contains(t, out, "Added the pre-commit hook")
	out, err = run("install-hook", repo)
	require.NoError(t, err)
	assert.Contains(t, out, "already there")
}
