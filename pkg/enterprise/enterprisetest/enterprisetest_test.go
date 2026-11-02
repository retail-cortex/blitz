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

package enterprisetest

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fakes refuse what the real ones would: a sign-in that isn't PKCE's
// or lacks a nonce, a code exchanged with the wrong verifier, a grant they
// don't know, and every call without a current access token.
func TestStrict(t *testing.T) {
	p := NewProvider(t)
	s := NewService(t, p.URL)
	get := func(u string) int {
		res, err := http.Get(u)
		require.NoError(t, err)
		res.Body.Close()
		return res.StatusCode
	}
	post := func(u string, form url.Values) int {
		res, err := http.PostForm(u, form)
		require.NoError(t, err)
		res.Body.Close()
		return res.StatusCode
	}
	assert.Equal(t, http.StatusBadRequest, get(p.URL+"/authorize?client_id=desktop&response_type=code&scope=openid&code_challenge_method=S256"), "no nonce")
	assert.Equal(t, http.StatusBadRequest, get(p.URL+"/authorize?client_id=nobody"), "unknown client")
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Get(p.URL + "/authorize?client_id=desktop&response_type=code&scope=openid&nonce=n&code_challenge_method=S256&code_challenge=abc&redirect_uri=http://127.0.0.1:1/callback&state=s")
	require.NoError(t, err)
	res.Body.Close()
	loc, _ := url.Parse(res.Header.Get("Location"))
	code := loc.Query().Get("code")
	assert.Equal(t, http.StatusBadRequest, post(p.URL+"/token", url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {"wrong"},
		"redirect_uri": {"http://127.0.0.1:1/callback"}, "client_id": {"desktop"}, "client_secret": {"d-secret"}}), "the wrong verifier")
	assert.Equal(t, http.StatusBadRequest, post(p.URL+"/token", url.Values{"grant_type": {"password"}, "client_id": {"desktop"}, "client_secret": {"d-secret"}}), "an unknown grant")
	assert.Equal(t, http.StatusUnauthorized, post(p.URL+"/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {"desktop"}, "client_secret": {"wrong"}}), "the wrong secret")
	assert.Equal(t, http.StatusUnauthorized, post(p.URL+"/device", url.Values{"client_id": {"nobody"}}), "an unknown device client")

	for _, path := range []string{"/api/v1/catalog", "/api/v1/catalog/files/abc", "/api/v1/catalog/watch", "/api/v1/workspaces", "/api/v1/workspaces/x/watch", "/api/v1/terms", "/api/v1/models", "/git/x.git/info/refs"} {
		assert.Equal(t, http.StatusUnauthorized, get(s.URL+path), path)
	}
	for _, path := range []string{"/api/v1/workspaces", "/api/v1/terms/accept"} {
		res, err := http.Post(s.URL+path, "application/json", strings.NewReader(`{}`))
		require.NoError(t, err)
		res.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, res.StatusCode, path)
	}
	// A run's calls need its secret, as JSON.
	for body, want := range map[string]int{`not JSON`: http.StatusBadRequest, `{"secret":"wrong"}`: http.StatusUnauthorized} {
		for _, path := range []string{"/api/v1/runs/r1/token", "/api/v1/runs/r1/end"} {
			res, err := http.Post(s.URL+path, "application/json", strings.NewReader(body))
			require.NoError(t, err)
			res.Body.Close()
			assert.Equal(t, want, res.StatusCode, "%s %s", path, body)
		}
	}
	res, err = http.Post(s.URL+"/api/v1/session", "application/json", strings.NewReader(`{"id_token":"forged"}`))
	require.NoError(t, err)
	res.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
	s.Close()
	s.Close() // closing twice is fine
}

// The service's jobs and names, as strict as the real one's: signed in
// only, refused when the license has no jobs, JSON only, and a name asked
// for its children or content.
func TestStrictJobs(t *testing.T) {
	p := NewProvider(t)
	s := NewService(t, p.URL)
	s.Access["tok"] = "s"
	s.ServiceJobs = []ServiceJob{{ID: "j1", Name: "digest"}}
	do := func(method, path, body string, signedIn bool) int {
		req, err := http.NewRequest(method, s.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		if signedIn {
			req.Header.Set("Authorization", "Bearer tok")
		}
		res, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		res.Body.Close()
		return res.StatusCode
	}
	assert.Equal(t, http.StatusUnauthorized, do(http.MethodGet, "/api/v1/jobs", "", false))
	assert.Equal(t, http.StatusUnauthorized, do(http.MethodGet, "/api/v1/resources/urn:blitz:jobs?children", "", false))
	assert.Equal(t, http.StatusAccepted, do(http.MethodPost, "/api/v1/jobs/j1/run", "", true), "its first run")
	assert.Equal(t, http.StatusBadRequest, do(http.MethodPut, "/api/v1/jobs/j1/enabled", "not JSON", true))
	assert.Equal(t, http.StatusBadRequest, do(http.MethodPut, "/api/v1/jobs/j1/enabled", "{}", true))
	assert.Equal(t, http.StatusNotFound, do(http.MethodPut, "/api/v1/jobs/nope/enabled", `{"enabled":true}`, true))
	assert.Equal(t, http.StatusNotFound, do(http.MethodGet, "/api/v1/jobs/nope/runs", "", true))
	assert.Equal(t, http.StatusBadRequest, do(http.MethodGet, "/api/v1/resources/urn:blitz:jobs", "", true), "children or content")
	assert.Equal(t, http.StatusNotFound, do(http.MethodGet, "/api/v1/resources/urn:blitz:jobs?children", "", true))
	assert.Equal(t, http.StatusNotFound, do(http.MethodGet, "/api/v1/resources/urn:blitz:jobs?content", "", true))
	assert.Equal(t, http.StatusBadRequest, do(http.MethodPost, "/api/v1/runs/r1/answer", "not JSON", true))
	assert.Equal(t, http.StatusBadRequest, do(http.MethodPost, "/api/v1/runs/r1/answer", `{"note":"?"}`, true))
	assert.Equal(t, http.StatusConflict, do(http.MethodPost, "/api/v1/runs/r1/answer", `{"approve":true}`, true), "not waiting")
	assert.Equal(t, http.StatusUnauthorized, do(http.MethodGet, "/api/v1/usage", "", false))
	assert.Equal(t, http.StatusOK, do(http.MethodGet, "/api/v1/usage", "", true))
	s.JobsRefused = true
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/jobs/j1/runs", ""},
		{http.MethodPost, "/api/v1/jobs/j1/run", ""},
		{http.MethodPut, "/api/v1/jobs/j1/enabled", `{"enabled":false}`},
		{http.MethodGet, "/api/v1/runs/pending", ""},
		{http.MethodGet, "/api/v1/runs/watch", ""},
		{http.MethodPost, "/api/v1/runs/r1/answer", `{"approve":true}`},
	} {
		assert.Equal(t, http.StatusNotFound, do(c.method, c.path, c.body, true), c.path)
	}
}
