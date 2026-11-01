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

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	pings "github.com/retail-cortex/blitz/pkg/activity"
	"github.com/retail-cortex/blitz/pkg/socket"
)

// A personal call from a named front end counts its use; one without the
// name, polling, or a front end that isn't one don't (spec_active_installs_054
// AI-01, AI-02).
func TestCountUse(t *testing.T) {
	var mu sync.Mutex
	var fronts []string
	retail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]string
		_ = json.NewDecoder(r.Body).Decode(&m)
		mu.Lock()
		fronts = append(fronts, m["front"])
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(retail.Close)
	use := &pings.Recorder{Dir: t.TempDir(), Version: "dev", URL: retail.URL}
	s := New(nil, WithUse(use))
	h := s.Handler()
	call := func(path, front string) {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		if front != "" {
			req.Header.Set(socket.ClientHeader, front)
		}
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	call("/blitz.v1.SessionService/GetActiveSession", "vscode")
	call("/blitz.v1.SessionService/RunTurn", "")
	call("/blitz.v1.SessionService/RunTurn", "tray")
	call("/blitz.v1.SessionService/RunTurn", "vscode")
	call("/blitz.v1.SessionService/Steer", "vscode")
	call("/blitz.v1.SessionService/NewSession", "cli")
	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(fronts) == 2
	}, 5*time.Second, 20*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	assert.ElementsMatch(t, []string{"vscode", "cli"}, fronts, "a turn from VS Code, once; a session from the CLI")
}

// RetryPings sends what failed at each interval, until its context ends
// (AI-12).
func TestRetryPings(t *testing.T) {
	var mu sync.Mutex
	status := http.StatusServiceUnavailable
	sent := 0
	retail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		sent++
		w.WriteHeader(status)
	}))
	t.Cleanup(retail.Close)
	use := &pings.Recorder{Dir: t.TempDir(), Version: "dev", URL: retail.URL}
	<-use.Use(pings.CLI)
	mu.Lock()
	status = http.StatusNoContent
	mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { RetryPings(ctx, use, 20*time.Millisecond); close(done) }()
	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return sent == 2
	}, 5*time.Second, 10*time.Millisecond, "sent again once the server answers")
	cancel()
	<-done
	RetryPings(context.Background(), nil, time.Millisecond) // nothing to retry: returns
}
