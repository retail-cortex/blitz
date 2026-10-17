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

package pagehost

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// winRegistry is a Windows machine's PATHs for a test: the system's and
// the user's, the folders that have blitz.exe, and what was written and
// announced.
type winRegistry struct {
	system, user string
	blitz        map[string]bool
	writes       []string
	announced    int
	readErr      error
	writeErr     error
}

func fakeWindows(t *testing.T, r *winRegistry) {
	t.Helper()
	oldRead, oldSys, oldWrite, oldChanged, oldHas := readUserPath, readSystemPath, writeUserPath, pathChanged, hasBlitzExe
	readUserPath = func() (string, error) { return r.user, r.readErr }
	readSystemPath = func() (string, error) { return r.system, nil }
	writeUserPath = func(v string) error {
		if r.writeErr != nil {
			return r.writeErr
		}
		r.writes = append(r.writes, v)
		r.user = v
		return nil
	}
	pathChanged = func() { r.announced++ }
	hasBlitzExe = func(dir string) bool { return r.blitz[dir] }
	t.Cleanup(func() {
		readUserPath, readSystemPath, writeUserPath, pathChanged, hasBlitzExe = oldRead, oldSys, oldWrite, oldChanged, oldHas
	})
}

const (
	ours   = `C:\Users\me\Blitz\resources\app\bin`
	ourCLI = ours + `\blitz.exe`
)

func TestWindowsCLIStatus(t *testing.T) {
	for _, tc := range []struct {
		name      string
		r         winRegistry
		onPath    string
		installed bool
	}{
		{"not on PATH", winRegistry{system: `C:\Windows`, user: `C:\tools`}, "", false},
		{"ours, on the user's PATH", winRegistry{user: `C:\tools;` + ours, blitz: map[string]bool{ours: true}}, ourCLI, true},
		{"ours, case and a trailing separator aside", winRegistry{user: `c:\users\me\blitz\resources\app\bin\`, blitz: map[string]bool{`c:\users\me\blitz\resources\app\bin\`: true}}, `c:\users\me\blitz\resources\app\bin\blitz.exe`, true},
		{"another, on the system's PATH first", winRegistry{system: `C:\Program Files\Blitz`, user: ours, blitz: map[string]bool{`C:\Program Files\Blitz`: true, ours: true}}, `C:\Program Files\Blitz\blitz.exe`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.r
			fakeWindows(t, &r)
			s := windowsCLIStatus(ourCLI)
			assert.Equal(t, "path", s.Method)
			assert.Equal(t, tc.onPath, s.OnPath)
			assert.Equal(t, tc.installed, s.Installed)
		})
	}
}

func TestWindowsInstallCLI(t *testing.T) {
	const (
		gone  = `D:\old\resources\app\bin`
		other = `E:\Blitz2\resources\app\bin`
	)
	for _, tc := range []struct {
		name      string
		again     bool
		r         winRegistry
		wantPath  string // "" when nothing is written
		removed   []string
		announced int
	}{
		{"first, ahead of the user's folders", false, winRegistry{user: `C:\tools;%USERPROFILE%\go\bin`}, ours + `;C:\tools;%USERPROFILE%\go\bin`, nil, 1},
		{"no PATH yet", false, winRegistry{}, ours, nil, 1},
		{"a blitz on PATH already: nothing", false, winRegistry{user: other, blitz: map[string]bool{other: true}}, "", nil, 0},
		{"again: other copies of Blitz come off", true, winRegistry{user: `C:\tools;` + gone + `;` + other, blitz: map[string]bool{other: true}}, ours + `;C:\tools`, []string{gone, other}, 1},
		{"again, ours already first: nothing", true, winRegistry{user: ours + `;C:\tools`, blitz: map[string]bool{ours: true}}, "", nil, 0},
		{"again, ours further down: first", true, winRegistry{user: `C:\tools;` + ours, blitz: map[string]bool{ours: true}}, ours + `;C:\tools`, nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.r
			fakeWindows(t, &r)
			got, err := windowsInstallCLI(ourCLI, tc.again)
			require.NoError(t, err)
			if tc.wantPath == "" {
				assert.Empty(t, r.writes)
			} else {
				assert.Equal(t, []string{tc.wantPath}, r.writes)
				assert.Equal(t, ourCLI, got.Link)
				assert.Equal(t, userPathKey, got.Profile)
			}
			assert.Equal(t, tc.removed, got.Removed)
			assert.Equal(t, tc.announced, r.announced)
		})
	}
}

func TestWindowsInstallCLIErrors(t *testing.T) {
	r := winRegistry{readErr: errors.New("no Environment key")}
	fakeWindows(t, &r)
	_, err := windowsInstallCLI(ourCLI, false)
	assert.ErrorContains(t, err, "no Environment key")

	r = winRegistry{writeErr: errors.New("access denied")}
	fakeWindows(t, &r)
	_, err = windowsInstallCLI(ourCLI, true)
	assert.ErrorContains(t, err, "access denied")
	assert.Zero(t, r.announced, "nothing to announce")
}

func TestWinPaths(t *testing.T) {
	t.Setenv("BLITZ_TEST_HOME", `C:\Users\me`)
	assert.Equal(t, []string{`C:\a`, `C:\b`}, splitWinPath(` C:\a ;;C:\b;`))
	assert.Equal(t, `C:\Users\me\go\bin`, expandWin(`%BLITZ_TEST_HOME%\go\bin`))
	assert.Equal(t, `%NO_SUCH_BLITZ_VAR%\x`, expandWin(`%NO_SUCH_BLITZ_VAR%\x`))
	assert.Equal(t, `50% off`, expandWin(`50% off`))
	assert.True(t, sameWinPath(`C:\A\B\`, `c:/a/b`))
	assert.False(t, sameWinPath(`C:\A\B`, `C:\A\C`))
	assert.Equal(t, `C:\a`, dirWin(`C:\a\blitz.exe`))
	assert.Equal(t, "blitz.exe", dirWin("blitz.exe"))
	assert.Equal(t, `C:\a\blitz.exe`, joinWin(`C:\a\`, "blitz.exe"))
	assert.True(t, blitzBinGone(`Z:\nowhere\resources\app\bin`))
	assert.False(t, blitzBinGone(`Z:\nowhere\tools`), "not Blitz's")
	assert.False(t, hasBlitzExe(t.TempDir()))
}

// Elsewhere than Windows there's no registry.
func TestNoRegistryElsewhere(t *testing.T) {
	_, err := registryUserPath()
	if err == nil {
		t.Skip("Windows")
	}
	assert.ErrorIs(t, err, errNotWindows)
	_, err = registrySystemPath()
	assert.ErrorIs(t, err, errNotWindows)
	assert.ErrorIs(t, registrySetUserPath("x"), errNotWindows)
	announcePathChange()
}
