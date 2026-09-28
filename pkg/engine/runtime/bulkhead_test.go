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

package runtime

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func TestBoundedRunnerCapsConcurrencyAndRunsEveryTask(t *testing.T) {
	var running, peak, ran atomic.Int32
	tasks := make([]func(context.Context), 20)
	for i := range tasks {
		tasks[i] = func(context.Context) {
			n := running.Add(1)
			for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
			}
			time.Sleep(10 * time.Millisecond)
			running.Add(-1)
			ran.Add(1)
		}
	}
	boundedRunner(3)(context.Background(), tasks)
	require.Equal(t, int32(20), ran.Load(), "ran %d of 20 tasks", ran.Load())
	p := peak.Load()
	require.Equal(t, int32(3), p, "peak concurrency %d, want 3", p)
}

// A task that fans out again (a sub-agent's tool calls) must not deadlock
// even when the limit is 1.
func TestBoundedRunnerNestedBatchesDoNotDeadlock(t *testing.T) {
	run := boundedRunner(1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		run(context.Background(), []func(context.Context){func(ctx context.Context) {
			run(ctx, []func(context.Context){func(context.Context) {}, func(context.Context) {}})
		}, func(context.Context) {}})
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("nested batch deadlocked")
	}
}

func parallelShellTurn(t *testing.T, maxParallel int) time.Duration {
	t.Helper()
	var calls []*genai.Part
	for range 4 {
		calls = append(calls, &genai.Part{FunctionCall: &genai.FunctionCall{Name: "run_shell_command", Args: map[string]any{"command": "sleep 0.3"}}})
	}
	// The OS sandbox is off: this measures scheduling, and on Linux each
	// sandboxed command first scans for blocked paths, which under -race
	// takes seconds and swamps the sleeps.
	f := newEngineWith(t, fixtureOpts{cfg: func(c *config.Config) { c.Tools.MaxParallel = maxParallel; c.Sandbox.Shell = "off" }},
		&genai.Content{Role: genai.RoleModel, Parts: calls}, textContent("done"))
	// Bypass mode needs the OS sandbox, which is off here: approve instead.
	f.tools.Hooks().SetApprover(func(context.Context, api.ApprovalRequest) (api.Decision, error) { return api.DecisionOnce, nil })
	start := time.Now()
	got, err := functionResponses(t, f.eng, "s", "run four sleeps")
	require.NoError(t, err)
	r := got["run_shell_command"]
	require.NotNil(t, r, "shell call failed: %v", r)
	require.False(t, r["error"] != nil && r["error"] != "", "shell call failed: %v", r)
	return time.Since(start)
}

func TestEngineCapsParallelToolCalls(t *testing.T) {
	d := parallelShellTurn(t, 1)
	require.GreaterOrEqual(t, d, 1100*time.Millisecond, "max_parallel=1 ran 4×0.3s sleeps in %v; they overlapped", d)
	d = parallelShellTurn(t, 0)
	require.LessOrEqual(t, d, 1100*time.Millisecond, "unlimited took %v; calls did not run in parallel", d)
}
