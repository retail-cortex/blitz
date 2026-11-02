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

// Package enterprisetest has an identity provider and an organization's
// service for tests, as strict as the real ones (spec_053_enterprise
// EN-12): the provider checks PKCE, the client and its redirect, and
// keeps the device code pending once; the service rotates refresh
// tokens, ends a session whose spent one comes back, and serves the
// person's models with an ETag.
package enterprisetest

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Provider is an identity provider as strict as Google's: the authorization
// code only with the PKCE verifier, the client's secret and the same
// redirect; the device code pending until it's approved.
type Provider struct {
	*httptest.Server
	Mu       sync.Mutex
	Clients  map[string]string // ID → secret
	codes    map[string]grant  // authorization codes
	approved bool              // the device code was entered
	Polls    int
	NoDevice bool
	IDToken  string // the ID token it gives, when not the usual
	Denied   bool   // the device code is refused
	Bare     bool   // the discovery document names no endpoints
}

type grant struct {
	client, challenge, redirect string
}

// NewProvider is a provider with two clients: "desktop" (secret
// d-secret) and "device" (v-secret). Its ID token is "id-for-<client>".
func NewProvider(t testing.TB) *Provider {
	p := &Provider{Clients: map[string]string{"desktop": "d-secret", "device": "v-secret"}, codes: map[string]grant{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		doc := map[string]string{"issuer": p.URL, "authorization_endpoint": p.URL + "/authorize", "token_endpoint": p.URL + "/token"}
		if p.Bare {
			doc = map[string]string{"issuer": p.URL}
		}
		if !p.NoDevice {
			doc["device_authorization_endpoint"] = p.URL + "/device"
		}
		_ = json.NewEncoder(w).Encode(doc)
	})
	// The person signs in: a redirect back with a code for the client.
	mux.HandleFunc("GET /authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if _, ok := p.Clients[q.Get("client_id")]; !ok || q.Get("code_challenge_method") != "S256" || q.Get("response_type") != "code" ||
			!strings.Contains(q.Get("scope"), "openid") || q.Get("nonce") == "" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		p.Mu.Lock()
		code := fmt.Sprintf("code-%d", len(p.codes))
		p.codes[code] = grant{q.Get("client_id"), q.Get("code_challenge"), q.Get("redirect_uri")}
		p.Mu.Unlock()
		http.Redirect(w, r, q.Get("redirect_uri")+"?"+url.Values{"code": {code}, "state": {q.Get("state")}}.Encode(), http.StatusFound)
	})
	mux.HandleFunc("POST /device", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		p.Mu.Lock()
		_, known := p.Clients[r.PostForm.Get("client_id")]
		p.Mu.Unlock()
		if !known {
			http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
			return
		}
		// Google's spelling, verification_url.
		_ = json.NewEncoder(w).Encode(map[string]any{"device_code": "dc", "user_code": "ABCD-EFGH",
			"verification_url": "https://www.google.com/device", "expires_in": 60, "interval": 1})
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		id, secret, ok := r.BasicAuth()
		if !ok {
			id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
		}
		w.Header().Set("Content-Type", "application/json")
		if want, ok := p.Clients[id]; !ok || want != secret {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
			return
		}
		p.Mu.Lock()
		defer p.Mu.Unlock()
		switch r.PostForm.Get("grant_type") {
		case "authorization_code":
			g, ok := p.codes[r.PostForm.Get("code")]
			delete(p.codes, r.PostForm.Get("code"))
			sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
			if !ok || g.client != id || g.redirect != r.PostForm.Get("redirect_uri") || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
		case "urn:ietf:params:oauth:grant-type:device_code":
			p.Polls++
			if p.Denied {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"access_denied"}`))
				return
			}
			if !p.approved {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
				p.approved = true // entered before the next poll
				return
			}
		default:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"unsupported_grant_type"}`))
			return
		}
		token := "id-for-" + id
		if p.IDToken != "" {
			token = p.IDToken
		}
		out := map[string]any{"access_token": "google-at", "token_type": "Bearer", "expires_in": 3600}
		if token != "none" {
			out["id_token"] = token
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	p.Server = httptest.NewServer(mux)
	t.Cleanup(p.Close)
	return p
}

// Service is the organization's service, as spec 053 EN-12 gives it: a
// refresh token is good once, and one used again ends its session.
type Service struct {
	*httptest.Server
	Mu        sync.Mutex
	issuer    string
	sessions  map[string]string // refresh token → session; "" when used
	Ended     map[string]bool
	Access    map[string]string // access token → session
	n         int
	ExpiresIn int
	Terms     string
	Refreshes int
	SignIns   []string // the client each sign-in named
	Offline   bool
	Tokenless bool // a sign-in answered without tokens
	// Key signs the branding; Branding is what it serves (none when nil),
	// and Terms the version in force, accepted or not, by person.
	Key      ed25519.PrivateKey
	KeyID    string
	Branding map[string]any
	Version  string
	Accepts  map[string]bool
	// Forged signs the branding and catalog with a key the service
	// doesn't publish.
	Forged bool
	// Catalog is the catalog it serves, by path (none when nil): each
	// file's lock and contents, under CatalogPolicy; CatalogExpiry is what
	// it says the catalog's expiry is. Announce publishes a version on the
	// watch stream.
	Catalog       map[string]CatalogFile
	CatalogPolicy map[string]any
	CatalogExpiry time.Time
	Announce      chan int
	FileCalls     int
	// quit ends the watch streams when the service closes, as a real
	// one's do when it shuts down.
	quit       chan struct{}
	quitOnce   sync.Once
	ModelCalls int
	ModelsFail bool
}

// NewService is a service whose identity provider is issuer, taking the
// ID tokens NewProvider gives; it names itself Acme Assist, offers the
// gateway and jobs, and signs Ana (ana@acme.com) in.
func NewService(t testing.TB, issuer string) *Service {
	_, key, _ := ed25519.GenerateKey(nil)
	s := &Service{Key: key, KeyID: "catalog-test", Accepts: map[string]bool{}, Announce: make(chan int, 4), quit: make(chan struct{}), issuer: issuer, sessions: map[string]string{}, Ended: map[string]bool{}, Access: map[string]string{}, ExpiresIn: 600}
	refuse := func(w http.ResponseWriter, status int, code, msg string) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": msg})
	}
	answer := func(w http.ResponseWriter, session string) {
		s.n++
		refresh, access := fmt.Sprintf("rt-%d", s.n), fmt.Sprintf("at-%d", s.n)
		s.sessions[refresh], s.Access[access] = session, session
		out := map[string]any{"access_token": access, "refresh_token": refresh, "expires_in": s.ExpiresIn,
			"person": map[string]any{"id": "p1", "email": "ana@acme.com", "name": "Ana", "roles": []string{"member"}}}
		if s.Terms != "" {
			out["terms"] = map[string]string{"version": s.Terms}
		}
		_ = json.NewEncoder(w).Encode(out)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/info", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"name":"Acme Assist","version":"1.0.0","oidc":{"issuer":%q,"client_id":"console",
			"desktop":{"client_id":"desktop","client_secret":"d-secret"},"device":{"client_id":"device","client_secret":"v-secret"}},
			"session_hours":12,"capabilities":["gateway","jobs","branding"],"catalog_keys":{%q:%q}}`, s.issuer, s.KeyID, base64.StdEncoding.EncodeToString(s.Key.Public().(ed25519.PublicKey)))
	})
	mux.HandleFunc("POST /api/v1/session", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ IDToken, Client string }
		_ = json.NewDecoder(r.Body).Decode(&struct {
			IDToken *string `json:"id_token"`
			Client  *string `json:"client"`
		}{&in.IDToken, &in.Client})
		s.Mu.Lock()
		defer s.Mu.Unlock()
		if in.IDToken != "id-for-desktop" && in.IDToken != "id-for-device" {
			refuse(w, http.StatusUnauthorized, "unauthenticated", "The sign-in couldn't be checked.")
			return
		}
		s.SignIns = append(s.SignIns, in.Client)
		if s.Tokenless {
			_, _ = w.Write([]byte(`{"person":{"id":"p1"}}`))
			return
		}
		answer(w, fmt.Sprintf("s%d", len(s.SignIns)))
	})
	mux.HandleFunc("POST /api/v1/session/refresh", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			RefreshToken string `json:"refresh_token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		s.Mu.Lock()
		defer s.Mu.Unlock()
		if s.Offline {
			refuse(w, http.StatusServiceUnavailable, "unavailable", "")
			return
		}
		s.Refreshes++
		session, ok := s.sessions[in.RefreshToken]
		if !ok || s.Ended[session] {
			refuse(w, http.StatusUnauthorized, "unauthenticated", "Sign in again.")
			return
		}
		if session == "" {
			refuse(w, http.StatusUnauthorized, "unauthenticated", "Sign in again.")
			return
		}
		s.sessions[in.RefreshToken] = ""
		answer(w, session)
	})
	mux.HandleFunc("GET /api/v1/models", func(w http.ResponseWriter, r *http.Request) {
		s.Mu.Lock()
		_, ok := s.Access[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		s.ModelCalls++
		s.Mu.Unlock()
		if !ok {
			refuse(w, http.StatusUnauthorized, "unauthenticated", "Sign in again.")
			return
		}
		if s.ModelsFail {
			refuse(w, http.StatusForbidden, "terms_required", "Accept the terms.")
			return
		}
		w.Header().Set("ETag", `"v1"`)
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = w.Write([]byte(`{"object":"list","data":[
			{"id":"fast","display_name":"Fast","vendor":"google","api":"responses","price":{"input_per_mtok":0.75,"output_per_mtok":3.75}},
			{"id":"smart","display_name":"Smart","vendor":"anthropic","api":"messages","default":true,"max_output_tokens":32000}]}`))
	})
	mux.HandleFunc("GET /api/v1/branding", func(w http.ResponseWriter, r *http.Request) {
		s.Mu.Lock()
		br := s.Branding
		s.Mu.Unlock()
		if br == nil {
			refuse(w, http.StatusNotFound, "not_found", "The organization hasn't published branding.")
			return
		}
		key := s.Key
		if s.Forged {
			_, key, _ = ed25519.GenerateKey(nil)
		}
		env := Seal(s.KeyID, key, br)
		sum := sha256.Sum256(env)
		etag := `"` + base64.RawURLEncoding.EncodeToString(sum[:9]) + `"`
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = w.Write(env)
	})
	signedIn := func(w http.ResponseWriter, r *http.Request) bool {
		s.Mu.Lock()
		_, ok := s.Access[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		s.Mu.Unlock()
		if !ok {
			refuse(w, http.StatusUnauthorized, "unauthenticated", "Sign in first.")
		}
		return ok
	}
	mux.HandleFunc("GET /api/v1/catalog", func(w http.ResponseWriter, r *http.Request) {
		if !signedIn(w, r) {
			return
		}
		s.Mu.Lock()
		defer s.Mu.Unlock()
		if s.Catalog == nil {
			refuse(w, http.StatusNotFound, "not_found", "The organization hasn't published a catalog yet.")
			return
		}
		var files []map[string]any
		paths := make([]string, 0, len(s.Catalog))
		for p := range s.Catalog {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			f := s.Catalog[p]
			sum := sha256.Sum256([]byte(f.Data))
			files = append(files, map[string]any{"kind": f.Kind, "name": f.Name, "path": p, "lock": f.Lock, "sha256": hex.EncodeToString(sum[:]), "size": len(f.Data)})
		}
		m := map[string]any{"version": len(paths), "files": files, "policy": s.CatalogPolicy, "expires": s.CatalogExpiry}
		key := s.Key
		if s.Forged {
			_, key, _ = ed25519.GenerateKey(nil)
		}
		env := Seal(s.KeyID, key, m)
		sum := sha256.Sum256(env)
		etag := `"` + hex.EncodeToString(sum[:8]) + `"`
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = w.Write(env)
	})
	mux.HandleFunc("GET /api/v1/catalog/files/{sha}", func(w http.ResponseWriter, r *http.Request) {
		if !signedIn(w, r) {
			return
		}
		s.Mu.Lock()
		defer s.Mu.Unlock()
		s.FileCalls++
		for _, f := range s.Catalog {
			if sum := sha256.Sum256([]byte(f.Data)); hex.EncodeToString(sum[:]) == r.PathValue("sha") {
				data := f.Data
				if f.Corrupt {
					data += "!"
				}
				_, _ = w.Write([]byte(data))
				return
			}
		}
		refuse(w, http.StatusNotFound, "not_found", "There's no such file in your catalog.")
	})
	mux.HandleFunc("GET /api/v1/catalog/watch", func(w http.ResponseWriter, r *http.Request) {
		if !signedIn(w, r) {
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(": watching\n\n"))
		w.(http.Flusher).Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-s.quit:
				return
			case v, ok := <-s.Announce:
				if !ok {
					return
				}
				fmt.Fprintf(w, "event: catalog\ndata: {\"version\":%d}\n\n", v)
				w.(http.Flusher).Flush()
			}
		}
	})
	mux.HandleFunc("GET /api/v1/terms", func(w http.ResponseWriter, r *http.Request) {
		s.Mu.Lock()
		defer s.Mu.Unlock()
		session, ok := s.Access[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		if !ok {
			refuse(w, http.StatusUnauthorized, "unauthenticated", "Sign in first.")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"version": s.Version, "accepted": s.Version == "" || s.Accepts[session+s.Version],
			"text": "Be kind.", "url": ""})
	})
	mux.HandleFunc("POST /api/v1/terms/accept", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Version string `json:"version"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		s.Mu.Lock()
		defer s.Mu.Unlock()
		session, ok := s.Access[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		switch {
		case !ok:
			refuse(w, http.StatusUnauthorized, "unauthenticated", "Sign in first.")
		case in.Version != s.Version:
			refuse(w, http.StatusConflict, "conflict", "That isn't the version of the terms in force: fetch it again.")
		default:
			s.Accepts[session+in.Version] = true
			w.WriteHeader(http.StatusNoContent)
		}
	})
	mux.HandleFunc("DELETE /api/v1/session", func(w http.ResponseWriter, r *http.Request) {
		s.Mu.Lock()
		defer s.Mu.Unlock()
		session, ok := s.Access[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		if !ok || s.Ended[session] {
			refuse(w, http.StatusUnauthorized, "unauthenticated", "Sign in again.")
			return
		}
		s.Ended[session] = true
		w.WriteHeader(http.StatusNoContent)
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

// Browse follows a sign-in address as the browser would: the provider
// redirects to Blitz's callback on this computer.
func Browse(t testing.TB) func(string) {
	return func(u string) {
		go func() {
			res, err := http.Get(u)
			if assert.NoError(t, err) {
				res.Body.Close()
				assert.Equal(t, http.StatusOK, res.StatusCode)
			}
		}()
	}
}

// Seal signs v as the service does: its JSON in base64url, signed with
// Ed25519 over "blitz-enterprise/v1", NUL, the key's ID, NUL and the
// payload.
func Seal(kid string, key ed25519.PrivateKey, v any) []byte {
	payload, _ := json.Marshal(v)
	signed := append([]byte("blitz-enterprise/v1\x00"+kid+"\x00"), payload...)
	env, _ := json.Marshal(map[string]string{"kid": kid, "payload": base64.RawURLEncoding.EncodeToString(payload),
		"signature": base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, signed))})
	return env
}

// CatalogFile is a file of the fake service's catalog.
type CatalogFile struct {
	Kind, Name, Lock, Data string
	// Corrupt serves contents that don't match the file's hash.
	Corrupt bool
}

// Close ends the watch streams, then the server.
func (s *Service) Close() {
	s.quitOnce.Do(func() { close(s.quit) })
	s.Server.Close()
}
