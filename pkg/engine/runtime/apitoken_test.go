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
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingFetch hands out tok-1, tok-2, … and counts its runs.
func countingFetch() (func(context.Context) (string, error), *atomic.Int32) {
	var n atomic.Int32
	return func(context.Context) (string, error) {
		return fmt.Sprintf("tok-%d", n.Add(1)), nil
	}, &n
}

// A session keeps its token; a new session runs the script afresh; a
// dropped token is fetched again (MDL-103).
func TestSessionTokens(t *testing.T) {
	fetch, runs := countingFetch()
	s := newSessionTokens(fetch)
	ctx := context.Background()

	a1, err := s.Token(ctx, "a")
	require.NoError(t, err)
	a2, _ := s.Token(ctx, "a")
	assert.Equal(t, "tok-1", a1)
	assert.Equal(t, a1, a2, "kept for the session")

	b, _ := s.Token(ctx, "b")
	assert.Equal(t, "tok-2", b, "a new session runs the script")

	s.Invalidate("a", "tok-1")
	a3, _ := s.Token(ctx, "a")
	assert.Equal(t, "tok-3", a3, "dropped, so fetched again")
	assert.Equal(t, int32(3), runs.Load())
}

// Invalidating with an old token leaves a newer one alone, and an unknown
// session is no matter.
func TestSessionTokensStaleInvalidate(t *testing.T) {
	fetch, _ := countingFetch()
	s := newSessionTokens(fetch)
	ctx := context.Background()
	_, _ = s.Token(ctx, "a")
	s.Invalidate("a", "tok-1")
	fresh, _ := s.Token(ctx, "a")
	s.Invalidate("a", "tok-1") // a slower call that also failed with tok-1
	again, _ := s.Token(ctx, "a")
	assert.Equal(t, fresh, again, "a stale failure dropped the fresh token")
	s.Invalidate("nobody", "x")
}

// A failure keeps nothing: the next call runs the script again.
func TestSessionTokensFailure(t *testing.T) {
	var calls int
	s := newSessionTokens(func(context.Context) (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("no network")
		}
		return "tok", nil
	})
	_, err := s.Token(context.Background(), "a")
	assert.ErrorContains(t, err, "no network")
	got, err := s.Token(context.Background(), "a")
	require.NoError(t, err)
	assert.Equal(t, "tok", got)
}

// Concurrent calls in one session share one run of the script.
func TestSessionTokensConcurrent(t *testing.T) {
	var runs atomic.Int32
	s := newSessionTokens(func(context.Context) (string, error) {
		runs.Add(1)
		time.Sleep(20 * time.Millisecond)
		return "tok", nil
	})
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := s.Token(context.Background(), "a")
			assert.NoError(t, err)
			assert.Equal(t, "tok", got)
		}()
	}
	wg.Wait()
	assert.Equal(t, int32(1), runs.Load())
}

// Beyond maxTokenSessions the least recently used session is dropped.
func TestSessionTokensBounded(t *testing.T) {
	fetch, runs := countingFetch()
	s := newSessionTokens(fetch)
	ctx := context.Background()
	for i := range maxTokenSessions {
		_, _ = s.Token(ctx, fmt.Sprint(i))
	}
	_, _ = s.Token(ctx, "0") // 0 is now the most recent; 1 the oldest
	_, _ = s.Token(ctx, "new")
	assert.Len(t, s.entries, maxTokenSessions)
	assert.NotContains(t, s.entries, "1", "the least recently used stayed")
	assert.Contains(t, s.entries, "0")
	before := runs.Load()
	_, _ = s.Token(ctx, "0")
	assert.Equal(t, before, runs.Load(), "a recent session lost its token")
}

// The session ID travels in the context.
func TestSessionContext(t *testing.T) {
	assert.Equal(t, "", sessionOf(context.Background()))
	assert.Equal(t, "s1", sessionOf(withSession(context.Background(), "s1")))
}
