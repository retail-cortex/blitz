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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseVersion reads a release from an interpreter's output, with or
// without its patch number.
func TestParseVersion(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []int
		ok   bool
	}{
		{in: "Python 3.11.4", want: []int{3, 11, 4}, ok: true},
		{in: "3.13", want: []int{3, 13}, ok: true},
		{in: "Python ???", ok: false},
	} {
		t.Run(c.in, func(t *testing.T) {
			got, ok := parseVersion(c.in)
			assert.Equal(t, c.ok, ok)
			assert.Equal(t, c.want, got)
		})
	}
}

// uvInstall runs uv's install in the box, in directories it makes first,
// and reports how it ended.
func TestUVInstall(t *testing.T) {
	root := t.TempDir()
	uv := filepath.Join(root, "uv")
	script := "#!/bin/sh\ncase \"$1 $2\" in\n\"python dir\") echo " + root + "/py;;\n\"cache dir\") echo " + root + "/cache;;\nesac\n"
	require.NoError(t, os.WriteFile(uv, []byte(script), 0o755))
	for _, c := range []struct {
		name string
		box  recordingBox
		err  string
	}{
		{name: "installed"},
		{name: "box error", box: recordingBox{err: errors.New("no box")}, err: "uv python install: no box"},
		{name: "timed out", box: recordingBox{result: ScriptResult{TimedOut: true}}, err: "installing Python took longer than"},
		{name: "failed", box: recordingBox{result: ScriptResult{ExitCode: 2}, output: "no such version"}, err: "uv python install failed (exit 2): no such version"},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := uvInstall(context.Background(), &c.box, uv, "==3.13.*")
			if c.err != "" {
				assert.ErrorContains(t, err, c.err)
			} else {
				require.NoError(t, err)
			}
			require.Len(t, c.box.reqs, 1)
			req := c.box.reqs[0]
			assert.Equal(t, []string{uv, "python", "install", "--no-config", "==3.13.*"}, req.Argv)
			assert.Equal(t, []string{root + "/py", root + "/cache"}, req.Writable)
			assert.DirExists(t, req.Dir, "the directories exist before uv's first install")
			assert.DirExists(t, root+"/cache")
		})
	}
}
