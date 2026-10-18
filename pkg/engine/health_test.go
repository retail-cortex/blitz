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

package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/health"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
)

const goodVerdict = `{"scores": [{"line": "clear", "score": 5, "reason": "Clear."}], "overall": 4.6, "summary": "Good.", "fixes": ["Say when it's done."]}`

// The dashboard: the workspace's items and their facts; a check judges
// each due item on the judge model and keeps the verdicts; the next judges
// only what changed, force all; a file that doesn't load isn't judged; a
// check can't overlap another.
func TestHealthCheck(t *testing.T) {
	w, _ := openTestWith(t, nil)
	judge := runtime.NewMockLLM("judge", text(goodVerdict), text("```json\n"+goodVerdict+"\n```"), text("not a verdict"), text(goodVerdict), text(goodVerdict), text(goodVerdict))
	var asked []string
	w.newModel = func(_ context.Context, _ *config.Config, ref string) (model.LLM, error) {
		asked = append(asked, ref)
		return judge, nil
	}
	w.cfg.Health.Model = "gemini/judge"
	agentPath := filepath.Join(w.ProjectAgentsDir(), "writer.md")
	require.NoError(t, os.MkdirAll(w.ProjectAgentsDir(), 0o755))
	require.NoError(t, os.WriteFile(agentPath, []byte("---\nname: writer\ndescription: Writes briefs\ntools: [read_file]\n---\nWrite a brief.\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(w.ProjectAgentsDir(), "bad.md"), []byte("no frontmatter"), 0o644))
	writeFlow(t, w, "brief", "nodes:\n  - {id: a, type: accumulate}\n")
	writeRule(t, w, "short", "---\nwhen: after_agent\naction: warn\n---\nOne line.\n")

	r := w.Health(context.Background())
	keys := map[string]health.Item{}
	for _, it := range r.Items {
		keys[it.Key()] = it
	}
	require.Contains(t, keys, "agent/writer")
	assert.Equal(t, []string{health.FlagUncovered}, keys["agent/writer"].Flags)
	assert.NotEmpty(t, keys["agent/bad"].Problem)
	assert.Equal(t, []string{health.FlagNeverRun}, keys["workflow/brief"].Flags)
	require.Contains(t, keys, "rule/short")
	assert.Equal(t, 1.0, r.Overall, "nothing judged: the file that doesn't load counts as a D")
	assert.Equal(t, "gemini/judge", r.Model)
	assert.True(t, r.Next.IsZero(), "the schedule waits for a first check")
	assert.Len(t, r.Days, health.Days)

	scope := HealthCheck{Items: []string{"agent/writer", "workflow/brief", "rule/short", "agent/bad"}}
	n, _, _ := w.EstimateHealthCheck(context.Background(), scope)
	assert.Equal(t, 3, n, "the bad file isn't judged")

	var progress []HealthProgress
	judged, _, err := w.CheckHealth(context.Background(), scope, func(p HealthProgress) { progress = append(progress, p) })
	require.NoError(t, err)
	assert.Equal(t, 2, judged, "one answer wasn't a verdict")
	require.Len(t, progress, 3)
	assert.Equal(t, 3, progress[2].Total)
	assert.NotEmpty(t, progress[2].Error)
	assert.Equal(t, []string{"gemini/judge"}, asked)
	assert.Contains(t, judge.Requests[0].Contents[0].Parts[0].Text, "Write a brief.", "the file")
	assert.Contains(t, judge.Requests[0].Contents[0].Parts[0].Text, "no eval checks it", "the evidence")

	r = w.Health(context.Background())
	v := r.Verdicts["agent/writer"]
	assert.Equal(t, 4.6, v.Overall)
	assert.Equal(t, "gemini/judge", v.Model)
	assert.Equal(t, []string{"Say when it's done."}, v.Fixes)
	assert.InDelta(t, 4.6, r.ByKind[health.KindAgent]*0+v.Overall, 1e-9)
	assert.Greater(t, r.Overall, 0.0)
	assert.False(t, r.State.LastCheck.IsZero())

	n, _, _ = w.EstimateHealthCheck(context.Background(), scope)
	assert.Equal(t, 1, n, "only the one without a verdict")
	require.NoError(t, os.WriteFile(agentPath, []byte("---\nname: writer\ndescription: Writes briefs\ntools: [read_file]\n---\nWrite a one-page brief.\n"), 0o644))
	n, _, _ = w.EstimateHealthCheck(context.Background(), scope)
	assert.Equal(t, 2, n, "and the changed one")
	n, _, _ = w.EstimateHealthCheck(context.Background(), HealthCheck{Items: scope.Items, Force: true})
	assert.Equal(t, 3, n)
	n, _, _ = w.EstimateHealthCheck(context.Background(), HealthCheck{Kinds: []string{health.KindRule}, Force: true})
	assert.Equal(t, 1, n, "one kind")
	judged, _, err = w.CheckHealth(context.Background(), scope, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, judged)
	assert.Len(t, w.Health(context.Background()).History["agent/writer"], 2, "its scores over time")

	w.healthMu.Lock()
	_, _, err = w.CheckHealth(context.Background(), scope, nil)
	w.healthMu.Unlock()
	assert.ErrorIs(t, err, ErrHealthCheckRunning)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = w.CheckHealth(ctx, HealthCheck{Items: scope.Items, Force: true}, nil)
	assert.ErrorIs(t, err, context.Canceled)
}

// The schedule: due once the last check is older than its next time, not
// before a first check, never when off; a scheduled check over the cost
// limit doesn't run, and says why.
func TestHealthSchedule(t *testing.T) {
	w, _ := openTestWith(t, nil)
	judge := runtime.NewMockLLM("judge", text(goodVerdict), text(goodVerdict), text(goodVerdict), text(goodVerdict), text(goodVerdict), text(goodVerdict))
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return judge, nil }
	require.NoError(t, os.MkdirAll(w.ProjectAgentsDir(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.ProjectAgentsDir(), "writer.md"), []byte("---\nname: writer\ndescription: Writes\ntools: [read_file]\n---\nWrite.\n"), 0o644))
	w.cfg.Health.Schedule = "@every 1h"
	now := time.Now()
	assert.False(t, w.HealthDue(now), "no first check yet")
	store := w.healthStore()
	require.NoError(t, store.SetState(health.State{LastCheck: now.Add(-2 * time.Hour)}))
	assert.True(t, w.HealthDue(now))
	w.cfg.Health.Schedule = "off"
	assert.False(t, w.HealthDue(now))
	w.cfg.Health.Schedule = "nonsense schedule"
	assert.False(t, w.HealthDue(now))
	w.cfg.Health.Schedule = "@every 1h"

	w.cfg.Health.MaxCostUSD = 1e-12
	require.NoError(t, w.ScheduledHealthCheck(context.Background()))
	st := store.State()
	assert.Contains(t, st.Skipped, "over the $0.00 limit")
	assert.Empty(t, judge.Requests, "nothing judged")
	assert.False(t, w.HealthDue(now), "not again until its next time")

	w.cfg.Health.MaxCostUSD = 0 // no limit
	require.NoError(t, store.SetState(health.State{LastCheck: now.Add(-2 * time.Hour)}))
	due, _, _ := w.EstimateHealthCheck(context.Background(), HealthCheck{})
	require.NoError(t, w.ScheduledHealthCheck(context.Background()))
	assert.Len(t, judge.Requests, due, "the agent and the built-in guardrails")
	assert.Empty(t, store.State().Skipped)
}
