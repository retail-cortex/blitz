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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/stretchr/testify/assert"
)

// The service is idle between requests, counted from the last one, and
// never while one is in flight.
func TestIdleFor(t *testing.T) {
	s := New(func(context.Context, string) (*engine.Workspace, error) { return nil, nil })
	assert.Positive(t, s.IdleFor(), "idle since it started")

	var during time.Duration
	h := s.act.track(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { during = s.IdleFor() }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil))
	assert.Zero(t, during, "busy while serving a request")
	assert.Less(t, s.IdleFor(), time.Second, "counted from the last request")

	s.runs.list = append(s.runs.list, &bgRun{done: make(chan struct{})})
	assert.Zero(t, s.IdleFor(), "a background run going")
	close(s.runs.list[0].done)
	assert.Positive(t, s.IdleFor(), "the run ended")
}
