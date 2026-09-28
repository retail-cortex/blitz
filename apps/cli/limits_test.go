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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func TestLimitErrorsExitWithThree(t *testing.T) {
	for _, err := range []error{
		api.ErrCostLimit, api.ErrTimeLimit, api.ErrMaxTurns,
		fmt.Errorf("%w ($0.50)", api.ErrCostLimit), fmt.Errorf("wrapped: %w", api.ErrTimeLimit),
	} {
		got := exitCodeFor(err)
		assert.Equal(t, exitMaxTurns, got, "exitCodeFor(%v) = %d, want %d", err, got, exitMaxTurns)
	}
}

func TestLimitFlagsNeedAOneShotPrompt(t *testing.T) {
	isolate(t)
	for name, args := range map[string][]string{
		"cost without prompt":    {"--max-cost-usd", "1", "-i"},
		"timeout without prompt": {"--timeout", "1m", "-i"},
		"turns without prompt":   {"--max-turns", "3", "-i"},
		"negative cost":          {"--max-cost-usd", "-1", "hi"},
		"negative timeout":       {"--timeout", "-1s", "hi"},
		"bad timeout":            {"--timeout", "soon", "hi"},
		"unknown mode":           {"--permission-mode", "yolo", "hi"},
		"unknown effort":         {"--effort", "extreme", "hi"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := runCLI(t, args...)
			assert.Equal(t, exitUsage, exitCodeFor(err), "%s: exit code %d (%v), want %d", name, exitCodeFor(err), err, exitUsage)
		})
	}
}

func loopReplies(n int) []*genai.Content {
	var out []*genai.Content
	for i := 0; i < n; i++ {
		out = append(out, toolCall("list_files", map[string]any{}))
	}
	return out
}

// Each mock call costs $0.0001125 at the built-in gemini-3.8-flash price,
// so a $0.0002 limit stops the run at its second call.
func TestOneShotStopsAtItsCostLimit(t *testing.T) {
	e := testEnv(t, loopReplies(6)...)
	sess, _ := e.Storage().CreateSession("", "t", "blitz")
	var out bytes.Buffer
	err := runOneShot(context.Background(), e, oneShotOptions{prompt: "loop", sessionID: sess.ID, format: formatJSON, maxCostUSD: 0.0002, stdout: &out})
	require.Equal(t, exitMaxTurns, exitCodeFor(err), "expected a cost-limit exit, got %v", err)
	require.Contains(t, fmt.Sprint(err), "cost limit", "expected a cost-limit exit, got %v", err)
	var res runResult
	require.NoError(t, json.Unmarshal(out.Bytes(), &res), "json: %v %s", err, out.String())
	assert.True(t, res.IsError, "result %+v", res)
	assert.Equal(t, exitMaxTurns, res.ExitCode, "result %+v", res)
	assert.GreaterOrEqual(t, res.Usage.ModelCalls, 2, "result %+v", res)
	assert.LessOrEqual(t, res.Usage.ModelCalls, 3, "result %+v", res)
}

func TestOneShotStopsAtItsTimeout(t *testing.T) {
	e := testEnv(t, toolCall("run_shell_command", map[string]any{"command": "sleep 5"}), toolCall("run_shell_command", map[string]any{"command": "sleep 5"}))
	sess, _ := e.Storage().CreateSession("", "t", "blitz")
	start := time.Now()
	err := runOneShot(context.Background(), e, oneShotOptions{prompt: "wait", sessionID: sess.ID, format: formatJSON, timeout: 300 * time.Millisecond, stdout: &bytes.Buffer{}})
	require.Equal(t, exitMaxTurns, exitCodeFor(err), "expected a time-limit exit, got %v", err)
	require.Contains(t, fmt.Sprint(err), "time limit", "expected a time-limit exit, got %v", err)
	d := time.Since(start)
	assert.LessOrEqual(t, d, 4*time.Second, "took %s: the timeout didn't stop the running command", d)
}
