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

// A deleted worker is forgotten: it's new again if it comes back, and a
// workspace with no workers left isn't listed.
func TestStoreForget(t *testing.T) {
	w, _ := Load(writeWorker(t, t.TempDir(), "deps", valid))
	path := filepath.Join(t.TempDir(), "workers.json")
	s, err := OpenStore(path)
	require.NoError(t, err)
	require.NoError(t, s.Enable("/work", w, w.Hash))
	require.NoError(t, s.Forget("/work", "deps"))
	assert.Equal(t, api.StateNew, s.State("/work", w, nil))
	assert.Empty(t, s.Workspaces())
	again, err := OpenStore(path)
	require.NoError(t, err)
	assert.Equal(t, api.StateNew, again.State("/work", w, nil), "forgotten on disk too")
	assert.NoError(t, s.Forget("/work", "never-known"))
}

// Known lists every worker on record, enabled or not; forgetting a
// workspace drops all of its, on disk too.
func TestStoreKnownAndForgetWorkspace(t *testing.T) {
	w, _ := Load(writeWorker(t, t.TempDir(), "deps", valid))
	path := filepath.Join(t.TempDir(), "workers.json")
	s, err := OpenStore(path)
	require.NoError(t, err)
	require.NoError(t, s.Enable("/gone", w, w.Hash))
	require.NoError(t, s.Disable("/gone", "news"))
	require.NoError(t, s.Disable("/kept", "deps"))
	assert.Equal(t, map[string]map[string]bool{"/gone": {"deps": true, "news": false}, "/kept": {"deps": false}}, s.Known())

	require.NoError(t, s.ForgetWorkspace("/gone"))
	assert.Equal(t, map[string]map[string]bool{"/kept": {"deps": false}}, s.Known())
	assert.Empty(t, s.Workspaces())
	again, err := OpenStore(path)
	require.NoError(t, err)
	assert.Equal(t, s.Known(), again.Known(), "forgotten on disk too")
	assert.NoError(t, s.ForgetWorkspace("/never-known"))
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
	tests := []struct {
		name    string
		cap     func(p *config.WorkerPolicy)
		turns   int
		timeout time.Duration
		notes   int
	}{
		// The shipped policy has defaults, not caps (v0.8.4): a job may ask for more.
		{name: "the shipped policy", cap: func(*config.WorkerPolicy) {}, turns: 500, timeout: 5 * time.Hour, notes: 1},
		{name: "a host's caps", cap: func(p *config.WorkerPolicy) { p.MaxTurns, p.MaxTimeout = 200, "2h" }, turns: 200, timeout: 2 * time.Hour, notes: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := config.DefaultConfig().Workers.Policy
			p.Allow = []string{"shell", "write"}
			tt.cap(&p)
			eff := Apply(w, p)
			assert.Len(t, eff.Permissions, 1, "permissions %v", eff.Permissions)
			assert.Equal(t, "shell", eff.Permissions[0].Kind, "permissions %v", eff.Permissions)
			assert.Equal(t, tt.turns, eff.Limits.MaxTurns, "limits %+v", eff.Limits)
			assert.Equal(t, p.DefaultMaxCostUSD, eff.Limits.MaxCostUSD, "limits %+v", eff.Limits)
			assert.Equal(t, tt.timeout, eff.Limits.Timeout, "limits %+v", eff.Limits)
			assert.Len(t, eff.Notes, tt.notes, "notes %q", eff.Notes)
		})
	}
}

// TestApplyPolicyDefaultsAndCostCap checks that a worker without limits
// gets the policy's defaults and that its cost is capped.
func TestApplyPolicyDefaultsAndCostCap(t *testing.T) {
	p := config.DefaultConfig().Workers.Policy
	p.DefaultMaxTurns, p.DefaultTimeout = 7, "3m"
	eff := Apply(&Worker{}, p)
	assert.Equal(t, 7, eff.Limits.MaxTurns)
	assert.Equal(t, 3*time.Minute, eff.Limits.Timeout)

	p.MaxCostUSD = 1
	eff = Apply(&Worker{Limits: api.Limits{MaxCostUSD: 5}}, p)
	assert.InDelta(t, 1.0, eff.Limits.MaxCostUSD, 1e-9)
	assert.Contains(t, eff.Notes, "max_cost_usd 5.00 capped at 1.00")
}

// TestStoreFileErrors checks a store that can't be read, parsed or saved.
func TestStoreFileErrors(t *testing.T) {
	dir := t.TempDir()
	_, err := OpenStore(dir)
	assert.Error(t, err, "the store is a directory")

	bad := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(bad, []byte("{"), 0o600))
	_, err = OpenStore(bad)
	assert.ErrorContains(t, err, "bad.json")

	later := filepath.Join(dir, "later")
	s, err := OpenStore(filepath.Join(later, "workers.json"))
	require.NoError(t, err, "a store that doesn't exist yet is empty")
	require.NoError(t, os.WriteFile(later, nil, 0o600))
	assert.Error(t, s.Disable("/ws", "deps"), "the store's directory can't be created")

	ro := filepath.Join(dir, "ro")
	require.NoError(t, os.Mkdir(ro, 0o500))
	t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
	s, err = OpenStore(filepath.Join(ro, "workers.json"))
	require.NoError(t, err)
	err = s.Disable("/ws", "deps")
	if err == nil {
		t.Skip("running with privileges that ignore file modes")
	}
	assert.Error(t, err, "the store's directory is read-only")
}
