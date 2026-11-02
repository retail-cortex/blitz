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

package enterprise

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/enterprise/enterprisetest"
)

// The service's jobs, as the person may see them (EN-42): listed, their
// runs, run now, switched off, and what a run left, a page at a time.
func TestServiceJobs(t *testing.T) {
	ctx := context.Background()
	_, s, c, _, _ := setup(t)
	_, err := c.Jobs(ctx)
	assert.ErrorIs(t, err, ErrNotSignedIn)
	_, err = c.Children(ctx, "urn:blitz:jobs")
	assert.ErrorIs(t, err, ErrNotSignedIn)
	_, err = c.OpenResource(ctx, "urn:blitz:jobs")
	assert.ErrorIs(t, err, ErrNotSignedIn)
	require.NoError(t, c.SignInBrowser(ctx, browse(t), nil))

	set := "urn:blitz:artifacts:w:j:r1"
	s.Mu.Lock()
	s.ServiceJobs = []enterprisetest.ServiceJob{{ID: "j1", URN: "urn:blitz:jobs:j1", Workspace: "plans", Name: "digest", Schedule: "Daily at 6 AM", Enabled: true}}
	s.ServiceRuns = map[string][]enterprisetest.ServiceRun{"j1": {{ID: "r1", Status: "succeeded", Trigger: "schedule", Branch: "blitz/jobs/digest/r1", Artifacts: set, CostUSD: 0.1}}}
	s.Files = map[string][]enterprisetest.File{set: {{URN: set + "/events.jsonl", Title: "events.jsonl"}, {URN: set + "/out/a.csv", Title: "out/a.csv"}, {URN: set + "/out/sales%20by%20week.csv", Title: "out/sales by week.csv", Size: 4}}}
	s.Contents = map[string]string{set + "/out/sales%20by%20week.csv": "w,s\n"}
	s.Mu.Unlock()

	list, err := c.Jobs(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "digest", list[0].Name)
	runs, err := c.JobRuns(ctx, "j1")
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "blitz/jobs/digest/r1", runs[0].Branch)
	assert.Equal(t, set, runs[0].Artifacts)

	files, err := c.Children(ctx, set)
	require.NoError(t, err)
	require.Len(t, files, 3, "every page")
	assert.Equal(t, "out/sales by week.csv", files[2].Title)
	body, err := c.OpenResource(ctx, files[2].URN)
	require.NoError(t, err)
	data, _ := io.ReadAll(body)
	body.Close()
	assert.Equal(t, "w,s\n", string(data))
	_, err = c.OpenResource(ctx, set+"/nope")
	var refused *Refusal
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, 404, refused.Status)
	_, err = c.Children(ctx, "urn:blitz:artifacts:w:j:r2")
	assert.Error(t, err)

	run, err := c.RunJobNow(ctx, "j1")
	require.NoError(t, err)
	assert.Equal(t, "running", run.Status)
	_, err = c.RunJobNow(ctx, "nope")
	assert.Error(t, err)
	require.NoError(t, c.SetJobEnabled(ctx, "j1", false))
	list, _ = c.Jobs(ctx)
	assert.False(t, list[0].Enabled)

	s.Mu.Lock()
	s.Going = []enterprisetest.GoingRun{{ServiceRun: enterprisetest.ServiceRun{ID: "r2", Status: "running"}, Job: "j1", JobName: "digest", Workspace: "plans"}}
	s.Mu.Unlock()
	going, err := c.GoingRuns(ctx)
	require.NoError(t, err)
	require.Len(t, going, 1)
	assert.Equal(t, "digest", going[0].JobName)
	assert.Equal(t, "j1", going[0].Job)

	wctx, cancel := context.WithCancel(ctx)
	heard := make(chan RunChange, 1)
	done := make(chan error, 1)
	go func() { done <- c.WatchRuns(wctx, func(ch RunChange) { heard <- ch }) }()
	s.RunChanges <- enterprisetest.RunChange{Run: "r2", Job: "j1", Workspace: "plans", Status: "succeeded"}
	select {
	case ch := <-heard:
		assert.Equal(t, RunChange{Run: "r2", Job: "j1", Workspace: "plans", Status: "succeeded"}, ch)
	case <-time.After(5 * time.Second):
		t.Fatal("no change")
	}
	cancel()
	assert.Error(t, <-done)

	s.Mu.Lock()
	s.JobsRefused = true
	s.Mu.Unlock()
	assert.Error(t, c.WatchRuns(ctx, func(RunChange) {}), "refused")
	_, err = c.Jobs(ctx)
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, "feature_off", refused.Code)
	_, err = c.JobRuns(ctx, "j1")
	assert.Error(t, err)

	s.Close()
	_, err = c.OpenResource(ctx, files[2].URN)
	assert.Error(t, err)
}
