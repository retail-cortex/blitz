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

// open_file shows a workspace file in the desktop's editor, at a line;
// without a window, or for what isn't a readable workspace file, it says
// why, and the agent gives the path instead (spec_060_blitz_tools DRV-31).
func TestOpenFile(t *testing.T) {
	cfg := testRegistryConfig(t)
	cfg.Sandbox.BlockedPaths = []string{"**/.env"}
	r, err := NewRegistry(cfg, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { r.Close() })
	dir := r.Workspace().Dir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "notes"), 0o755))
	writeFile(t, filepath.Join(dir, "notes", "plan.md"), "# Plan\n")
	writeFile(t, filepath.Join(dir, ".env"), "KEY=1\n")
	outside := filepath.Join(t.TempDir(), "elsewhere.md")
	writeFile(t, outside, "x")
	rt, ok := r.tools["open_file"].(runnerTool)
	require.True(t, ok)

	type opened struct {
		path string
		line int
	}
	var got []opened
	r.SetFileOpener(func(_ context.Context, p string, line int) error {
		got = append(got, opened{p, line})
		return nil
	})
	cases := []struct {
		name string
		args map[string]any
		want string // the path it opened ("": none)
		line int
		err  string
	}{
		{name: "a file at a line", args: map[string]any{"path": "notes/plan.md", "line": 3}, want: "notes/plan.md", line: 3},
		{name: "an absolute path inside", args: map[string]any{"path": filepath.Join(dir, "notes", "plan.md")}, want: "notes/plan.md"},
		{name: "no path", args: map[string]any{"path": " "}, err: "path is required"},
		{name: "missing", args: map[string]any{"path": "nope.md"}, err: "nope.md"},
		{name: "a folder", args: map[string]any{"path": "notes"}, err: "is a folder"},
		{name: "blocked", args: map[string]any{"path": ".env"}, err: ".env"},
		{name: "outside the workspace", args: map[string]any{"path": outside}, err: "elsewhere.md"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got = nil
			out := runTool(t, rt, c.args)
			if c.err != "" {
				assert.Contains(t, errOf(out), c.err)
				assert.Empty(t, got, "nothing opens")
				return
			}
			assert.Empty(t, errOf(out))
			assert.Equal(t, true, out["opened"])
			assert.Equal(t, []opened{{c.want, c.line}}, got)
		})
	}

	t.Run("no window", func(t *testing.T) {
		r.SetFileOpener(func(context.Context, string, int) error { return errors.New("no desktop window to browse in") })
		out := runTool(t, rt, map[string]any{"path": "notes/plan.md"})
		assert.Contains(t, errOf(out), "tell the user the file's path instead")
		assert.Equal(t, "notes/plan.md", out["path"])
	})
	t.Run("no opener", func(t *testing.T) {
		r.SetFileOpener(nil)
		out := runTool(t, rt, map[string]any{"path": "notes/plan.md"})
		assert.Contains(t, errOf(out), "no Blitz window is open")
	})
}
