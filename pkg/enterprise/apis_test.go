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
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// EN-91: an API connector's call goes through the service as the person:
// the path under the connector, its query, body and content type; a token
// the service ended early is renewed once and the call asked again.
func TestCallAPI(t *testing.T) {
	_, s, c, _, _ := setup(t)
	ctx := context.Background()
	_, err := c.CallAPI(ctx, "review", http.MethodPost, "/check", "", nil, nil)
	assert.Error(t, err, "not signed in")
	require.NoError(t, c.SignInBrowser(ctx, browse(t), nil))

	var got struct{ path, query, body, ctype string }
	s.Mu.Lock()
	s.SharedAPIs = map[string]http.Handler{"review": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got.path, got.query, got.body, got.ctype = r.URL.Path, r.URL.RawQuery, string(b), r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"verdict":"pass"}`))
	})}
	s.Mu.Unlock()

	tests := []struct {
		name      string
		connector string
		status    int
	}{
		{"its connector", "review", http.StatusOK},
		{"no such connector", "nope", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := c.CallAPI(ctx, tt.connector, http.MethodPost, "/check", "strict=1", []byte(`{"diff":"x"}`), http.Header{"Content-Type": {"application/json"}, "X-Other": {"dropped"}})
			require.NoError(t, err)
			defer res.Body.Close()
			assert.Equal(t, tt.status, res.StatusCode)
		})
	}
	assert.Equal(t, "/check", got.path)
	assert.Equal(t, "strict=1", got.query)
	assert.JSONEq(t, `{"diff":"x"}`, got.body)
	assert.Equal(t, "application/json", got.ctype)

	// The service ends the access token: renewed, and asked again.
	s.Mu.Lock()
	clear(s.Access)
	s.Mu.Unlock()
	got.path = ""
	res, err := c.CallAPI(ctx, "review", http.MethodGet, "again", "", nil, nil)
	require.NoError(t, err)
	defer res.Body.Close()
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "/again", got.path)
}

// EN-92: a git hook's workflow's receipt reaches the service.
func TestReceipt(t *testing.T) {
	_, s, c, _, _ := setup(t)
	ctx := context.Background()
	at := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	r := Receipt{Workflow: "code-review", Version: "1.2.0", Event: "pre-commit", Tree: "abc123", Passed: false, Message: "a secret in main.go", Workspace: "shop", At: at}
	assert.Error(t, c.Receipt(ctx, r), "not signed in")
	require.NoError(t, c.SignInBrowser(ctx, browse(t), nil))
	require.NoError(t, c.Receipt(ctx, r))
	s.Mu.Lock()
	defer s.Mu.Unlock()
	require.Len(t, s.Receipts, 1)
	want, _ := json.Marshal(r)
	got, _ := json.Marshal(s.Receipts[0])
	assert.JSONEq(t, string(want), string(got))
}

// An API step's path stays under its connector: a "..", plain or
// escaped, is refused before anything is sent (security review
// enterprise#5).
func TestCallAPIPathStaysUnderItsConnector(t *testing.T) {
	_, _, c, _, _ := setup(t)
	ctx := context.Background()
	for _, p := range []string{"../../me/library/skill/x", "/a/../../b", "%2e%2e/%2e%2e/me", `a\..\..\b`, "%zz"} {
		_, err := c.CallAPI(ctx, "review", http.MethodGet, p, "", nil, nil)
		assert.ErrorContains(t, err, "can't leave its connector", p)
	}
}
