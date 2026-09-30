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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseInterval(t *testing.T) {
	for _, tt := range []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{in: "10m", want: 10 * time.Minute},
		{in: "1h30m", want: 90 * time.Minute},
		{in: "5", want: 5 * time.Minute},
		{in: "30s", wantErr: true},
		{in: "soon", wantErr: true},
	} {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseInterval(tt.in)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLoopsSchedule(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	l := &loops{now: func() time.Time { return now }}
	a := l.add(10*time.Minute, "check the build")
	b := l.add(time.Hour, "summarise")
	assert.Nil(t, l.takeDue(), "nothing due yet")

	now = now.Add(10 * time.Minute)
	due := l.takeDue()
	require.NotNil(t, due)
	assert.Equal(t, a.n, due.n)
	assert.Nil(t, l.takeDue(), "rescheduled from now")
	assert.Equal(t, now.Add(10*time.Minute), l.all()[0].next)

	assert.True(t, l.stop(b.n))
	assert.False(t, l.stop(b.n))
	assert.Len(t, l.all(), 1)
}

func TestLoopsWaitCtx(t *testing.T) {
	now := time.Now()
	l := &loops{now: func() time.Time { return now }}
	ctx, stop := l.waitCtx(context.Background())
	stop()
	assert.NotErrorIs(t, context.Cause(ctx), errLoopDue, "no loops: never due")

	l.add(time.Minute, "x")
	now = now.Add(-time.Minute) // so it's due at once
	l.list[0].next = now
	ctx, stop = l.waitCtx(context.Background())
	defer stop()
	select {
	case <-ctx.Done():
		assert.True(t, errors.Is(context.Cause(ctx), errLoopDue))
	case <-time.After(5 * time.Second):
		t.Fatal("the due loop didn't interrupt")
	}
}

func TestLoopAndGoalCommands(t *testing.T) {
	app := newTestApp(t, nil)
	out := captureStdout(t, func() { cmdLoop(nil, app) })
	assert.Contains(t, out, "No loops")
	out = captureStdout(t, func() { cmdLoop([]string{"15m", "run", "the", "tests"}, app) })
	assert.Contains(t, out, "Loop 1 started: run the tests, every 15m0s.")
	out = captureStdout(t, func() { cmdLoop(nil, app) })
	assert.Contains(t, out, "1. every 15m0s: run the tests")
	out = captureStdout(t, func() { cmdLoop([]string{"30s", "x"}, app) })
	assert.Contains(t, out, "Usage: /loop")
	out = captureStdout(t, func() { cmdLoop([]string{"stop", "1"}, app) })
	assert.Contains(t, out, "Loop 1 stopped.")
	out = captureStdout(t, func() { cmdLoop([]string{"stop", "9"}, app) })
	assert.Contains(t, out, "Usage: /loop")

	out = captureStdout(t, func() { cmdGoal(nil, app) })
	assert.Contains(t, out, "No goal")
	_, err := app.Workspace.SetGoal("docs build")
	require.Error(t, err, "no session yet")
	_, err = app.Workspace.NewSession()
	require.NoError(t, err)
	_, err = app.Workspace.SetGoal("docs build")
	require.NoError(t, err)
	out = captureStdout(t, func() { cmdGoal(nil, app) })
	assert.Contains(t, out, "Goal: docs build (0/20 turns so far)")
	out = captureStdout(t, func() { cmdGoal([]string{"clear"}, app) })
	assert.Contains(t, out, "Goal cleared.")
	out = captureStdout(t, func() { cmdGoal([]string{"clear"}, app) })
	assert.Contains(t, out, "No goal")
}
