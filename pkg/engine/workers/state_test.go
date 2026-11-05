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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/flows"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreStatesAndPersistence(t *testing.T) {
	root := t.TempDir()
	dir := writeWorker(t, root, "deps", valid)
	w, _ := Load(dir, "")
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
	edited, _ := Load(dir, "")
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
	w, _ := Load(writeWorker(t, t.TempDir(), "deps", valid), "")
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
	w, _ := Load(writeWorker(t, t.TempDir(), "deps", valid), "")
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
`), "")
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

// A [workers.policy] duration that can't be read is never "no limit":
// the shipped 30m applies, as the default and as the cap, with a note
// (N26 engine-sub-2#7).
func TestApplyPolicyUnreadableDurations(t *testing.T) {
	tests := []struct {
		name, def, max string
		asked, want    time.Duration
		note           string
	}{
		{name: "default", def: "30 minutes", want: 30 * time.Minute, note: `workers.policy.default_timeout "30 minutes" can't be read`},
		{name: "negative default", def: "-5m", want: 30 * time.Minute, note: "default_timeout"},
		{name: "empty default", def: "", want: 30 * time.Minute},
		{name: "cap", def: "10m", max: "1h 30m", asked: 5 * time.Hour, want: 30 * time.Minute, note: `workers.policy.max_timeout "1h 30m" can't be read`},
		{name: "readable cap", def: "10m", max: "2h", asked: 5 * time.Hour, want: 2 * time.Hour, note: "timeout 5h0m0s capped at 2h0m0s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := config.DefaultConfig().Workers.Policy
			p.DefaultTimeout, p.MaxTimeout = tt.def, tt.max
			eff := Apply(&Worker{Limits: api.Limits{Timeout: tt.asked}}, p)
			assert.Equal(t, tt.want, eff.Limits.Timeout)
			if tt.note == "" {
				assert.Empty(t, eff.Notes)
				return
			}
			assert.True(t, slices.ContainsFunc(eff.Notes, func(n string) bool { return strings.Contains(n, tt.note) }), "notes %q", eff.Notes)
		})
	}
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

	// A change that can't be written isn't kept in memory either (N26
	// engine-sub-2#12).
	w, err := Load(writeWorker(t, t.TempDir(), "deps", valid), "")
	require.NoError(t, err)
	assert.Error(t, s.Enable("/ws", w, w.Hash))
	assert.Equal(t, api.StateNew, s.State("/ws", w, nil), "not enabled until the service restarts")
	assert.Empty(t, s.Known())
}

// A job that runs a workflow is hashed with the workflow's file, whose
// steps add to what the job may do (flows.SchedulePermissions): editing,
// adding or removing it suspends an enabled job as changed, until it's
// enabled again at the new hash (scan N26 desktop-4#19 follow-up).
func TestWorkflowChangeSuspendsJob(t *testing.T) {
	const job = "---\nschedule: hourly\nworkflow: digest\n---\n"
	const flow = "nodes:\n  - id: save\n    type: save_file\n    path: out.md\n"
	write := func(t *testing.T, dir, data string) {
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "digest"+flowSuffix), []byte(data), 0o644))
	}
	cases := []struct {
		name          string
		before, after func(t *testing.T, dir string)
	}{
		{"edited", func(t *testing.T, d string) { write(t, d, flow) }, func(t *testing.T, d string) {
			write(t, d, flow+"  - id: web\n    type: browser\n")
		}},
		{"created", func(*testing.T, string) {}, func(t *testing.T, d string) { write(t, d, flow) }},
		{"removed", func(t *testing.T, d string) { write(t, d, flow) }, func(t *testing.T, d string) {
			require.NoError(t, os.Remove(filepath.Join(d, "digest"+flowSuffix)))
		}},
		{"another file with its name in another case", func(t *testing.T, d string) { write(t, d, flow) }, func(t *testing.T, d string) {
			require.NoError(t, os.WriteFile(filepath.Join(d, "digest.FLOW.yaml"), []byte(flow+"  - id: web\n    type: browser\n"), 0o644))
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ws := t.TempDir()
			wf := filepath.Join(ws, ".agents", "workflows")
			c.before(t, wf)
			dir := writeWorker(t, filepath.Join(ws, ".agents", "workers"), "digest-job", job)
			s, err := OpenStore(filepath.Join(t.TempDir(), "workers.json"))
			require.NoError(t, err)
			w, err := Load(dir, wf)
			require.NoError(t, err)
			require.NoError(t, s.Enable(ws, w, w.Hash))
			require.Equal(t, api.StateEnabled, s.State(ws, w, nil))

			c.after(t, wf)
			now, err := Load(dir, wf)
			require.NoError(t, err)
			assert.NotEqual(t, w.Hash, now.Hash, "the workflow's change left the job's hash as it was")
			assert.Equal(t, api.StateChanged, s.State(ws, now, nil))
			hash, err := HashDir(dir, wf)
			require.NoError(t, err)
			assert.Equal(t, now.Hash, hash, "HashDir and Load disagree")
			_, read, err := ReadSpec(dir, wf)
			require.NoError(t, err)
			assert.Equal(t, now.Hash, read, "ReadSpec and Load disagree")
			found, err := Discover(wf, filepath.Dir(dir))
			require.NoError(t, err)
			require.Len(t, found, 1)
			assert.Equal(t, now.Hash, found[0].Worker.Hash, "Discover and Load disagree")

			assert.ErrorIs(t, s.Enable(ws, now, w.Hash), api.ErrHashMismatch, "enabled at the old hash")
			require.NoError(t, s.Enable(ws, now, now.Hash))
			assert.Equal(t, api.StateEnabled, s.State(ws, now, nil))
		})
	}
}

// A job without a workflow has the hash it had before workflows were
// hashed, so the upgrade doesn't suspend it; workflows beside it don't
// change it.
func TestHashWithoutWorkflowUnchanged(t *testing.T) {
	ws := t.TempDir()
	wf := filepath.Join(ws, ".agents", "workflows")
	require.NoError(t, os.MkdirAll(wf, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(wf, "deps"+flowSuffix), []byte("nodes: []\n"), 0o644))
	dir := writeWorker(t, filepath.Join(ws, ".agents", "workers"), "deps", valid)
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%s", FileName, len(valid), valid)))
	want := "sha256:" + hex.EncodeToString(sum[:])
	for name, flowsDir := range map[string]string{"no workflows folder": "", "with workflows": wf} {
		t.Run(name, func(t *testing.T) {
			w, err := Load(dir, flowsDir)
			require.NoError(t, err)
			assert.Equal(t, want, w.Hash)
		})
	}
}

// flowSuffix is the flows package's.
func TestFlowSuffix(t *testing.T) { assert.Equal(t, flows.Suffix, flowSuffix) }

// Load hands back the workflow's file as it hashed it, so a run takes
// the workflow's steps and grants from the bytes its enabled hash covers,
// not from another read (WK-05b, N26 engine-decisions).
func TestLoadKeepsTheWorkflowItHashed(t *testing.T) {
	const flow = "nodes:\n  - {id: save, type: save_file, path: out.md}\n"
	cases := []struct {
		name, job, flow string
		want            bool
	}{
		{name: "its workflow", job: "---\nschedule: hourly\nworkflow: digest\n---\n", flow: flow, want: true},
		{name: "its workflow is missing", job: "---\nschedule: hourly\nworkflow: digest\n---\n"},
		{name: "no workflow", job: valid, flow: flow},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ws := t.TempDir()
			wf := filepath.Join(ws, ".agents", "workflows")
			require.NoError(t, os.MkdirAll(wf, 0o755))
			path := filepath.Join(wf, "digest"+flowSuffix)
			if c.flow != "" {
				require.NoError(t, os.WriteFile(path, []byte(c.flow), 0o644))
			}
			w, err := Load(writeWorker(t, filepath.Join(ws, ".agents", "workers"), "digest", c.job), wf)
			require.NoError(t, err)
			if !c.want {
				assert.Nil(t, w.FlowFile)
				return
			}
			require.NotNil(t, w.FlowFile)
			assert.Equal(t, path, w.FlowFile.Path)
			assert.Equal(t, c.flow, string(w.FlowFile.Data))
			// The bytes kept are the bytes hashed: the file edited after
			// Load changes the hash, not what Load handed back.
			require.NoError(t, os.WriteFile(path, []byte(flow+"  - {id: web, type: browser}\n"), 0o644))
			assert.Equal(t, c.flow, string(w.FlowFile.Data))
			now, err := HashDir(w.Dir, wf)
			require.NoError(t, err)
			assert.NotEqual(t, w.Hash, now)
		})
	}
}
