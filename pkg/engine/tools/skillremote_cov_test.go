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

package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newRemoteCov is a skill whose script comes from srv (body is what the
// definition pins), run in the pass-through sandbox.
func newRemoteCov(t *testing.T, srv *httptest.Server, body string, decide func(api.ApprovalRequest) api.Decision, web *WebFetchConfig) covFixture {
	t.Helper()
	doc := "---\nname: remote\nscripts:\n  - name: fetched\n    language: python\n    storage_uri: " + srv.URL + "/fetched.py\n    storage_sha256: " + hexSHA256([]byte(body)) + "\n---\n"
	f := newCovFixture(t, map[string]string{"remote": doc}, nil, decide, nil)
	if web != nil {
		f.r.SetWeb(*web)
		f.r.web.client.Transport = srv.Client().Transport
	}
	return f
}

// A storage_uri script is fetched under the web rules and its pin: each
// way it can be refused says why, and a fetched one is cached by its hash
// (BL-SK-04).
func TestRemoteScriptFetchRules(t *testing.T) {
	const body = "print('from storage')\n"
	var status = http.StatusOK
	var payload = body
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte(payload))
	}))
	defer srv.Close()
	open := &WebFetchConfig{AllowPrivate: true, AllowNetwork: true}

	for _, c := range []struct {
		name    string
		web     *WebFetchConfig
		decide  func(api.ApprovalRequest) api.Decision
		status  int
		payload string
		err     string
	}{
		{name: "web off", err: "web access is off"},
		{name: "no network", web: &WebFetchConfig{AllowPrivate: true}, err: "the sandbox has no network"},
		{name: "denied domain", web: &WebFetchConfig{AllowPrivate: true, AllowNetwork: true, DenyDomains: []string{"127.0.0.1"}}, err: "blocked by web.deny_domains"},
		{name: "fetch refused", web: open, decide: denyAll, err: "not approved"},
		{name: "not found", web: open, status: http.StatusNotFound, err: "404 Not Found"},
		{name: "too large", web: open, payload: strings.Repeat("#", remoteScriptLimit+1), err: "is larger than 10 MiB"},
		{name: "pin mismatch", web: open, payload: "print('changed')\n", err: "doesn't match the SHA-256"},
		{name: "fetched", web: open},
	} {
		t.Run(c.name, func(t *testing.T) {
			status, payload = http.StatusOK, body
			if c.status != 0 {
				status = c.status
			}
			if c.payload != "" {
				payload = c.payload
			}
			decide := c.decide
			if decide == nil {
				decide = approveAll
			}
			f := newRemoteCov(t, srv, body, decide, c.web)
			out := f.r.Run(context.Background(), RunSkillScriptInput{Skill: "remote", Script: "fetched"})
			if c.err != "" {
				assert.Contains(t, out.Error, c.err, "%+v", out)
				return
			}
			require.Equal(t, "", out.Error, "%+v", out)
			assert.Equal(t, "from storage\n", out.Stdout)
			cached := filepath.Join(os.Getenv("HOME"), ".blitz", "skill-cache", hexSHA256([]byte(body)))
			b, err := os.ReadFile(cached)
			require.NoError(t, err, "cached by its hash")
			assert.Equal(t, body, string(b))

			srv.Close() // from the cache now, with no server
			f.r.web = nil
			again := f.r.Run(context.Background(), RunSkillScriptInput{Skill: "remote", Script: "fetched"})
			require.Equal(t, "", again.Error, "%+v", again)
		})
	}
}

// A fetch that can't reach the server says so.
func TestRemoteScriptUnreachable(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	f := newRemoteCov(t, srv, "print(1)\n", approveAll, &WebFetchConfig{AllowPrivate: true, AllowNetwork: true})
	srv.Close()
	out := f.r.Run(context.Background(), RunSkillScriptInput{Skill: "remote", Script: "fetched"})
	assert.Contains(t, out.Error, "fetching https://", "%+v", out)
}
