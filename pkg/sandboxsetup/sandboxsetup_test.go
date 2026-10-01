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

package sandboxsetup

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSystem is a Linux machine with bwrap at dir/bwrap, AppArmor's
// restriction as given, apparmor_parser if parser, and a probe that
// succeeds once works says so.
type fakeSystem struct {
	dir   string
	works bool
	ran   [][]string // what Fix ran
}

func newFakeSystem(t *testing.T, restriction string, withParser, withBwrap bool, elevations ...string) *fakeSystem {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir()) // macOS: /var is a link, and Check resolves bwrap's path
	require.NoError(t, err)
	f := &fakeSystem{dir: dir}
	bin := filepath.Join(f.dir, "bin")
	require.NoError(t, os.MkdirAll(bin, 0o755))
	if withBwrap {
		require.NoError(t, os.WriteFile(filepath.Join(bin, "bwrap"), nil, 0o755))
	}
	for _, e := range elevations {
		require.NoError(t, os.WriteFile(filepath.Join(bin, e), nil, 0o755))
	}
	parserPath := filepath.Join(f.dir, "apparmor_parser")
	if withParser {
		require.NoError(t, os.WriteFile(parserPath, nil, 0o755))
	}
	restrict := filepath.Join(f.dir, "restrict")
	if restriction != "" {
		require.NoError(t, os.WriteFile(restrict, []byte(restriction+"\n"), 0o644))
	}
	old := []any{goos, lookPath, restrictFile, profileDir, parsers, probe, run}
	t.Cleanup(func() {
		goos, lookPath, restrictFile, profileDir = old[0].(string), old[1].(func(string) (string, error)), old[2].(string), old[3].(string)
		parsers = old[4].([]string)
		probe = old[5].(func(context.Context, string) ([]byte, error))
		run = old[6].(func(*exec.Cmd) error)
	})
	goos = "linux"
	lookPath = func(name string) (string, error) {
		p := filepath.Join(bin, name)
		if _, err := os.Stat(p); err != nil {
			return "", exec.ErrNotFound
		}
		return p, nil
	}
	restrictFile = restrict
	profileDir = filepath.Join(f.dir, "apparmor.d")
	parsers = []string{parserPath}
	probe = func(context.Context, string) ([]byte, error) {
		if f.works {
			return nil, nil
		}
		return []byte("bwrap: setting up uid map: Permission denied\n"), errors.New("exit status 1")
	}
	run = func(cmd *exec.Cmd) error {
		f.ran = append(f.ran, cmd.Args)
		return nil
	}
	return f
}

// Check tells the states apart: where Fix applies, and where it doesn't.
func TestCheck(t *testing.T) {
	tests := []struct {
		name        string
		goos        string
		restriction string
		parser      bool
		bwrap       bool
		works       bool
		want        State
	}{
		{name: "macOS", goos: "darwin", want: Unsupported},
		{name: "no bubblewrap", restriction: "1", parser: true, want: NoBwrap},
		{name: "it runs", restriction: "1", parser: true, bwrap: true, works: true, want: Ready},
		{name: "AppArmor restricts it", restriction: "1", parser: true, bwrap: true, want: Restricted},
		{name: "restricted, no apparmor_parser", restriction: "1", bwrap: true, want: Broken},
		{name: "not restricted, still failing", restriction: "0", parser: true, bwrap: true, want: Broken},
		{name: "no restriction setting", parser: true, bwrap: true, want: Broken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeSystem(t, tt.restriction, tt.parser, tt.bwrap)
			f.works = tt.works
			if tt.goos != "" {
				goos = tt.goos
			}
			st := Check(context.Background())
			assert.Equal(t, tt.want, st.State)
			if tt.want == Restricted || tt.want == Broken {
				assert.Equal(t, "bwrap: setting up uid map: Permission denied", st.Detail)
				assert.Equal(t, filepath.Join(f.dir, "bin", "bwrap"), st.Bwrap)
			}
		})
	}
}

// The profile lets bwrap, by its path, create user namespaces, and nothing
// else changes.
func TestProfile(t *testing.T) {
	p := Profile("/usr/bin/bwrap")
	assert.Contains(t, p, "profile blitz-bwrap /usr/bin/bwrap flags=(unconfined) {")
	assert.Contains(t, p, "  userns,\n")
	assert.Contains(t, p, "include if exists <local/blitz-bwrap>")
	assert.Contains(t, p, "abi <abi/4.0>,")
}

// Fix installs and loads the profile with one elevated command, then
// checks again; it leaves a working or unfixable sandbox alone.
func TestFix(t *testing.T) {
	t.Run("restricted: installed and loaded", func(t *testing.T) {
		f := newFakeSystem(t, "1", true, true, "sudo")
		run = func(cmd *exec.Cmd) error {
			f.ran = append(f.ran, cmd.Args)
			profile, err := os.ReadFile(cmd.Args[5]) // the temporary copy, while it exists
			require.NoError(t, err)
			assert.Contains(t, string(profile), filepath.Join(f.dir, "bin", "bwrap"))
			f.works = true
			return nil
		}
		st, err := Fix(context.Background(), Elevation{})
		require.NoError(t, err)
		assert.Equal(t, Ready, st.State)
		require.Len(t, f.ran, 1)
		args := f.ran[0]
		assert.Equal(t, filepath.Join(f.dir, "bin", "sudo"), args[0])
		assert.Equal(t, []string{"/bin/sh", "-c", install, "sh"}, args[1:5])
		assert.Equal(t, []string{filepath.Join(f.dir, "apparmor.d", ProfileName), filepath.Join(f.dir, "apparmor_parser")}, args[6:])
		assert.NoFileExists(t, args[5], "the temporary copy is removed")
	})
	t.Run("pkexec for the desktop", func(t *testing.T) {
		f := newFakeSystem(t, "1", true, true, "sudo", "pkexec")
		run = func(cmd *exec.Cmd) error { f.ran = append(f.ran, cmd.Args); f.works = true; return nil }
		_, err := Fix(context.Background(), Elevation{Graphical: true})
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(f.dir, "bin", "pkexec"), f.ran[0][0])
	})
	t.Run("the desktop without pkexec", func(t *testing.T) {
		newFakeSystem(t, "1", true, true, "sudo")
		_, err := Fix(context.Background(), Elevation{Graphical: true})
		assert.ErrorIs(t, err, ErrNoElevation, "sudo has no terminal to ask in")
	})
	t.Run("the password refused", func(t *testing.T) {
		f := newFakeSystem(t, "1", true, true, "sudo")
		run = func(cmd *exec.Cmd) error {
			f.ran = append(f.ran, cmd.Args)
			_, _ = cmd.Stderr.Write([]byte("sudo: 3 incorrect password attempts\n"))
			return errors.New("exit status 1")
		}
		st, err := Fix(context.Background(), Elevation{})
		assert.ErrorContains(t, err, "3 incorrect password attempts")
		assert.Equal(t, Restricted, st.State)
	})
	t.Run("loaded, but still failing", func(t *testing.T) {
		newFakeSystem(t, "1", true, true, "sudo")
		_, err := Fix(context.Background(), Elevation{})
		assert.ErrorContains(t, err, "bubblewrap still fails")
	})
	t.Run("no sudo or pkexec", func(t *testing.T) {
		newFakeSystem(t, "1", true, true)
		_, err := Fix(context.Background(), Elevation{})
		assert.ErrorIs(t, err, ErrNoElevation)
	})
	t.Run("working already", func(t *testing.T) {
		f := newFakeSystem(t, "1", true, true, "sudo")
		f.works = true
		st, err := Fix(context.Background(), Elevation{})
		require.NoError(t, err)
		assert.Equal(t, Ready, st.State)
		assert.Empty(t, f.ran)
	})
	t.Run("not AppArmor's doing", func(t *testing.T) {
		f := newFakeSystem(t, "0", true, true, "sudo")
		_, err := Fix(context.Background(), Elevation{})
		assert.ErrorIs(t, err, ErrCantFix)
		assert.Empty(t, f.ran)
	})
}

// The commands to run by hand write the same profile and load it.
func TestCommands(t *testing.T) {
	f := newFakeSystem(t, "1", true, true)
	st := Check(context.Background())
	cmds := Commands(st)
	assert.Contains(t, cmds, "sudo tee "+filepath.Join(f.dir, "apparmor.d", ProfileName))
	assert.Contains(t, cmds, Profile(st.Bwrap))
	assert.Contains(t, cmds, "sudo "+filepath.Join(f.dir, "apparmor_parser")+" -r ")
}
