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

package mcpauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// oauthServers are an authorization server and an MCP server that wants
// its tokens.
func oauthServers(t *testing.T) (mcpURL string, refreshed *atomic.Int32) {
	t.Helper()
	refreshed = &atomic.Int32{}
	var as *httptest.Server
	as = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"issuer": as.URL, "authorization_endpoint": as.URL + "/authorize", "token_endpoint": as.URL + "/token",
				"registration_endpoint": as.URL + "/register", "response_types_supported": []string{"code"},
				"code_challenge_methods_supported": []string{"S256"}, "grant_types_supported": []string{"authorization_code", "refresh_token"},
				"token_endpoint_auth_methods_supported": []string{"none"},
			})
		case "/register":
			var meta map[string]any
			json.NewDecoder(r.Body).Decode(&meta)
			meta["client_id"] = "blitz-client"
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(meta)
		case "/authorize":
			q := r.URL.Query()
			to, _ := url.Parse(q.Get("redirect_uri"))
			to.RawQuery = url.Values{"code": {"the-code"}, "state": {q.Get("state")}}.Encode()
			http.Redirect(w, r, to.String(), http.StatusFound)
		case "/token":
			r.ParseForm()
			access := "token-1"
			if r.Form.Get("grant_type") == "refresh_token" {
				refreshed.Add(1)
				access = "token-2"
			} else if r.Form.Get("code") != "the-code" || r.Form.Get("code_verifier") == "" {
				http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"access_token": access, "token_type": "Bearer", "refresh_token": "refresh-1", "expires_in": 3600})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(as.Close)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	var ms *httptest.Server
	ms = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/oauth-protected-resource" || strings.HasPrefix(r.URL.Path, "/.well-known/oauth-protected-resource/") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"resource": ms.URL + "/mcp", "authorization_servers": []string{as.URL}})
			return
		}
		if h := r.Header.Get("Authorization"); h != "Bearer token-1" && h != "Bearer token-2" {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+ms.URL+`/.well-known/oauth-protected-resource"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(ms.Close)
	t.Cleanup(func() {
		for ss := range srv.Sessions() {
			ss.Close()
		}
	})
	return ms.URL + "/mcp", refreshed
}

func TestLoginAndRefresh(t *testing.T) {
	mcpURL, refreshed := oauthServers(t)
	store := &secrets.Memory{}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := Load(store, "docs")
	assert.ErrorIs(t, err, ErrNotSignedIn)

	// The "browser" follows the address to the local callback.
	browse := func(u string) {
		go func() {
			if resp, err := http.Get(u); err == nil {
				resp.Body.Close()
			}
		}()
	}
	require.NoError(t, Login(ctx, store, "docs", mcpURL, browse, nil))
	rec, err := Load(store, "docs")
	require.NoError(t, err)
	assert.Equal(t, "blitz-client", rec.ClientID)
	assert.Equal(t, "token-1", rec.Token.AccessToken)

	// An expired token is refreshed, and the new one kept.
	rec.Token.Expiry = time.Now().Add(-time.Hour)
	require.NoError(t, Save(store, "docs", rec))
	rec, _ = Load(store, "docs")
	ts, err := Handler(store, "docs", rec).TokenSource(ctx)
	require.NoError(t, err)
	tok, err := ts.Token()
	require.NoError(t, err)
	assert.Equal(t, "token-2", tok.AccessToken)
	assert.EqualValues(t, 1, refreshed.Load())
	again, _ := Load(store, "docs")
	assert.Equal(t, "token-2", again.Token.AccessToken, "the refreshed token wasn't kept")

	require.NoError(t, Forget(store, "docs"))
	_, err = Load(store, "docs")
	assert.ErrorIs(t, err, ErrNotSignedIn)
}

func TestNeedsLogin(t *testing.T) {
	h := NeedsLogin("docs")
	ts, err := h.TokenSource(context.Background())
	require.NoError(t, err)
	assert.Nil(t, ts)
	err = h.Authorize(context.Background(), nil, nil)
	assert.ErrorContains(t, err, "blitz mcp login docs")
}

func TestParsePasted(t *testing.T) {
	res := parsePasted(" http://127.0.0.1:5555/callback?code=abc&state=xyz \n")
	require.NotNil(t, res)
	assert.Equal(t, "abc", res.Code)
	assert.Equal(t, "xyz", res.State)
	assert.Nil(t, parsePasted("just-a-code"))
}
