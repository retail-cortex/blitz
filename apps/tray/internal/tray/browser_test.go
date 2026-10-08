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

package tray

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The page opens in Edge as an app on Windows, wherever Edge is, else in
// the default browser.
func TestAppWindow(t *testing.T) {
	const link = "http://127.0.0.1:5000/open?t=x"
	env := map[string]string{"ProgramFiles(x86)": "/pf86", "ProgramFiles": "/pf", "LOCALAPPDATA": "/local"}
	edge := func(root string) string { return filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe") }
	for _, tc := range []struct {
		name  string
		goos  string
		files []string
		want  []string
	}{
		{"Edge for every user", "windows", []string{edge("/pf86"), edge("/local")}, []string{edge("/pf86"), "--app=" + link}},
		{"Edge for this user", "windows", []string{edge("/local")}, []string{edge("/local"), "--app=" + link}},
		{"no Edge", "windows", nil, []string{"rundll32", "url.dll,FileProtocolHandler", link}},
		{"macOS", "darwin", nil, []string{"open", link}},
		{"Linux", "linux", nil, []string{"xdg-open", link}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isFile := func(p string) bool {
				for _, f := range tc.files {
					if f == p {
						return true
					}
				}
				return false
			}
			name, args := appWindow(tc.goos, func(k string) string { return env[k] }, isFile, link)
			assert.Equal(t, tc.want, append([]string{name}, args...))
		})
	}
}

// With a Page, Open Blitz opens its link in an app window; a page that
// can't be served is reported.
func TestOpenAppPage(t *testing.T) {
	f, a := newFake(t)
	a.Page = func() (string, error) { return "http://127.0.0.1:1/open?t=x", nil }
	require.NoError(t, a.OpenApp())
	require.Len(t, f.ran, 1)
	assert.Contains(t, f.ran[0], "http://127.0.0.1:1/open?t=x")

	a.Page = func() (string, error) { return "", assert.AnError }
	assert.ErrorIs(t, a.OpenApp(), assert.AnError)
}
