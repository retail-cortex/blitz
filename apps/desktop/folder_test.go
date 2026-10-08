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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tray switch runs blitz-tray --install or --uninstall, and says why
// when it fails.
func TestSetTray(t *testing.T) {
	bin := t.TempDir()
	out := filepath.Join(bin, "args")
	script := "#!/bin/sh\necho \"$1\" >> " + out + "\n[ \"$1\" = --install ] || [ \"$1\" = --uninstall ]\n"
	require.NoError(t, os.WriteFile(filepath.Join(bin, "blitz-tray"), []byte(script), 0o755))
	t.Setenv("PATH", bin)
	t.Setenv("HOME", t.TempDir())
	orig := trayDirs
	trayDirs = func() []string { return nil } // never a real tray
	t.Cleanup(func() { trayDirs = orig })
	a := &App{}
	require.NoError(t, a.SetTray(true))
	require.NoError(t, a.SetTray(false))
	got, _ := os.ReadFile(out)
	assert.Equal(t, "--install\n--uninstall\n", string(got))

	require.NoError(t, os.WriteFile(filepath.Join(bin, "blitz-tray"), []byte("#!/bin/sh\necho no display >&2\nexit 1\n"), 0o755))
	err := a.SetTray(true)
	assert.ErrorContains(t, err, "no display")

	t.Setenv("PATH", t.TempDir())
	assert.ErrorContains(t, a.SetTray(true), "isn't installed")
}
