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
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// ErrUnreachable is the organization's service not answering lately:
// Blitz works on as if offline (spec_053_enterprise EN-37), its files
// and folders as ever, the organization's models and the rest waiting
// until the service answers again.
var ErrUnreachable = errors.New("your organization's service isn't answering")

// The limits on reaching the service: connecting, its TLS handshake, the
// wait for an answer to start (a stream's or a download's body may take
// as long as it takes), and how long after it failed to answer calls
// fail at once instead of each waiting again.
var (
	dialTimeout   = 10 * time.Second
	tlsTimeout    = 10 * time.Second
	headerTimeout = 20 * time.Second
	downFor       = 30 * time.Second
)

// HTTPClient is the client calls to an organization's service go through
// unless Options says: one that gives up on a service that doesn't
// answer, where Go's default client would wait for good.
var HTTPClient = sync.OnceValue(func() *http.Client {
	return &http.Client{Transport: transport()}
})

func transport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = (&net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}).DialContext
	t.TLSHandshakeTimeout = tlsTimeout
	t.ResponseHeaderTimeout = headerTimeout
	return t
}

// reach is a client's transport that remembers the service didn't
// answer: for downFor after a failure, every call fails at once with
// ErrUnreachable, so opening workspaces, building their models and the
// rest don't each wait out the limits again. An answer, any answer,
// clears it.
type reach struct {
	base http.RoundTripper
	now  func() time.Time

	mu   sync.Mutex
	down time.Time
	why  error
}

func (r *reach) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	if !r.down.IsZero() && r.now().Sub(r.down) < downFor {
		why := r.why
		r.mu.Unlock()
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, why)
	}
	r.mu.Unlock()
	res, err := r.base.RoundTrip(req)
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case err == nil:
		r.down, r.why = time.Time{}, nil
	case !errors.Is(req.Context().Err(), context.Canceled):
		// No answer (a caller's deadline counts; its giving up doesn't).
		r.down, r.why = r.now(), err
	}
	return res, err
}

// Reachable is false while the service hasn't answered lately (the last
// call failed to reach it within downFor).
func (c *Client) Reachable() bool {
	r, ok := c.hc.Transport.(*reach)
	if !ok {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.down.IsZero() || r.now().Sub(r.down) >= downFor
}
