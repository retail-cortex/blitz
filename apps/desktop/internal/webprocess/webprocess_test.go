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

//go:build linux

package webprocess

import (
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GTK runs on the main thread: TestMain runs the tests beside its loop,
// and runs it again when Quit ends it, saying so on loopEnded. Without a
// display the tests skip.
func init() { runtime.LockOSThread() }

var (
	ui        bool
	loopEnded = make(chan struct{}, 1)
)

func TestMain(m *testing.M) {
	ui = initUI()
	if !ui {
		os.Exit(m.Run())
	}
	go func() { os.Exit(m.Run()) }()
	for {
		runMainLoop()
		loopEnded <- struct{}{}
	}
}

// A loaded web view's process is ended by WebKit, not crashed or left to
// exit on its own.
func TestEndWebProcesses(t *testing.T) {
	if !ui {
		t.Skip("no display for a web view")
	}
	openView("<p>Blitz</p>")
	require.Eventually(t, viewLoaded, 30*time.Second, 10*time.Millisecond, "the page loads")
	assert.Zero(t, viewEnded(), "running")
	endWebProcesses()
	require.Eventually(t, func() bool { return viewEnded() != 0 }, 10*time.Second, 10*time.Millisecond, "the process ends")
	assert.Equal(t, 1, viewEnded(), "ended by WebKit's API")
}

// Quit ends the web process, by WebKit's API, and then GTK's main loop,
// and leaves quitting to itself.
func TestQuit(t *testing.T) {
	if !ui {
		t.Skip("no display for a web view")
	}
	openView("<p>Blitz</p>")
	require.Eventually(t, viewLoaded, 30*time.Second, 10*time.Millisecond, "the page loads")
	assert.True(t, Quit(), "quitting is Quit's")
	select {
	case <-loopEnded:
	case <-time.After(10 * time.Second):
		require.Fail(t, "GTK's main loop didn't end")
	}
	require.Eventually(t, func() bool { return viewEnded() != 0 }, 10*time.Second, 10*time.Millisecond, "the process ends")
	assert.Equal(t, 1, viewEnded(), "ended by WebKit's API")
}
