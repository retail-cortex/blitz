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
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func startSleep(t *testing.T, pm *tools.ProcessManager, secs string) {
	t.Helper()
	_, err := pm.Start("", "sleep "+secs, t.TempDir())
	require.NoError(t, err)
}

func newPM(t *testing.T) *tools.ProcessManager {
	pm := tools.NewProcessManager(0, 0)
	t.Cleanup(pm.Shutdown)
	return pm
}

func input(s string) *LineReader { return NewLineReader(strings.NewReader(s), io.Discard) }

var interactive = ExitPrompt{CanPrompt: true, AllowCancel: true}

func TestConfirmExitNoProcesses(t *testing.T) {
	ctx := context.Background()
	assert.True(t, ConfirmExit(ctx, input(""), nil, nil, interactive), "nil manager should allow exit")
	assert.True(t, ConfirmExit(ctx, input(""), newPM(t), nil, interactive), "no running processes should allow exit without prompting")
}

func TestConfirmExitKill(t *testing.T) {
	pm := newPM(t)
	startSleep(t, pm, "30")
	require.True(t, ConfirmExit(context.Background(), input("k\n"), pm, nil, interactive), "kill should exit")
	n := len(pm.Running())
	assert.Equal(t, 0, n, "%d processes still running after kill", n)
}

func TestConfirmExitWait(t *testing.T) {
	pm := newPM(t)
	startSleep(t, pm, "0.3")
	start := time.Now()
	require.True(t, ConfirmExit(context.Background(), input("w\n"), pm, nil, interactive), "wait should exit")
	assert.GreaterOrEqual(t, time.Since(start), 200*time.Millisecond, "returned before the process finished")
	list := pm.List()
	assert.Len(t, list, 1, "process should have finished normally: %+v", list)
	assert.False(t, list[0].Running, "process should have finished normally: %+v", list)
	assert.Equal(t, 0, list[0].ExitCode, "process should have finished normally: %+v", list)
}

func TestConfirmExitCancel(t *testing.T) {
	pm := newPM(t)
	startSleep(t, pm, "30")
	require.False(t, ConfirmExit(context.Background(), input("c\n"), pm, nil, interactive), "cancel should not exit")
	assert.Len(t, pm.Running(), 1, "cancel must leave processes running")
	// Without AllowCancel (one-shot mode), anything but wait kills.
	require.True(t, ConfirmExit(context.Background(), input("c\n"), pm, nil, ExitPrompt{CanPrompt: true}), "expected exit when cancel is not offered")
	assert.Len(t, pm.Running(), 0, "processes should be killed")
}

func TestConfirmExitForceQuit(t *testing.T) {
	// EOF at the prompt force-quits.
	pm := newPM(t)
	startSleep(t, pm, "30")
	assert.True(t, ConfirmExit(context.Background(), input(""), pm, nil, interactive), "EOF should force quit and kill")
	assert.Len(t, pm.Running(), 0, "EOF should force quit and kill")

	// Cannot prompt (non-interactive): kill without reading input.
	pm = newPM(t)
	startSleep(t, pm, "30")
	assert.True(t, ConfirmExit(context.Background(), input("c\n"), pm, nil, ExitPrompt{}), "non-interactive exit should kill")
	assert.Len(t, pm.Running(), 0, "non-interactive exit should kill")

	// Second Ctrl+C at the prompt force-quits.
	pm = newPM(t)
	startSleep(t, pm, "30")
	pr, pw := io.Pipe()
	defer pw.Close()
	sigs := make(chan os.Signal, 1)
	sigs <- os.Interrupt
	done := make(chan bool, 1)
	go func() {
		done <- ConfirmExit(context.Background(), NewLineReader(pr, io.Discard), pm, sigs, interactive)
	}()
	select {
	case ok := <-done:
		assert.True(t, ok, "Ctrl+C at prompt should kill and exit")
		assert.Len(t, pm.Running(), 0, "Ctrl+C at prompt should kill and exit")
	case <-time.After(5 * time.Second):
		t.Fatal("Ctrl+C at prompt did not force quit")
	}

	// Ctrl+C while waiting force-quits.
	pm = newPM(t)
	startSleep(t, pm, "30")
	sigs = make(chan os.Signal, 1)
	go func() { time.Sleep(300 * time.Millisecond); sigs <- os.Interrupt }()
	start := time.Now()
	require.True(t, ConfirmExit(context.Background(), input("w\n"), pm, sigs, interactive), "expected exit")
	assert.LessOrEqual(t, time.Since(start), 5*time.Second, "Ctrl+C while waiting should kill promptly")
	assert.Len(t, pm.Running(), 0, "Ctrl+C while waiting should kill promptly")
}

func TestREPLExitWithBackgroundProcesses(t *testing.T) {
	app := newTestApp(t, nil)
	pm := local(app).Tools().Processes()
	startSleep(t, pm, "30")

	// /exit -> cancel -> keep working -> /exit -> kill.
	app.Input = input("/exit\nc\n/exit\nk\n")
	done := make(chan error, 1)
	go func() { done <- RunREPL(context.Background(), app) }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("REPL did not exit")
	}
	assert.Len(t, pm.Running(), 0, "background process survived REPL exit")
}

func TestREPLCtrlCAtPromptWithBackgroundProcess(t *testing.T) {
	app := newTestApp(t, nil)
	pm := local(app).Tools().Processes()
	startSleep(t, pm, "30")
	pr, pw := io.Pipe()
	app.Input = NewLineReader(pr, io.Discard)
	sigs := make(chan os.Signal, 1)
	app.Interrupts = sigs

	done := make(chan error, 1)
	go func() { done <- RunREPL(context.Background(), app) }()
	time.Sleep(100 * time.Millisecond)
	sigs <- os.Interrupt // first Ctrl+C: warn and prompt instead of exiting
	time.Sleep(100 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("REPL exited on the first Ctrl+C despite running background processes")
	default:
	}
	go pw.Write([]byte("k\n"))
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("REPL did not exit after choosing kill")
	}
	pw.Close()
	assert.Len(t, pm.Running(), 0, "background process survived")
}
