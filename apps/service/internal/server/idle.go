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
	"net/http"
	"sync"
	"time"
)

// activity is when the service last served a request, and how many it's
// serving now (a turn's stream lasts as long as the turn).
type activity struct {
	mu       sync.Mutex
	inflight int
	last     time.Time
}

// track counts h's requests as activity.
func (a *activity) track(h http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.inflight++
		a.mu.Unlock()
		defer func() {
			a.mu.Lock()
			a.inflight--
			a.last = time.Now()
			a.mu.Unlock()
		}()
		h.ServeHTTP(rw, r)
	})
}

// IdleFor is how long the service has had nothing to do: no request in
// flight, no turn running in any workspace and no background run going;
// 0 while it has.
func (s *Server) IdleFor() time.Duration {
	s.act.mu.Lock()
	inflight, last := s.act.inflight, s.act.last
	s.act.mu.Unlock()
	if inflight > 0 {
		return 0
	}
	s.runs.mu.Lock()
	for _, r := range s.runs.list {
		select {
		case <-r.done:
		default:
			s.runs.mu.Unlock()
			return 0
		}
	}
	s.runs.mu.Unlock()
	s.mu.Lock()
	for _, w := range s.workspaces {
		if w.Busy() {
			s.mu.Unlock()
			return 0
		}
	}
	s.mu.Unlock()
	return time.Since(last)
}
