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

package workers

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreStatesAndPersistence(t *testing.T) {
	root := t.TempDir()
	dir := writeWorker(t, root, "deps", valid)
	w, _ := Load(dir)
	path := filepath.Join(t.TempDir(), "workers.json")
	s, err := OpenStore(path)
	require.NoError(t, err)
	ws := "/work"
	got := s.State(ws, w, nil)
	assert.Equal(t, api.StateNew, got, "new: %s", got)
	err = s.Enable(ws, w, "sha256:reviewed-something-else")
	assert.ErrorIs(t, err, api.ErrHashMismatch, "stale hash: %v", err)
	require.NoError(t, s.Enable(ws, w, w.Hash))
	got = s.State(ws, w, nil)
	assert.Equal(t, api.StateEnabled, got, "enabled: %s %v", got, s.Workspaces())
	assert.Len(t, s.Workspaces(), 1, "enabled: %s %v", got, s.Workspaces())

	// Another process reads the same state.
	again, _ := OpenStore(path)
	got = again.State(ws, w, nil)
	assert.Equal(t, api.StateEnabled, got, "reloaded: %s", got)

	// Editing the worker suspends it until re-enabled.
	os.WriteFile(w.Path, []byte(valid+"\nAlso check tools.\n"), 0o644)
	edited, _ := Load(dir)
	got = s.State(ws, edited, nil)
	assert.Equal(t, api.StateChanged, got, "edited: %s", got)
	require.NoError(t, s.Disable(ws, "deps"))
	got = s.State(ws, edited, nil)
	assert.Equal(t, api.StateDisabled, got, "disabled: %s %v", got, s.Workspaces())
	assert.Len(t, s.Workspaces(), 0, "disabled: %s %v", got, s.Workspaces())
	got = s.State(ws, edited, errors.New("broken"))
	assert.Equal(t, api.StateInvalid, got, "invalid: %s", got)
}

func TestApplyPolicy(t *testing.T) {
	w, err := Load(writeWorker(t, t.TempDir(), "deps", `---
schedule: hourly
permissions: ["shell:go list -m -u all", "web:proxy.golang.org"]
limits: { max_turns: 500, timeout: 5h }
---
do it
`))
	require.NoError(t, err)
	p := config.DefaultConfig().Workers.Policy
	p.Allow = []string{"shell", "write"}
	eff := Apply(w, p)
	assert.Len(t, eff.Permissions, 1, "permissions %v", eff.Permissions)
	assert.Equal(t, "shell", eff.Permissions[0].Kind, "permissions %v", eff.Permissions)
	assert.Equal(t, p.MaxTurns, eff.Limits.MaxTurns, "limits %+v", eff.Limits)
	assert.Equal(t, p.DefaultMaxCostUSD, eff.Limits.MaxCostUSD, "limits %+v", eff.Limits)
	assert.Equal(t, 2*time.Hour, eff.Limits.Timeout, "limits %+v", eff.Limits)
	assert.Len(t, eff.Notes, 3, "notes %q", eff.Notes)
}
