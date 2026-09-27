package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/internal/app"
	"google.golang.org/genai"
)

func TestLimitErrorsExitWithThree(t *testing.T) {
	for _, err := range []error{
		app.ErrCostLimit, app.ErrTimeLimit, app.ErrMaxTurns,
		fmt.Errorf("%w ($0.50)", app.ErrCostLimit), fmt.Errorf("wrapped: %w", app.ErrTimeLimit),
	} {
		if got := exitCodeFor(err); got != exitMaxTurns {
			t.Errorf("exitCodeFor(%v) = %d, want %d", err, got, exitMaxTurns)
		}
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
		if _, err := runCLI(t, args...); exitCodeFor(err) != exitUsage {
			t.Errorf("%s: exit code %d (%v), want %d", name, exitCodeFor(err), err, exitUsage)
		}
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
	if exitCodeFor(err) != exitMaxTurns || !strings.Contains(fmt.Sprint(err), "cost limit") {
		t.Fatalf("expected a cost-limit exit, got %v", err)
	}
	var res runResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("json: %v %s", err, out.String())
	}
	if !res.IsError || res.ExitCode != exitMaxTurns || res.Usage.ModelCalls < 2 || res.Usage.ModelCalls > 3 {
		t.Errorf("result %+v", res)
	}
}

func TestOneShotStopsAtItsTimeout(t *testing.T) {
	e := testEnv(t, toolCall("run_shell_command", map[string]any{"command": "sleep 5"}), toolCall("run_shell_command", map[string]any{"command": "sleep 5"}))
	sess, _ := e.Storage().CreateSession("", "t", "blitz")
	start := time.Now()
	err := runOneShot(context.Background(), e, oneShotOptions{prompt: "wait", sessionID: sess.ID, format: formatJSON, timeout: 300 * time.Millisecond, stdout: &bytes.Buffer{}})
	if exitCodeFor(err) != exitMaxTurns || !strings.Contains(fmt.Sprint(err), "time limit") {
		t.Fatalf("expected a time-limit exit, got %v", err)
	}
	if d := time.Since(start); d > 4*time.Second {
		t.Errorf("took %s: the timeout didn't stop the running command", d)
	}
}
