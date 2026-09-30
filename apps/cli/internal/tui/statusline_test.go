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
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultStatusLine(t *testing.T) {
	tests := []struct {
		name string
		s    StatusState
		want string
	}{
		{"new session", StatusState{Model: "m", Mode: "default"}, "m · default"},
		{"context", StatusState{Model: "m", Mode: "plan", ContextTokens: 12000}, "m · plan · context 12.0k"},
		{"threshold and cost", StatusState{Model: "m", Mode: "default", ContextTokens: 50000, ContextPercent: 50, CostUSD: 0.126, Priced: true}, "m · default · context 50.0k (50%) · $0.13"},
		{"unpriced", StatusState{Model: "m", Mode: "default", CostUSD: 1, Priced: false}, "m · default"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, defaultStatusLine(tt.s))
		})
	}
}

func TestStatusLine(t *testing.T) {
	app := newTestApp(t, nil)
	app.Version = "1.2.3"
	ctx := context.Background()
	assert.Empty(t, statusLine(ctx, app), "none by default")

	app.StatusLine = "default"
	assert.Equal(t, "mock-a · default", statusLine(ctx, app))

	state := filepath.Join(t.TempDir(), "state.json")
	app.StatusLine = "cat > " + state + "; printf 'first \\033[31mline\\nsecond\\n'"
	assert.Equal(t, "first [31mline", statusLine(ctx, app), "the first line, without control characters")
	b, err := os.ReadFile(state)
	require.NoError(t, err)
	var s StatusState
	require.NoError(t, json.Unmarshal(b, &s))
	assert.Equal(t, "1.2.3", s.Version)
	assert.Equal(t, "mock-a", s.Model)
	assert.Equal(t, app.Workspace.Dir(), s.Workspace)

	app.StatusLine = "exit 3"
	assert.Contains(t, statusLine(ctx, app), "status line: exit status 3")

	old := statusLineTimeout
	statusLineTimeout = 50 * time.Millisecond
	defer func() { statusLineTimeout = old }()
	app.StatusLine = "sleep 5"
	start := time.Now()
	assert.Contains(t, statusLine(ctx, app), "status line:")
	assert.Less(t, time.Since(start), 3*time.Second, "the command wasn't stopped")
}

func TestREPLStatusLine(t *testing.T) {
	app := newTestApp(t, nil)
	app.StatusLine = "echo from the command"
	app.Input = NewLineReader(strings.NewReader("/exit\n"), io.Discard)
	out := captureStdout(t, func() { assert.NoError(t, RunREPL(context.Background(), app)) })
	assert.Contains(t, out, "from the command")
}
