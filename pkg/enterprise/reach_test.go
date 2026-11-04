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

package enterprise

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/secrets"
)

// A service that takes a request and never answers (EN-37): the call
// gives up, and the next ones fail at once until downFor passes; an
// answer clears it.
func TestUnanswered(t *testing.T) {
	defer func(h time.Duration) { headerTimeout = h }(headerTimeout)
	headerTimeout = 50 * time.Millisecond
	var hang atomic.Bool
	hang.Store(true)
	var calls atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if hang.Load() {
			select {
			case <-release:
			case <-r.Context().Done():
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"Acme","oidc":{"issuer":"x"}}`))
	}))
	defer srv.Close()
	defer close(release)
	now := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	c := New(srv.URL, &secrets.Memory{}, Options{Now: func() time.Time { return now }})
	getWith := func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
		if err != nil {
			return err
		}
		res, err := c.hc.Do(req)
		if err == nil {
			res.Body.Close()
		}
		return err
	}
	get := func() error { return getWith(context.Background()) }

	assert.True(t, c.Reachable())
	start := time.Now()
	require.Error(t, get(), "no answer")
	assert.Less(t, time.Since(start), 5*time.Second, "given up on, not waited for")
	assert.False(t, c.Reachable())
	assert.Equal(t, int32(1), calls.Load())

	err := get()
	assert.ErrorIs(t, err, ErrUnreachable, "at once")
	assert.Equal(t, int32(1), calls.Load(), "without asking again")

	hang.Store(false)
	now = now.Add(downFor)
	require.NoError(t, get(), "asked again after a while")
	assert.True(t, c.Reachable())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.Error(t, getWith(ctx))
	assert.True(t, c.Reachable(), "a caller giving up isn't the service down")
	assert.True(t, (&Client{hc: http.DefaultClient}).Reachable(), "a client of the caller's own")
	assert.Same(t, HTTPClient(), HTTPClient())
}
