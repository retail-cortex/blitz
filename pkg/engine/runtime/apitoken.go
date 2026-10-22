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

package runtime

import (
	"context"
	"sync"

	adksession "google.golang.org/adk/v2/session"
)

// maxTokenSessions bounds how many sessions' tokens are kept (MDL-103).
const maxTokenSessions = 64

// sessionTokens keeps the api provider's token per session (MDL-103): the
// script runs when a session first needs one and again after it's
// dropped; concurrent calls in a session share one run; a failure keeps
// nothing. Tokens live in memory only.
type sessionTokens struct {
	fetch func(context.Context) (string, error)

	mu      sync.Mutex
	entries map[string]*tokenEntry
	clock   uint64 // counts uses, for dropping the least recently used
}

// tokenEntry is one session's token. Its mutex is held while the script
// runs, so the session's other calls wait for that run.
type tokenEntry struct {
	mu    sync.Mutex
	token string
	used  uint64
}

func newSessionTokens(fetch func(context.Context) (string, error)) *sessionTokens {
	return &sessionTokens{fetch: fetch, entries: map[string]*tokenEntry{}}
}

// Token is session's token: kept, or from running the script.
func (s *sessionTokens) Token(ctx context.Context, session string) (string, error) {
	e := s.entry(session)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.token != "" {
		return e.token, nil
	}
	token, err := s.fetch(ctx)
	if err != nil {
		return "", err
	}
	e.token = token
	return token, nil
}

// Invalidate drops session's token if it is still stale, the one a request
// failed with: a call that failed with an older token doesn't drop the new
// one another call has just fetched.
func (s *sessionTokens) Invalidate(session, stale string) {
	s.mu.Lock()
	e := s.entries[session]
	s.mu.Unlock()
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.token == stale {
		e.token = ""
	}
}

// entry is session's entry, made if needed and marked as just used; the
// least recently used is dropped beyond maxTokenSessions.
func (s *sessionTokens) entry(session string) *tokenEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clock++
	e := s.entries[session]
	if e == nil {
		e = &tokenEntry{}
		s.entries[session] = e
		if len(s.entries) > maxTokenSessions {
			s.dropOldest(session)
		}
	}
	e.used = s.clock
	return e
}

// dropOldest drops the least recently used entry other than keep.
func (s *sessionTokens) dropOldest(keep string) {
	oldest, at := "", ^uint64(0)
	for id, e := range s.entries {
		if id != keep && e.used < at {
			oldest, at = id, e.used
		}
	}
	delete(s.entries, oldest)
}

// sessionKey carries a call's session ID down to its HTTP requests.
type sessionKey struct{}

// adkSessionID is the ID of the ADK session a model call belongs to (the
// ADK passes the invocation to the model as its context), "" outside one.
func adkSessionID(ctx context.Context) string {
	if c, ok := ctx.(interface{ Session() adksession.Session }); ok && c.Session() != nil {
		return c.Session().ID()
	}
	return ""
}

// withSession is ctx carrying session for the api provider's requests.
func withSession(ctx context.Context, session string) context.Context {
	return context.WithValue(ctx, sessionKey{}, session)
}

// sessionOf is the session ctx carries ("" outside one).
func sessionOf(ctx context.Context) string {
	s, _ := ctx.Value(sessionKey{}).(string)
	return s
}
