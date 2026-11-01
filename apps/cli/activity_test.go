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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/activity"
)

// A command counts the CLI's use, once a day; --version and --help don't
// (spec_active_installs_054 AI-01, AI-03), and the CLI waits for the ping
// at most pingGrace.
func TestCommandsCountUse(t *testing.T) {
	var mu sync.Mutex
	var fronts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]string
		_ = json.NewDecoder(r.Body).Decode(&m)
		mu.Lock()
		fronts = append(fronts, m["front"])
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	saved := activeUse
	activeUse = &activity.Recorder{Dir: t.TempDir(), Version: "dev", URL: srv.URL}
	t.Cleanup(func() { activeUse, pinging = saved, nil })

	run := func(args ...string) {
		t.Helper()
		pinging = nil
		root := newRootCommand()
		root.SetArgs(args)
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		_ = root.Execute()
		began := time.Now()
		waitForPing()
		assert.Less(t, time.Since(began), pingGrace+500*time.Millisecond)
	}
	for _, args := range [][]string{{"--version"}, {"-v"}, {"--help"}, {"help"}, {"doctor", "--help"}} {
		run(args...)
		mu.Lock()
		require.Empty(t, fronts, "%v isn't use", args)
		mu.Unlock()
	}
	run("doctor")
	run("doctor")
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"cli"}, fronts, "once a day")
}

// The CLI waits for a slow ping at most pingGrace, then exits (AI-11).
func TestPingGrace(t *testing.T) {
	saved, savedGrace := pinging, pingGrace
	t.Cleanup(func() { pinging, pingGrace = saved, savedGrace })
	pingGrace = 20 * time.Millisecond
	pinging = make(chan struct{}) // never done
	began := time.Now()
	waitForPing()
	assert.Less(t, time.Since(began), time.Second)
}
