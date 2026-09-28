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
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSeatbeltProfile(t *testing.T) {
	m, _ := NewPathMatcher([]string{".env"}, nil)
	profile := seatbeltProfile(OSSandboxSpec{WritableDirs: []string{`/ws/with "quote"`}, ReadOnlyDirs: []string{"/ws/ro"}, Blocked: m})
	for _, want := range []string{
		"(allow default)",
		"(deny file-write*)",
		`(subpath "/ws/with \"quote\"")`,
		`(regex #"/\.env(/.*)?$")`,
		"(deny network*)",
	} {
		assert.Contains(t, profile, want, "profile missing %q:\n%s", want, profile)
	}
	// Read-only and blocked rules must come after the writable allowances so they win.
	allowAt := strings.Index(profile, "(allow file-write*")
	assert.GreaterOrEqual(t, strings.Index(profile, "(deny file-read*"), allowAt, "read-only and blocked rules must follow writable allowances")
	assert.GreaterOrEqual(t, strings.Index(profile, "(subpath \"/ws/ro\")"), allowAt, "read-only and blocked rules must follow writable allowances")
	assert.NotContains(t, seatbeltProfile(OSSandboxSpec{AllowNetwork: true}), "deny network", "network should not be denied when allowed")
}

func TestNewOSSandboxModes(t *testing.T) {
	orig := platformSandbox
	t.Cleanup(func() { platformSandbox = orig })

	_, err := NewOSSandbox(OSSandboxSpec{Mode: "sometimes"})
	assert.Error(t, err, "expected error for invalid mode")

	off, _ := NewOSSandbox(OSSandboxSpec{Mode: SandboxOff})
	assert.False(t, off.Active(), "off mode: %s", off.Status())
	assert.Contains(t, off.Status(), "disabled", "off mode: %s", off.Status())
	got := off.wrap([]string{"ls"})
	assert.Len(t, got, 1, "inactive sandbox must not wrap: %v", got)

	platformSandbox = func(OSSandboxSpec) (sandboxWrapper, error) { return nil, errors.New("no sandbox here") }
	// Auto degrades gracefully and says why.
	auto, err := NewOSSandbox(OSSandboxSpec{Mode: SandboxAuto})
	assert.NoError(t, err, "auto without platform support: %v %s", err, auto.Status())
	assert.False(t, auto.Active(), "auto without platform support: %v %s", err, auto.Status())
	assert.Contains(t, auto.Status(), "no sandbox here", "auto without platform support: %v %s", err, auto.Status())
	// Required fails closed.
	_, err = NewOSSandbox(OSSandboxSpec{Mode: SandboxRequired})
	assert.Error(t, err, "required without platform support should fail, got")
	assert.Contains(t, err.Error(), "required", "required without platform support should fail, got %v", err)

	platformSandbox = func(OSSandboxSpec) (sandboxWrapper, error) { return prefixWrapper("sbx", "--"), nil }
	on, _ := NewOSSandbox(OSSandboxSpec{Mode: SandboxRequired})
	assert.Equal(t, "sbx -- ls -l", strings.Join(on.wrap([]string{"ls", "-l"}), " "), "wrap")
}

func TestRegistryFailsClosedWhenSandboxRequired(t *testing.T) {
	orig := platformSandbox
	t.Cleanup(func() { platformSandbox = orig })
	platformSandbox = func(OSSandboxSpec) (sandboxWrapper, error) { return nil, errors.New("unsupported") }

	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Sandbox.Shell = "required"
	_, err := NewRegistry(cfg, nil, nil)
	require.Error(t, err, "expected registry to refuse to start without the required sandbox")

	// Negative config: bad pattern / missing allowed path are startup errors.
	cfg.Sandbox.Shell = "off"
	cfg.Sandbox.BlockedPaths = []string{`x"y`}
	_, err = NewRegistry(cfg, nil, nil)
	assert.Error(t, err, "expected error for invalid blocked pattern")
	cfg.Sandbox.BlockedPaths = nil
	cfg.Sandbox.AllowedPaths = []string{filepath.Join(t.TempDir(), "nope")}
	_, err = NewRegistry(cfg, nil, nil)
	assert.Error(t, err, "expected error for missing allowed path")
}

// TestOSSandboxEnforcement runs real commands under the platform sandbox
// (Seatbelt on macOS, bubblewrap on Linux) and is skipped where none works.
func TestOSSandboxEnforcement(t *testing.T) {
	f := newSandboxFixture(t)
	// The fixtures live under $TMPDIR, so the default temp allowances are left
	// out here; otherwise every fixture directory would be writable.
	osb, err := NewOSSandbox(OSSandboxSpec{
		Mode: SandboxRequired, WritableDirs: f.ws.WritableDirs(), ReadOnlyDirs: f.ws.ReadOnlyDirs(),
		Blocked: f.ws.Blocked(), AllowNetwork: true,
	})
	if err != nil {
		t.Skipf("no usable OS sandbox on %s: %v", runtime.GOOS, err)
	}
	cfg := ShellConfig{Workspace: f.ws, Hooks: allowAll(), Exec: &ExecEnv{Sandbox: osb}}
	run := func(cmd string) RunShellCommandOutput {
		return runShellCommand(context.Background(), cfg, RunShellCommandInput{Command: cmd})
	}

	// Positive: writes inside the workspace and allowed roots work.
	out := run("echo ok > inside.txt && echo ok > " + filepath.Join(f.shared, "s.txt"))
	assert.Equal(t, 0, out.ExitCode, "allowed writes failed: %+v", out)
	// Negative: writes to read-only roots and outside all roots are denied.
	for _, target := range []string{filepath.Join(f.docs, "x.md"), filepath.Join(f.outside, "x.txt"), filepath.Join(f.work, "vendor-ro", "../vendor-ro/x.go")} {
		out := run("echo pwned > '" + target + "'")
		assert.NotEqual(t, 0, out.ExitCode, "sandbox allowed write to %s", target)
	}
	b, _ := os.ReadFile(filepath.Join(f.outside, "x.txt"))
	assert.Equal(t, "outside\n", string(b), "file outside the sandbox was modified")
	// Negative: blocked files can't be read by the shell, even inside the
	// workspace. (Seatbelt denies the read; bubblewrap shows an empty file.)
	out = run("cat .env")
	assert.NotContains(t, out.Output, "API_KEY", "shell read a blocked file: %+v", out)
	run("cp .env leaked.txt")
	b, _ = os.ReadFile(filepath.Join(f.work, "leaked.txt"))
	assert.NotContains(t, string(b), "API_KEY", "shell copied a blocked file's contents")
	// A read-only root nested in the writable workspace stays read-only.
	out = run("echo pwned > vendor-ro/lib.go")
	assert.NotEqual(t, 0, out.ExitCode, "shell wrote into a read-only root nested in the workspace")
	// Reads elsewhere still work.
	out = run("cat main.go")
	assert.Contains(t, out.Output, "package main", "normal read failed: %+v", out)
}

// TestOSSandboxIsolationExtras covers home-directory secrets, symlinks to
// blocked files, system paths, and network denial under the real platform
// sandbox; skipped where none is usable.
func TestOSSandboxIsolationExtras(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeFile(t, filepath.Join(home, ".ssh", "id_ed25519"), "PRIVATE-KEY-MATERIAL")
	ws, dir := newTestWorkspace(t)
	writeFile(t, filepath.Join(dir, ".env"), "API_KEY=leak")
	require.NoError(t, os.Symlink(".env", filepath.Join(dir, "innocent.txt")))
	blocked, err := NewPathMatcher([]string{".env", "~/.ssh"}, ws.RootDirs())
	require.NoError(t, err)
	osb, err := NewOSSandbox(OSSandboxSpec{Mode: SandboxRequired, WritableDirs: ws.WritableDirs(), Blocked: blocked, AllowNetwork: false})
	if err != nil {
		t.Skipf("no usable OS sandbox on %s: %v", runtime.GOOS, err)
	}
	cfg := ShellConfig{Workspace: ws, Hooks: allowAll(), Exec: &ExecEnv{Sandbox: osb}}
	run := func(cmd string) RunShellCommandOutput {
		return runShellCommand(context.Background(), cfg, RunShellCommandInput{Command: cmd, TimeoutSeconds: 15})
	}

	out := run("cat \"$HOME/.ssh/id_ed25519\"; ls -A \"$HOME/.ssh\"")
	assert.NotContains(t, out.Output, "PRIVATE-KEY", "home secret visible: %q", out.Output)
	assert.NotContains(t, out.Output, "id_ed25519\n", "home secret visible: %q", out.Output)
	out = run("cat innocent.txt")
	assert.NotContains(t, out.Output, "leak", "symlink bypassed blocked path: %q", out.Output)
	out = run("echo x >> .env")
	assert.NotEqual(t, 0, out.ExitCode, "blocked file was writable")
	out = run("echo x > /etc/blitz-sandbox-test")
	assert.NotEqual(t, 0, out.ExitCode, "system path was writable")
	out = run("echo x > \"$HOME/outside.txt\"")
	assert.NotEqual(t, 0, out.ExitCode, "home directory outside the workspace was writable")
	// Network denied: a raw TCP connect must fail (never succeeds offline either).
	out = run("timeout 5 bash -c 'echo > /dev/tcp/1.1.1.1/53' && echo CONNECTED")
	assert.NotContains(t, out.Output, "CONNECTED", "network reachable with allow_network = false")
	b, _ := os.ReadFile(filepath.Join(dir, ".env"))
	assert.Equal(t, "API_KEY=leak", string(b), "blocked file was modified")
}
