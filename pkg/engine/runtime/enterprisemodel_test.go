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
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/enterprise"
	"github.com/retail-cortex/blitz/pkg/redact"
	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/agent"
)

// orgService is the organization's service as Blitz meets it after
// sign-in: it renews the session (each refresh token good once), lists
// the person's models, and passes model calls to its gateway, which takes
// only a current access token.
type orgService struct {
	*httptest.Server
	g        *fakeGateway
	mu       sync.Mutex
	n        int
	current  string // the access token it takes
	refresh  string // the refresh token it takes
	renewals int
}

func newOrgService(t *testing.T, replies ...[2]any) *orgService {
	s := &orgService{g: newFakeGateway(t, replies...), refresh: "rt-0"}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/session/refresh", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			RefreshToken string `json:"refresh_token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		s.mu.Lock()
		defer s.mu.Unlock()
		if in.RefreshToken != s.refresh {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"unauthenticated","message":"Sign in again."}`))
			return
		}
		s.n++
		s.renewals++
		s.current, s.refresh = fmt.Sprintf("at-%d", s.n), fmt.Sprintf("rt-%d", s.n)
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": s.current, "refresh_token": s.refresh, "expires_in": 600,
			"person": map[string]any{"id": "p1", "email": "ana@acme.com"}})
	})
	mux.HandleFunc("GET /api/v1/models", func(w http.ResponseWriter, r *http.Request) {
		if !s.takes(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"fast","api":"responses"},{"id":"smart","api":"messages","default":true,"capabilities":{"tools":true,"context":200000}}]}`))
	})
	mux.HandleFunc("/api/v1/", func(w http.ResponseWriter, r *http.Request) {
		if !s.takes(r) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"unauthenticated","message":"Sign in again."}`))
			return
		}
		s.g.Config.Handler.ServeHTTP(w, r)
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func (s *orgService) takes(r *http.Request) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current != "" && r.Header.Get("Authorization") == "Bearer "+s.current
}

// signedIn is cfg connected to s, with a sign-in kept as pkg/enterprise
// keeps it, registered as blitzd registers its connection.
func signedIn(t *testing.T, s *orgService) *config.Config {
	store := &secrets.Memory{}
	require.NoError(t, store.Set("enterprise-session", fmt.Sprintf(`{"url":%q,"refresh_token":"rt-0","person":{"email":"ana@acme.com"}}`, s.URL)))
	enterprise.Register(enterprise.New(s.URL, store, enterprise.Options{}))
	cfg := config.DefaultConfig()
	cfg.Enterprise.URL = s.URL
	return cfg
}

// agentCtx is a model call's context in a session, for an agent.
type agentCtx struct {
	sessionCtx
	a agent.Agent
}

func (c agentCtx) Agent() agent.Agent { return c.a }

func inAgent(t *testing.T, session, name string) context.Context {
	a, err := agent.New(agent.Config{Name: name})
	require.NoError(t, err)
	return agentCtx{sessionCtx{context.Background(), session}, a}
}

// Connected to an organization, its models are the models: whatever the
// settings name, through its gateway with the person's own session, the
// workspace, session and agent named (spec_053_enterprise EN-20, EN-22).
func TestEnterpriseModel(t *testing.T) {
	t.Run("an OpenAI model", func(t *testing.T) {
		s := newOrgService(t)
		cfg := signedIn(t, s)
		cfg.LLM.Provider, cfg.LLM.Gemini.APIKey = "gemini", "a key of my own"
		cfg.Tools.OrgWorkspace = "ws-plans"
		m, err := NewModel(context.Background(), cfg, "fast")
		require.NoError(t, err)
		text, _, err := callAPI(inAgent(t, "sess-1", "coder"), m, helloRequest())
		require.NoError(t, err)
		assert.Equal(t, "One cell, c1, alive.", text)
		got := s.g.requests()
		require.Len(t, got, 1)
		assert.Equal(t, "/api/v1/responses", got[0].Path)
		assert.Equal(t, "Bearer at-1", got[0].Header.Get("Authorization"))
		assert.Equal(t, "sess-1", got[0].Header.Get("Blitz-Session"))
		assert.Equal(t, "coder", got[0].Header.Get("Blitz-Agent"))
		assert.Equal(t, "ws-plans", got[0].Header.Get("Blitz-Workspace"))
		assert.Equal(t, "fast", got[0].Body["model"])
	})
	t.Run("a Claude model, at /messages", func(t *testing.T) {
		s := newOrgService(t, [2]any{http.StatusOK, gwClaudeText})
		cfg := signedIn(t, s)
		m, err := NewModel(context.Background(), cfg, "anthropic/claude-sonnet-5-5")
		require.NoError(t, err)
		text, _, err := callAPI(inSession("sess-2"), m, helloRequest())
		require.NoError(t, err)
		assert.Equal(t, "One cell, c1, alive.", text)
		got := s.g.requests()
		require.Len(t, got, 1)
		assert.Equal(t, "/api/v1/messages", got[0].Path)
		assert.Equal(t, "smart", got[0].Body["model"], "not offered: the organization's default")
		assert.Empty(t, got[0].Header.Get("X-Api-Key"), "no key of its own")
		assert.Empty(t, got[0].Header.Get("Blitz-Agent"), "outside an agent")
		assert.Empty(t, got[0].Header.Get("Blitz-Workspace"), "not the organization's workspace")
	})
	t.Run("an access token the service stopped taking is renewed", func(t *testing.T) {
		s := newOrgService(t)
		cfg := signedIn(t, s)
		m, err := NewModel(context.Background(), cfg, "fast")
		require.NoError(t, err)
		// The gateway stops taking at-1 (the key that signed it changed):
		// the renewal gives at-2, which it takes.
		s.mu.Lock()
		s.current = "at-2"
		s.mu.Unlock()
		_, _, err = callAPI(inSession("sess-3"), m, helloRequest())
		require.NoError(t, err)
		assert.Equal(t, 2, s.renewals)
	})
	t.Run("the session ended after the model was built", func(t *testing.T) {
		s := newOrgService(t)
		cfg := signedIn(t, s)
		m, err := NewModel(context.Background(), cfg, "fast")
		require.NoError(t, err)
		c, err := enterpriseClient(cfg)
		require.NoError(t, err)
		c.Invalidate()
		s.mu.Lock()
		s.refresh = "ended"
		s.mu.Unlock()
		_, _, err = callAPI(inSession("sess-4"), m, helloRequest())
		assert.ErrorIs(t, err, enterprise.ErrNotSignedIn)
	})
	// The organization's errors are worded for its people: no [llm.api]
	// and no token script (N26 engine-runtime#16).
	for _, c := range []struct {
		name   string
		status int
		want   string
	}{
		{name: "a refused sign-in", status: http.StatusForbidden, want: "your organization's service refused your sign-in (HTTP 403): sign in again (Settings › Organization, or blitz enterprise login)"},
		{name: "another error", status: http.StatusNotFound, want: "your organization's service: HTTP 404 Not Found: no such model"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newOrgService(t, [2]any{c.status, `{"error":{"message":"no such model"}}`})
			m, err := NewModel(context.Background(), signedIn(t, s), "fast")
			require.NoError(t, err)
			_, _, err = callAPI(inSession("sess-5"), m, helloRequest())
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.want)
			assert.NotContains(t, err.Error(), "[llm.api]")
			assert.NotContains(t, err.Error(), "token script")
		})
	}
	t.Run("signed out", func(t *testing.T) {
		s := newOrgService(t)
		enterprise.Register(enterprise.New(s.URL, &secrets.Memory{}, enterprise.Options{}))
		cfg := config.DefaultConfig()
		cfg.Enterprise.URL = s.URL
		_, err := NewModel(context.Background(), cfg, "fast")
		assert.ErrorContains(t, err, "sign in to your organization")
	})
	t.Run("the session ended at the service", func(t *testing.T) {
		s := newOrgService(t)
		cfg := signedIn(t, s)
		s.mu.Lock()
		s.refresh = "another"
		s.mu.Unlock()
		_, err := NewModel(context.Background(), cfg, "fast")
		assert.ErrorContains(t, err, "Sign in again.")
	})
	t.Run("no models for this person", func(t *testing.T) {
		s := newOrgService(t)
		cfg := signedIn(t, s)
		empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/models") {
				_, _ = w.Write([]byte(`{"data":[]}`))
				return
			}
			s.Config.Handler.ServeHTTP(w, r)
		}))
		t.Cleanup(empty.Close)
		store := &secrets.Memory{}
		require.NoError(t, store.Set("enterprise-session", fmt.Sprintf(`{"url":%q,"refresh_token":"rt-0"}`, empty.URL)))
		enterprise.Register(enterprise.New(empty.URL, store, enterprise.Options{}))
		cfg.Enterprise.URL = empty.URL
		_, err := NewModel(context.Background(), cfg, "fast")
		assert.ErrorContains(t, err, "offers you no models")
	})
	t.Run("an address that isn't one", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Enterprise.URL = "http://blitz.acme.com"
		_, err := NewModel(context.Background(), cfg, "fast")
		assert.ErrorContains(t, err, "[enterprise] url")
	})
}

// The model menu lists the organization's models, at its prices, in
// place of the person's own providers (EN-20, EN-21).
func TestEnterpriseListModels(t *testing.T) {
	s := newOrgService(t)
	cfg := signedIn(t, s)
	cfg.LLM.Gemini.APIKey = "a key of my own"
	assert.Equal(t, []string{"enterprise"}, ConfiguredProviders(cfg))
	got := ListModels(context.Background(), cfg)
	require.Len(t, got, 1)
	require.NoError(t, got[0].Err)
	assert.Equal(t, []ListedModel{{ID: "fast"}, {ID: "smart", Default: true, Caps: &enterprise.Capabilities{Tools: true, Context: 200000}}}, got[0].Models)

	priced := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = w.Write([]byte(`{"data":[{"id":"fast","price":{"input_per_mtok":0.75,"output_per_mtok":3.75}}]}`))
			return
		}
		s.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(priced.Close)
	// The same session, kept for this address: its latest refresh token.
	s.mu.Lock()
	latest := s.refresh
	s.mu.Unlock()
	store := &secrets.Memory{}
	require.NoError(t, store.Set("enterprise-session", fmt.Sprintf(`{"url":%q,"refresh_token":%q}`, priced.URL, latest)))
	enterprise.Register(enterprise.New(priced.URL, store, enterprise.Options{}))
	cfg.Enterprise.URL = priced.URL
	got = ListModels(context.Background(), cfg)
	require.NoError(t, got[0].Err)
	assert.Equal(t, &config.ModelPrice{InputPerMTok: 0.75, OutputPerMTok: 3.75}, got[0].Models[0].Price, "the organization's price")

	t.Run("signed out", func(t *testing.T) {
		enterprise.Register(enterprise.New(priced.URL, &secrets.Memory{}, enterprise.Options{}))
		got := ListModels(context.Background(), cfg)
		assert.ErrorIs(t, got[0].Err, enterprise.ErrNotSignedIn)
	})
	t.Run("an address that isn't one", func(t *testing.T) {
		bad := config.DefaultConfig()
		bad.Enterprise.URL = "ftp://acme"
		assert.Error(t, ListModels(context.Background(), bad)[0].Err)
	})
}

// The organization's policy and its catalog's expiry: a model named with
// a provider of the person's own uses it when the policy allows (bare
// names stay the organization's), and the organization's models wait
// once its catalog is out of date (EN-33, EN-35).
func TestEnterprisePolicy(t *testing.T) {
	s := newOrgService(t)
	cfg := signedIn(t, s)
	cfg.LLM.Provider, cfg.LLM.Gemini.APIKey = "gemini", "a key of my own"
	cfg.Org = config.OrgLayer{Defaults: t.TempDir(), Locked: t.TempDir()}

	stood, err := NewModel(context.Background(), cfg, "openai/gpt-x")
	require.NoError(t, err)
	assert.Equal(t, "smart", stood.Name(), "not allowed: the organization's default stands in")
	assert.Equal(t, []string{"enterprise"}, ConfiguredProviders(cfg))

	cfg.Org.Policy.Models.OwnProviders = true
	assert.Equal(t, []string{"enterprise", "gemini"}, ConfiguredProviders(cfg))
	own, err := NewModel(context.Background(), cfg, "openai/gpt-x")
	require.NoError(t, err)
	assert.Equal(t, "gpt-x", own.Name(), "allowed: the person's own provider (the organization's would stand its default, smart, in)")
	m, err := NewModel(context.Background(), cfg, "fast")
	require.NoError(t, err)
	_, _, err = callAPI(inSession("s"), m, helloRequest())
	require.NoError(t, err)
	assert.Equal(t, "/api/v1/responses", s.g.requests()[0].Path, "a bare name is the organization's")

	cfg.Org.Expires = time.Now().Add(-time.Minute)
	_, err = NewModel(context.Background(), cfg, "fast")
	assert.ErrorContains(t, err, "catalog is out of date")

	// A policy that can't be read: the models wait too (N26 api-client-config#4).
	cfg.Org.Expires, cfg.Org.Problem = time.Time{}, "not fetched yet"
	_, err = NewModel(context.Background(), cfg, "fast")
	assert.ErrorContains(t, err, "policy can't be read (not fetched yet)")
}

// fixedSource gives one access token.
type fixedSource string

func (f fixedSource) AccessToken(context.Context) (string, error) { return string(f), nil }
func (f fixedSource) Invalidate()                                 {}

// The organization's access token is remembered by the redactor, as a
// token script's is (MDL-108; N26 engine-runtime#27).
func TestEnterpriseTokenIsMasked(t *testing.T) {
	p := &apiProvider{source: fixedSource("org-access-token-27")}
	tok, err := p.token(context.Background(), "s")
	require.NoError(t, err)
	assert.Equal(t, "org-access-token-27", tok)
	assert.NotContains(t, redact.Mask("the gateway said: Bearer org-access-token-27"), "org-access-token-27")
}
