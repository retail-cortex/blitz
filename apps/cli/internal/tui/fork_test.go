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

package tui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExportName(t *testing.T) {
	for _, tt := range []struct{ title, id, want string }{
		{"Fix the build!", "s1", "fix-the-build.md"},
		{"", "s2", "session-s2.md"},
		{"A very long title that goes on and on past fifty characters for sure", "s3", "a-very-long-title-that-goes-on-and-on-past-fifty-c.md"},
	} {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, exportName(tt.title, tt.id))
		})
	}
}

func TestForkAndExportCommands(t *testing.T) {
	app := newTestApp(t, nil)
	ctx := context.Background()
	out := captureStdout(t, func() { cmdFork(ctx, []string{"zero"}, app) })
	assert.Contains(t, out, "Usage: /fork")
	out = captureStdout(t, func() { cmdFork(ctx, nil, app) })
	assert.Contains(t, out, "✗", "no session yet")

	_, err := app.Workspace.NewSession()
	require.NoError(t, err)
	_, err = app.Workspace.RenameSession("Tidy up")
	require.NoError(t, err)
	out = captureStdout(t, func() { cmdExport(nil, app) })
	assert.Contains(t, out, "Wrote the session to")
	data, err := os.ReadFile(filepath.Join(app.Workspace.Dir(), "tidy-up.md"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "# Tidy up")

	out = captureStdout(t, func() { cmdFork(ctx, nil, app) })
	assert.Contains(t, out, "Now in a copy of the session")
}
