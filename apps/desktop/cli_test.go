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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// program writes an executable file at dir/name and returns its path,
// symlinks resolved.
func program(t *testing.T, dir, name string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755))
	p, err := filepath.EvalSymlinks(p)
	require.NoError(t, err)
	return p
}

// A new terminal's PATH comes from the login shell, whatever else its
// startup files print; without a shell that works, it's the app's own.
func TestUserPath(t *testing.T) {
	dir := t.TempDir()
	shell := filepath.Join(dir, "sh")
	require.NoError(t, os.WriteFile(shell, []byte("#!/bin/sh\necho welcome\nprintf 'BLITZ_PATH=/a:/b\\n'\necho bye\n"), 0o755))
	t.Setenv("PATH", "/app/bin")
	tests := []struct {
		name  string
		shell string
		want  []string
	}{
		{"the shell's", shell, []string{"/a", "/b"}},
		{"no shell", filepath.Join(dir, "missing"), []string{"/app/bin"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, userPath(tt.shell))
		})
	}
}

// Each shell's startup file, the one a new terminal reads.
func TestProfileFor(t *testing.T) {
	t.Setenv("ZDOTDIR", "")
	home := "/home/u"
	dir := "/home/u/.local/bin"
	tests := []struct {
		shell, goos, file, line string
	}{
		{"/bin/zsh", "darwin", "/home/u/.zshrc", `export PATH="$HOME/.local/bin:$PATH"`},
		{"/bin/bash", "darwin", "/home/u/.bash_profile", `export PATH="$HOME/.local/bin:$PATH"`},
		{"/bin/bash", "linux", "/home/u/.bashrc", `export PATH="$HOME/.local/bin:$PATH"`},
		{"/usr/bin/fish", "linux", "/home/u/.config/fish/conf.d/blitz.fish", "fish_add_path --path $HOME/.local/bin"},
		{"/bin/dash", "linux", "/home/u/.profile", `export PATH="$HOME/.local/bin:$PATH"`},
	}
	for _, tt := range tests {
		t.Run(filepath.Base(tt.shell)+"_"+tt.goos, func(t *testing.T) {
			file, line := profileFor(home, tt.shell, tt.goos, dir)
			assert.Equal(t, tt.file, file)
			assert.Equal(t, tt.line, line)
		})
	}
	t.Run("ZDOTDIR", func(t *testing.T) {
		t.Setenv("ZDOTDIR", "/home/u/.config/zsh")
		file, _ := profileFor(home, "/bin/zsh", "linux", dir)
		assert.Equal(t, "/home/u/.config/zsh/.zshrc", file)
	})
}

// The link goes in the user's own directory already on PATH, else in
// ~/.local/bin, which then needs adding.
func TestLinkDir(t *testing.T) {
	home := t.TempDir()
	local := filepath.Join(home, ".local", "bin")
	bin := filepath.Join(home, "bin")
	tests := []struct {
		name   string
		path   []string
		dir    string
		onPath bool
	}{
		{"~/.local/bin on PATH", []string{"/usr/bin", local}, local, true},
		{"~/bin on PATH", []string{bin, "/usr/bin"}, bin, true},
		{"both: ~/.local/bin first", []string{bin, local}, local, true},
		{"neither", []string{"/usr/bin"}, local, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, onPath := linkDir(home, tt.path)
			assert.Equal(t, tt.dir, dir)
			assert.Equal(t, tt.onPath, onPath)
		})
	}
}

func TestInstallCLI(t *testing.T) {
	t.Setenv("ZDOTDIR", "")
	tests := []struct {
		name string
		// setup prepares home and returns PATH.
		setup   func(t *testing.T, home string) []string
		link    string // relative to home ("" for none)
		profile string // relative to home ("" for none)
		err     string
	}{
		{
			name: "a blitz on PATH already",
			setup: func(t *testing.T, home string) []string {
				program(t, filepath.Join(home, "other"), "blitz")
				return []string{filepath.Join(home, "other")}
			},
			link: "other/blitz",
		},
		{
			name:  "~/.local/bin on PATH",
			setup: func(t *testing.T, home string) []string { return []string{filepath.Join(home, ".local", "bin")} },
			link:  ".local/bin/blitz",
		},
		{
			name:    "nothing on PATH",
			setup:   func(t *testing.T, home string) []string { return []string{"/usr/bin"} },
			link:    ".local/bin/blitz",
			profile: ".zshrc",
		},
		{
			name: "a link left by a moved app",
			setup: func(t *testing.T, home string) []string {
				dir := filepath.Join(home, ".local", "bin")
				require.NoError(t, os.MkdirAll(dir, 0o755))
				require.NoError(t, os.Symlink("/gone/blitz", filepath.Join(dir, "blitz")))
				return []string{dir}
			},
			link: ".local/bin/blitz",
		},
		{
			name: "the user's own file",
			setup: func(t *testing.T, home string) []string {
				dir := filepath.Join(home, ".local", "bin")
				require.NoError(t, os.MkdirAll(dir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "blitz"), []byte("notes"), 0o644))
				return []string{dir}
			},
			err: "isn't a link",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			cli := program(t, t.TempDir(), "blitz")
			path := tt.setup(t, home)
			got, err := installCLI(cli, home, "/bin/zsh", "linux", path)
			if tt.err != "" {
				assert.ErrorContains(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, filepath.Join(home, tt.link), got.Link)
			if tt.profile == "" {
				assert.Empty(t, got.Profile)
			} else {
				assert.Equal(t, filepath.Join(home, tt.profile), got.Profile)
			}
			if !strings.HasPrefix(tt.link, "other/") {
				target, err := filepath.EvalSymlinks(got.Link)
				require.NoError(t, err)
				assert.Equal(t, cli, target)
			}
		})
	}
}

// Reinstalling: dead blitz links come off PATH, the link the shell finds
// is pointed at this app's, the user's own program is left alone, and with
// none left it installs as InstallCLI does.
func TestReinstallCLI(t *testing.T) {
	t.Setenv("ZDOTDIR", "")
	tests := []struct {
		name string
		// setup prepares home and returns PATH.
		setup   func(t *testing.T, home string) []string
		link    string   // relative to home
		removed []string // relative to home
		err     string
	}{
		{
			name: "another build's link",
			setup: func(t *testing.T, home string) []string {
				dir := filepath.Join(home, ".local", "bin")
				old := program(t, filepath.Join(home, "old-build"), "blitz")
				require.NoError(t, os.MkdirAll(dir, 0o755))
				require.NoError(t, os.Symlink(old, filepath.Join(dir, "blitz")))
				return []string{dir}
			},
			link: ".local/bin/blitz",
		},
		{
			name: "dead links before it",
			setup: func(t *testing.T, home string) []string {
				dead := filepath.Join(home, "bin")
				dir := filepath.Join(home, ".local", "bin")
				old := program(t, filepath.Join(home, "old-build"), "blitz")
				for _, d := range []string{dead, dir} {
					require.NoError(t, os.MkdirAll(d, 0o755))
				}
				require.NoError(t, os.Symlink("/gone/bazel-bin/blitz", filepath.Join(dead, "blitz")))
				require.NoError(t, os.Symlink(old, filepath.Join(dir, "blitz")))
				return []string{dead, dir}
			},
			link:    ".local/bin/blitz",
			removed: []string{"bin/blitz"},
		},
		{
			name: "only a dead link",
			setup: func(t *testing.T, home string) []string {
				dir := filepath.Join(home, ".local", "bin")
				require.NoError(t, os.MkdirAll(dir, 0o755))
				require.NoError(t, os.Symlink("/gone/blitz", filepath.Join(dir, "blitz")))
				return []string{dir}
			},
			link:    ".local/bin/blitz",
			removed: []string{".local/bin/blitz"},
		},
		{
			name: "already this app's",
			setup: func(t *testing.T, home string) []string {
				return []string{filepath.Join(home, ".local", "bin")}
			},
			link: ".local/bin/blitz",
		},
		{
			name: "the user's own program",
			setup: func(t *testing.T, home string) []string {
				program(t, filepath.Join(home, "tools"), "blitz")
				return []string{filepath.Join(home, "tools")}
			},
			err: "not a link",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			cli := program(t, t.TempDir(), "blitz")
			path := tt.setup(t, home)
			if tt.name == "already this app's" {
				require.NoError(t, os.MkdirAll(path[0], 0o755))
				require.NoError(t, os.Symlink(cli, filepath.Join(path[0], "blitz")))
			}
			got, err := reinstallCLI(cli, home, "/bin/zsh", "linux", path)
			if tt.err != "" {
				assert.ErrorContains(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, filepath.Join(home, tt.link), got.Link)
			var removed []string
			for _, r := range tt.removed {
				removed = append(removed, filepath.Join(home, r))
			}
			assert.Equal(t, removed, got.Removed)
			for _, r := range removed {
				if r != got.Link {
					assert.NoFileExists(t, r)
				}
			}
			target, err := filepath.EvalSymlinks(got.Link)
			require.NoError(t, err)
			assert.Equal(t, cli, target)
			assert.NoFileExists(t, got.Link+".blitz-new")
		})
	}
}

// The line that puts ~/.local/bin on PATH goes in once, after what the
// file had.
func TestAddToPathOnce(t *testing.T) {
	t.Setenv("ZDOTDIR", "")
	home := t.TempDir()
	rc := filepath.Join(home, ".zshrc")
	require.NoError(t, os.WriteFile(rc, []byte("alias ll='ls -l'"), 0o644))
	dir := filepath.Join(home, ".local", "bin")
	for range 2 {
		_, err := addToPath(home, "/bin/zsh", "linux", dir)
		require.NoError(t, err)
	}
	data, err := os.ReadFile(rc)
	require.NoError(t, err)
	assert.Equal(t, "alias ll='ls -l'\n\n# Added by Blitz, for its blitz command.\nexport PATH=\"$HOME/.local/bin:$PATH\"\n", string(data))
}

// The status tells this app's blitz on PATH from another one.
func TestCLIStatus(t *testing.T) {
	cli := program(t, t.TempDir(), "blitz")
	linked := t.TempDir()
	require.NoError(t, os.Symlink(cli, filepath.Join(linked, "blitz")))
	other := t.TempDir()
	program(t, other, "blitz")
	tests := []struct {
		name      string
		path      []string
		onPath    string
		installed bool
	}{
		{"this app's", []string{linked}, filepath.Join(linked, "blitz"), true},
		{"another", []string{other, linked}, filepath.Join(other, "blitz"), false},
		{"none", []string{t.TempDir()}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := cliStatus(cli, tt.path)
			assert.Equal(t, cli, s.CLI)
			assert.Equal(t, tt.onPath, s.OnPath)
			assert.Equal(t, tt.installed, s.Installed)
		})
	}
}
