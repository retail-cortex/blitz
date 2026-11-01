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
	"slices"
	"time"

	pings "github.com/retail-cortex/blitz/pkg/activity"
	"github.com/retail-cortex/blitz/pkg/socket"
)

// WithUse counts the front ends' use with r, once a day each
// (spec_active_installs_054 AI-02): nil, or without it, nothing is
// counted.
func WithUse(r *pings.Recorder) Option { return func(s *Server) { s.use = r } }

// personal are the calls only a person makes: a turn, a steer, a session
// started or opened, an approval or an answer. Polling, status and the
// scheduler's runs aren't use (AI-01).
var personal = map[string]bool{
	"/blitz.v1.SessionService/RunTurn":     true,
	"/blitz.v1.SessionService/Steer":       true,
	"/blitz.v1.SessionService/NewSession":  true,
	"/blitz.v1.SessionService/OpenSession": true,
	"/blitz.v1.SessionService/Approve":     true,
	"/blitz.v1.SessionService/Answer":      true,
}

// countUse marks the use of the front end a personal call names
// (socket.ClientHeader), before serving it; the ping goes in the
// background.
func (s *Server) countUse(h http.Handler) http.Handler {
	if s.use == nil {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if personal[r.URL.Path] {
			if front := r.Header.Get(socket.ClientHeader); slices.Contains(pings.Fronts, front) {
				s.use.Use(front)
			}
		}
		h.ServeHTTP(w, r)
	})
}

// RetryPings sends the pings that failed every interval until ctx ends
// (AI-12): a ping that waited out an outage goes within the hour.
func RetryPings(ctx context.Context, r *pings.Recorder, every time.Duration) {
	if r == nil {
		return
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = r.Flush(ctx)
		}
	}
}
