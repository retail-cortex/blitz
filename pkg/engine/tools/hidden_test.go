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
	"bufio"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/retail-cortex/blitz/pkg/activity"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Hidden paths (keys, certificates, .env files) are kept from the model,
// not from commands: the file tools don't read them, list them or share
// them, but may make one; the OS sandbox doesn't hide them, so pip finds
// the system's CA bundle. A project's .env.*.toml is its own.
func TestWorkspaceHiddenPaths(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		".env": "SECRET=1\n", "certs/ca.pem": "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n",
		".env.dev.toml": "port = 8080\n", "main.go": "package main\n",
	} {
		writeFile(t, filepath.Join(dir, name), body)
	}
	ws, err := OpenWorkspace(WorkspaceOptions{Dir: dir, BlockedPaths: config.DefaultBlockedPaths, HiddenPaths: config.DefaultHiddenPaths})
	require.NoError(t, err)
	t.Cleanup(func() { ws.Close() })

	for _, p := range []string{".env", "certs/ca.pem"} {
		t.Run(p, func(t *testing.T) {
			_, err := ws.ReadFile(p)
			assert.ErrorIs(t, err, ErrHiddenPath)
			assert.ErrorIs(t, err, ErrBlockedPath, "a hidden file is refused as a blocked one is")
			assert.Equal(t, "blocked", ws.AgentRule(p), "the file tree doesn't share it with the agent")
			_, kept := ws.KeptFromModel(filepath.Join(ws.Dir(), p))
			assert.True(t, kept)
		})
	}
	require.NoError(t, ws.CreateExclusive(context.Background(), "new.key", []byte("made")), "the agent may make a hidden file")
	_, err = ws.ReadFile("new.key")
	assert.ErrorIs(t, err, ErrHiddenPath, "and still never read it")

	// Through a link with an innocent name too.
	require.NoError(t, os.Symlink(".env", filepath.Join(dir, "settings.txt")))
	_, err = ws.ReadFile("settings.txt")
	assert.ErrorIs(t, err, ErrHiddenPath)
	_, err = OpenWorkspace(WorkspaceOptions{Dir: dir, HiddenPaths: []string{`bad"name`}})
	assert.ErrorContains(t, err, "invalid", "a pattern that can't be one")

	data, err := ws.ReadFile(".env.dev.toml")
	require.NoError(t, err, "a project's .env.*.toml isn't Blitz's settings")
	assert.Equal(t, "port = 8080\n", string(data))
	assert.Equal(t, "", ws.AgentRule(".env.dev.toml"))

	var seen []string
	require.NoError(t, ws.Walk(".", func(e WalkEntry) error {
		seen = append(seen, filepath.ToSlash(e.Path))
		return nil
	}))
	assert.NotContains(t, seen, ".env")
	assert.NotContains(t, seen, "certs/ca.pem")
	assert.Contains(t, seen, ".env.dev.toml")

	profile := seatbeltProfile(OSSandboxSpec{WritableDirs: []string{ws.Dir()}, Blocked: ws.Blocked()})
	assert.NotContains(t, profile, "pem", "commands read certificates")
	assert.NotContains(t, profile, `/\.env(`, "and .env files")
	assert.Contains(t, profile, ".ssh", "the secret stores stay blocked")
	home, _ := os.UserHomeDir()
	_, blocked := ws.Blocked().Match(filepath.Join(home, ".blitz", ".env.toml"))
	assert.True(t, blocked, "Blitz's own settings stay blocked")
	assert.Contains(t, ws.Describe()[len(ws.Describe())-1], "*.pem")
}

// A command reads a hidden file (here a certificate) as any program
// does, but a private key it prints never reaches the model.
func TestShellHidesPrivateKeys(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	writeFile(t, filepath.Join(dir, "ca.pem"), "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n")
	key := "-----BEGIN RSA PRIVATE KEY-----\nMIIEsecret\n-----END RSA PRIVATE KEY-----"
	writeFile(t, filepath.Join(dir, "k.txt"), "before\n"+key+"\nafter\n")
	out := runShellCommand(context.Background(), shellCfg(ws), RunShellCommandInput{Command: "cat ca.pem k.txt"})
	require.Equal(t, 0, out.ExitCode, "%+v", out)
	assert.Contains(t, out.Output, "BEGIN CERTIFICATE")
	assert.Contains(t, out.Output, "before\n[a private key, kept from the model]\nafter")
	assert.NotContains(t, out.Output, "MIIEsecret")
}

// What Blitz runs for the model says so (activity.AgentEnv): a blitz
// command there isn't the person's use of the CLI (spec_054 AI-03a). A
// language server gets its extra environment too (a Bazel workspace's
// package driver).
func TestCommandsSayTheyreTheAgents(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	cmd, err := (&ExecEnv{Dir: t.TempDir()}).command(context.Background(), []string{"sh", "-c", "echo $" + activity.AgentEnv})
	require.NoError(t, err)
	out, err := cmd.Output()
	require.NoError(t, err)
	assert.Equal(t, "1\n", string(out))

	p, err := lspLauncher(&ExecEnv{Dir: t.TempDir()})(context.Background(), "", []string{"sh", "-c", "echo $" + activity.AgentEnv + " $EXTRA"}, []string{"EXTRA=driver"})
	require.NoError(t, err)
	line, err := bufio.NewReader(p.Stdout()).ReadString('\n')
	require.NoError(t, err)
	assert.Equal(t, "1 driver\n", line)
	p.Stop()
}
