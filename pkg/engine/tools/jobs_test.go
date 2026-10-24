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
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeJobs is a workspace's jobs for the tools: what was created, and what
// Draft says is wrong.
func fakeJobs(dir string, problems []string, created *[]api.WorkerSpec) Jobs {
	return Jobs{
		Draft: func(spec api.WorkerSpec) (string, []byte, []string, error) {
			return filepath.Join(dir, ".agents", "workers", spec.Name), []byte("---\nschedule: " + spec.Schedule + "\n---\n" + spec.Prompt + "\n"), problems, nil
		},
		Preview: func(schedule, _ string) (api.SchedulePreview, error) {
			return api.SchedulePreview{Cron: "0 8 * * *", Next: []time.Time{time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC), time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC)}}, nil
		},
		Create: func(spec api.WorkerSpec) (api.WorkerInfo, []string, error) {
			*created = append(*created, spec)
			return api.WorkerInfo{Name: spec.Name, Schedule: spec.Schedule, Path: filepath.Join(dir, ".agents", "workers", spec.Name, "WORKER.md"), State: api.StateNew}, nil, nil
		},
		List: func() ([]api.WorkerInfo, error) {
			return []api.WorkerInfo{{Name: "nightly", Schedule: "daily at 2 AM", State: api.StateEnabled, Next: time.Date(2026, 10, 24, 2, 0, 0, 0, time.UTC), Permissions: []string{"shell:go test *"}}}, nil
		},
	}
}

// create_job: a job asked for in the chat, shown with its file and next
// runs, always asked about, created paused (spec_workers_023 WK-70).
func TestCreateJobTool(t *testing.T) {
	args := map[string]any{
		"name":        "ai-news",
		"schedule":    "every day at 8 am",
		"prompt":      "Fetch the latest AI news from news.google.com and write a summary to reports/ai-news/MM-DD-YYYY.html.",
		"permissions": []any{"web:news.google.com", " write:reports/ai-news/** ", ""},
		"max_turns":   80,
	}
	tests := []struct {
		name     string
		approve  bool
		off      bool
		problems []string
		change   func(*Jobs)
		wantErr  string
	}{
		{name: "approved", approve: true},
		{name: "refused", approve: false, wantErr: "declined"},
		{name: "not valid yet", approve: true, problems: []string{"schedule: can't read \"someday\""}, wantErr: "isn't valid yet"},
		{name: "jobs off", approve: true, off: true, wantErr: "jobs are off"},
		{name: "can't draft it", approve: true, change: func(j *Jobs) {
			j.Draft = func(api.WorkerSpec) (string, []byte, []string, error) {
				return "", nil, nil, errors.New("no workers folder")
			}
		}, wantErr: "no workers folder"},
		{name: "can't write it", approve: true, change: func(j *Jobs) {
			j.Create = func(api.WorkerSpec) (api.WorkerInfo, []string, error) {
				return api.WorkerInfo{}, nil, errors.New("disk full")
			}
		}, wantErr: "disk full"},
		{name: "not valid once written", approve: true, change: func(j *Jobs) {
			j.Create = func(api.WorkerSpec) (api.WorkerInfo, []string, error) {
				return api.WorkerInfo{}, []string{"name: taken"}, nil
			}
		}, wantErr: "isn't valid yet"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws, dir := newTestWorkspace(t)
			hooks, asked := approverHooks(tt.approve)
			r := &Registry{hooks: hooks, workspace: ws}
			var created []api.WorkerSpec
			if !tt.off {
				j := fakeJobs(dir, tt.problems, &created)
				if tt.change != nil {
					tt.change(&j)
				}
				r.SetJobs(j)
			}
			out := runTool(t, toolOf(t)(NewCreateJobTool(r)), args)
			if tt.wantErr != "" {
				assert.Contains(t, errOf(out), tt.wantErr)
				assert.Empty(t, created, "nothing created")
				if tt.problems != nil {
					assert.Equal(t, []any{tt.problems[0]}, out["problems"])
					assert.Empty(t, *asked, "an invalid job isn't asked about")
				}
				return
			}
			require.Empty(t, errOf(out))
			require.Len(t, *asked, 1)
			req := (*asked)[0]
			assert.True(t, req.MustAsk, "always asked, whatever the mode")
			assert.Equal(t, api.ActionWrite, req.Kind)
			assert.Contains(t, req.Detail, `Create the job "ai-news": every day at 8 am`)
			assert.Contains(t, req.Detail, "next: Sat 24 Oct 2026 08:00 UTC")
			assert.Contains(t, req.Detail, "starts paused")
			assert.Contains(t, req.Diff, "+schedule: every day at 8 am")
			require.Len(t, created, 1)
			assert.Equal(t, []string{"web:news.google.com", "write:reports/ai-news/**"}, created[0].Permissions, "trimmed, blanks dropped")
			assert.Equal(t, 80, created[0].Limits.MaxTurns)
			assert.Equal(t, true, out["paused"])
			assert.Equal(t, "ai-news", out["name"])
			assert.Len(t, out["next_runs"], 2)
		})
	}
}

// A job can't be made where no one can be asked (a job's own run).
func TestCreateJobToolUnattended(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	hooks, _ := approverHooks(true)
	r := &Registry{hooks: hooks, workspace: ws}
	var created []api.WorkerSpec
	r.SetJobs(fakeJobs(dir, nil, &created))
	ctx := Unattended(context.Background(), func(context.Context, api.ApprovalRequest) (api.Decision, error) { return api.DecisionOnce, nil })
	out := runToolCtx(t, ctx, toolOf(t)(NewCreateJobTool(r)), map[string]any{"name": "x", "schedule": "@daily", "prompt": "p"})
	assert.NotEmpty(t, errOf(out))
	assert.Empty(t, created)
}

func TestListJobsTool(t *testing.T) {
	r := &Registry{}
	out := runTool(t, toolOf(t)(NewListJobsTool(r)), map[string]any{})
	assert.Contains(t, errOf(out), "jobs are off")

	var created []api.WorkerSpec
	r.SetJobs(fakeJobs(t.TempDir(), nil, &created))
	out = runTool(t, toolOf(t)(NewListJobsTool(r)), map[string]any{})
	require.Empty(t, errOf(out))
	jobs := out["jobs"].([]any)
	require.Len(t, jobs, 1)
	j := jobs[0].(map[string]any)
	assert.Equal(t, "nightly", j["name"])
	assert.Equal(t, "enabled", j["state"])
	assert.Equal(t, "2026-10-24T02:00:00Z", j["next_run"])

	r.SetJobs(Jobs{List: func() ([]api.WorkerInfo, error) { return nil, errors.New("workers off") }})
	out = runTool(t, toolOf(t)(NewListJobsTool(r)), map[string]any{})
	assert.Contains(t, errOf(out), "workers off")
}

// install_secret_hook: asked about as a write showing the hook, then the
// hook is in the workspace's repository.
func TestInstallSecretHookTool(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	for _, approve := range []bool{true, false} {
		ws, dir := newTestWorkspace(t)
		init := exec.Command("git", "init", "-q")
		init.Dir = dir
		require.NoError(t, init.Run())
		hooks, asked := approverHooks(approve)
		r := &Registry{hooks: hooks, workspace: ws}
		out := runTool(t, toolOf(t)(NewInstallSecretHookTool(r)), map[string]any{})
		require.Len(t, *asked, 1)
		assert.Contains(t, (*asked)[0].Diff, "blitz security scan-secrets --staged")
		if !approve {
			assert.Contains(t, errOf(out), "declined")
			assert.NoFileExists(t, filepath.Join(dir, ".githooks", "pre-commit"))
			continue
		}
		require.Empty(t, errOf(out))
		assert.Equal(t, "installed", out["status"])
		assert.FileExists(t, filepath.Join(dir, ".githooks", "pre-commit"))
	}

	t.Run("where the hook can't go", func(t *testing.T) {
		ws, dir := newTestWorkspace(t)
		init := exec.Command("git", "init", "-q")
		init.Dir = dir
		require.NoError(t, init.Run())
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".githooks"), nil, 0o644))
		hooks, _ := approverHooks(true)
		out := runTool(t, toolOf(t)(NewInstallSecretHookTool(&Registry{hooks: hooks, workspace: ws})), map[string]any{})
		assert.Contains(t, errOf(out), ".githooks")
	})
}
