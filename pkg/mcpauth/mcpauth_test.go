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
	"errors"
	"io"
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
	"golang.org/x/oauth2"
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

// failing is a secret store whose operations fail with err.
type failing struct{ err error }

func (f failing) Kind() string               { return "failing" }
func (f failing) Get(string) (string, error) { return "", f.err }
func (f failing) Set(string, string) error   { return f.err }
func (f failing) Delete(string) error        { return f.err }

// The store's own failures are reported; a missing or unreadable sign-in
// is ErrNotSignedIn, and forgetting one that's gone is fine.
func TestStoreErrors(t *testing.T) {
	boom := errors.New("keychain locked")
	_, err := Load(failing{boom}, "docs")
	assert.ErrorIs(t, err, boom)
	assert.ErrorIs(t, Forget(failing{boom}, "docs"), boom)
	assert.NoError(t, Forget(failing{secrets.ErrNotFound}, "docs"))

	store := &secrets.Memory{}
	for name, v := range map[string]string{"not JSON": "{", "no token": `{"client_id":"x"}`} {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, store.Set(secretName("docs"), v))
			_, err := Load(store, "docs")
			assert.ErrorIs(t, err, ErrNotSignedIn)
		})
	}
}

// A refresh that fails is reported, and nothing is saved.
func TestRefreshFails(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
	}))
	defer ts.Close()
	store := &secrets.Memory{}
	rec := &Record{ClientID: "c", TokenURL: ts.URL, Token: &oauth2.Token{AccessToken: "old", RefreshToken: "r", Expiry: time.Now().Add(-time.Hour)}}
	src, err := Handler(store, "docs", rec).TokenSource(context.Background())
	require.NoError(t, err)
	_, err = src.Token()
	assert.Error(t, err)
	_, err = Load(store, "docs")
	assert.ErrorIs(t, err, ErrNotSignedIn, "nothing saved")
}

// Authorize closes the response it's given and asks for blitz mcp login.
func TestAuthorizeClosesBody(t *testing.T) {
	body := &closeRecorder{Reader: strings.NewReader("x")}
	err := NeedsLogin("docs").Authorize(context.Background(), nil, &http.Response{Body: body})
	assert.ErrorContains(t, err, "needs signing in again")
	assert.True(t, body.closed)
}

type closeRecorder struct {
	io.Reader
	closed bool
}

func (c *closeRecorder) Close() error { c.closed = true; return nil }

// authorizeRedirect asks the authorization server for a code, as a browser
// would, and returns the callback address it redirects to (not followed).
func authorizeRedirect(t *testing.T, authURL string) string {
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Get(authURL)
	if err != nil {
		t.Error(err)
		return ""
	}
	resp.Body.Close()
	return resp.Header.Get("Location")
}

// get fetches u and returns its status.
func get(t *testing.T, u string) int {
	resp, err := http.Get(u)
	if err != nil {
		t.Error(err)
		return 0
	}
	resp.Body.Close()
	return resp.StatusCode
}

// The code can be pasted (the address the browser ended on); what isn't
// an address with a code is ignored. The local callback refuses other
// paths and a redirect without a code.
func TestLoginPasted(t *testing.T) {
	mcpURL, _ := oauthServers(t)
	store := &secrets.Memory{}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pasted := make(chan string, 2)
	show := func(u string) {
		loc := authorizeRedirect(t, u)
		cb, err := url.Parse(loc)
		assert.NoError(t, err) // not require: this runs on the client's goroutine
		other := *cb
		other.Path, other.RawQuery = "/elsewhere", ""
		assert.Equal(t, http.StatusNotFound, get(t, other.String()))
		noCode := *cb
		noCode.RawQuery = "error=access_denied"
		assert.Equal(t, http.StatusBadRequest, get(t, noCode.String()))
		pasted <- "the-code-alone"
		pasted <- loc
	}
	require.NoError(t, Login(ctx, store, "docs", mcpURL, show, pasted))
	rec, err := Load(store, "docs")
	require.NoError(t, err)
	assert.Equal(t, "token-1", rec.Token.AccessToken)
}

// With nothing to paste from, the callback still works, and a second
// callback is ignored.
func TestLoginClosedPasteTwoCallbacks(t *testing.T) {
	mcpURL, _ := oauthServers(t)
	store := &secrets.Memory{}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pasted := make(chan string)
	close(pasted)
	show := func(u string) {
		loc := authorizeRedirect(t, u)
		assert.Equal(t, http.StatusOK, get(t, loc))
		assert.Equal(t, http.StatusOK, get(t, loc), "the second is answered, and dropped")
	}
	require.NoError(t, Login(ctx, store, "docs", mcpURL, show, pasted))
}

// Login fails when the sign-in is abandoned, can't be saved, the server
// can't be reached, or the server needs no sign-in.
func TestLoginFailures(t *testing.T) {
	t.Run("abandoned", func(t *testing.T) {
		mcpURL, _ := oauthServers(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := Login(ctx, &secrets.Memory{}, "docs", mcpURL, func(string) { cancel() }, nil)
		assert.Error(t, err)
	})
	t.Run("not saved", func(t *testing.T) {
		mcpURL, _ := oauthServers(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		browse := func(u string) { get(t, authorizeRedirect(t, u)) }
		err := Login(ctx, failing{errors.New("locked")}, "docs", mcpURL, browse, nil)
		assert.ErrorContains(t, err, "saving the sign-in")
	})
	t.Run("unreachable", func(t *testing.T) {
		ts := httptest.NewServer(http.NotFoundHandler())
		u := ts.URL
		ts.Close()
		err := Login(context.Background(), &secrets.Memory{}, "docs", u+"/mcp", func(string) {}, nil)
		assert.Error(t, err)
	})
	t.Run("no OAuth", func(t *testing.T) {
		srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
		ts := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil))
		defer ts.Close()
		err := Login(context.Background(), &secrets.Memory{}, "docs", ts.URL, func(string) {}, nil)
		assert.ErrorContains(t, err, "needs no OAuth")
	})
}
