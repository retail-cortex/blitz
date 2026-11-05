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
	"cmp"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/retail-cortex/blitz/pkg/jobspec"
	"github.com/retail-cortex/blitz/pkg/urn"
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
	// ListDown makes listing the workspaces fail (503), as a service
	// that's slow or restarting does; set with Mu held.
	ListDown  bool
	Tokenless bool // a sign-in or a run's token answered without tokens
	// Key signs the branding; Branding is what it serves (none when nil),
	// and Terms the version in force, accepted or not, by person.
	Key      ed25519.PrivateKey
	KeyID    string
	Branding map[string]any
	Version  string
	accepts  map[string]bool
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
	// Workspaces are the organization's workspaces, by handle, each a bare
	// repository in repos served through Git's smart HTTP at /git/, which
	// takes the access token as the password, as the service's proxy does.
	Workspaces map[string]Workspace
	repos      string
	// Pushes announces a push to a workspace, by handle, on its watch
	// stream.
	Pushes chan string
	// quit ends the watch streams when the service closes, as a real
	// one's do when it shuts down.
	quit       chan struct{}
	quitOnce   sync.Once
	ModelCalls int
	ModelsFail bool
	// Runs are jobs' runs, by ID, each with its secret: a token for the
	// secret is an access token as Ana; Ends are how they ended, as each
	// runner said (the secret is no good after).
	Runs map[string]string
	Ends map[string]map[string]any
	// Kept are the files each run kept, by run and path; KeepRefused
	// refuses them all, as a service without a store does.
	Kept        map[string]map[string]Artifact
	KeepRefused bool
	// MaxRunFiles is how many files the service keeps a run, a new one
	// past it refused (blitz-enterprise's jobs.MaxRunFiles, 1,000, when 0).
	MaxRunFiles int
	// ServiceJobs are the jobs the service runs (GET /api/v1/jobs);
	// ServiceRuns their runs, by job ID, newest first; Files the files
	// under each resource name, and Contents each file's, by its name.
	// A job run now gets a running run; JobsRefused refuses every jobs
	// call as a license without jobs does.
	ServiceJobs []ServiceJob
	// Reads are the workspaces whose main was read on request, in order
	// (POST /api/v1/workspaces/{handle}/jobs/read).
	Reads       []string
	ServiceRuns map[string][]ServiceRun
	Files       map[string][]File
	Contents    map[string]string
	JobsRefused bool
	// Going are the runs going now (GET /api/v1/runs); RunChanges are
	// sent, each as it comes, on every runs' stream.
	Going      []GoingRun
	RunChanges chan RunChange
	// EndRunStreams ends the runs' streams open now, as a service that
	// restarts does.
	EndRunStreams chan struct{}
	// Waiting are the runs paused for the person's answer; Answers each
	// answer given, by run.
	Waiting []GoingRun
	Answers map[string]map[string]any
	// Usage is GET /api/v1/usage's answer, as JSON.
	Usage map[string]any
	// Notices are the person's notice settings (JB-31).
	Notices map[string]any
	// Reviews are the catalog's reviews (POST makes one, open, with each
	// file kept in Published); NotAdmin refuses them as to a member.
	Reviews   []map[string]any
	Published map[string]string
	NotAdmin  bool
}

// GoingRun is a run going now, with its job and workspace.
type GoingRun struct {
	ServiceRun
	Job       string `json:"job"`
	JobName   string `json:"job_name"`
	Workspace string `json:"workspace"`
}

// RunChange is one run's change on the runs' stream.
type RunChange struct {
	Run       string `json:"run"`
	Job       string `json:"job"`
	Workspace string `json:"workspace"`
	Status    string `json:"status"`
}

// ServiceJob is a job the service runs, as GET /api/v1/jobs answers it.
type ServiceJob struct {
	ID        string `json:"id"`
	URN       string `json:"urn"`
	Workspace string `json:"workspace"`
	Name      string `json:"name"`
	Schedule  string `json:"schedule"`
	Enabled   bool   `json:"enabled"`
	// NextRun is when it runs next (nil: not scheduled).
	NextRun *time.Time `json:"next_run,omitempty"`
}

// ServiceRun is one of its runs.
type ServiceRun struct {
	Pending   map[string]string `json:"pending,omitempty"`
	ID        string            `json:"id"`
	URN       string            `json:"urn"`
	Trigger   string            `json:"trigger"`
	Status    string            `json:"status"`
	Note      string            `json:"note,omitempty"`
	Output    string            `json:"output,omitempty"`
	CostUSD   float64           `json:"cost_usd"`
	Created   time.Time         `json:"created"`
	Branch    string            `json:"branch,omitempty"`
	Artifacts string            `json:"artifacts,omitempty"`
	Merged    bool              `json:"merged,omitempty"`
	MergeNote string            `json:"merge_note,omitempty"`
}

// File is a file under a resource name.
type File struct {
	URN       string `json:"urn"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	MediaType string `json:"media_type"`
	Size      int64  `json:"size"`
}

// Artifact is one file a run kept.
type Artifact struct {
	MediaType, Body string
}

// NewService is a service whose identity provider is issuer, taking the
// ID tokens NewProvider gives; it names itself Acme Assist, offers the
// gateway and jobs, and signs Ana (ana@acme.com) in.
func NewService(t testing.TB, issuer string) *Service {
	_, key, _ := ed25519.GenerateKey(nil)
	s := &Service{Key: key, KeyID: "catalog-test", accepts: map[string]bool{}, Announce: make(chan int, 4), quit: make(chan struct{}), Workspaces: map[string]Workspace{}, Runs: map[string]string{}, repos: t.TempDir(), Pushes: make(chan string, 4), RunChanges: make(chan RunChange, 4), EndRunStreams: make(chan struct{}, 4), issuer: issuer, sessions: map[string]string{}, Ended: map[string]bool{}, Access: map[string]string{}, ExpiresIn: 600}
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
			{"id":"smart","display_name":"Smart","vendor":"anthropic","api":"messages","default":true,"max_output_tokens":32000,
			 "capabilities":{"tools":true,"images":true,"thinking":true,"context":200000}}]}`))
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
		env := seal(s.KeyID, key, br)
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
			files = append(files, map[string]any{"kind": f.Kind, "name": f.Name, "path": p, "lock": f.Lock, "groups": f.Groups, "sha256": hex.EncodeToString(sum[:]), "size": len(f.Data)})
		}
		m := map[string]any{"version": len(paths), "files": files, "policy": s.CatalogPolicy, "expires": s.CatalogExpiry}
		key := s.Key
		if s.Forged {
			_, key, _ = ed25519.GenerateKey(nil)
		}
		env := seal(s.KeyID, key, m)
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
			if sum := sha256.Sum256([]byte(f.Data)); hex.EncodeToString(sum[:]) == r.PathValue("sha") && !f.Missing {
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
	view := func(handle string, ws Workspace) map[string]any {
		return map[string]any{"workspace": map[string]any{"id": WorkspaceID(handle), "handle": handle, "name": ws.Name, "role": ws.Role, "sync": ws.Sync}, "clone_url": s.URL + "/git/" + handle + ".git"}
	}
	mux.HandleFunc("GET /api/v1/workspaces", func(w http.ResponseWriter, r *http.Request) {
		if !signedIn(w, r) {
			return
		}
		s.Mu.Lock()
		defer s.Mu.Unlock()
		if s.ListDown {
			refuse(w, http.StatusServiceUnavailable, "unavailable", "")
			return
		}
		handles := make([]string, 0, len(s.Workspaces))
		for h := range s.Workspaces {
			handles = append(handles, h)
		}
		sort.Strings(handles)
		out := []map[string]any{}
		for _, h := range handles {
			out = append(out, view(h, s.Workspaces[h]))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"workspaces": out})
	})
	mux.HandleFunc("POST /api/v1/workspaces", func(w http.ResponseWriter, r *http.Request) {
		if !signedIn(w, r) {
			return
		}
		var in struct {
			Name, Handle string
			Personal     bool
		}
		_ = json.NewDecoder(r.Body).Decode(&struct {
			Name     *string `json:"name"`
			Handle   *string `json:"handle"`
			Personal *bool   `json:"personal"`
		}{&in.Name, &in.Handle, &in.Personal})
		if in.Handle == "" {
			in.Handle = strings.ToLower(strings.ReplaceAll(in.Name, " ", "-"))
		}
		if in.Personal {
			in.Handle = "~ana/" + in.Handle
		}
		s.Mu.Lock()
		_, taken := s.Workspaces[in.Handle]
		s.Mu.Unlock()
		if taken {
			refuse(w, http.StatusConflict, "taken", "a workspace has that handle already")
			return
		}
		ws := Workspace{Name: in.Name, Role: "owner", Sync: true}
		// Empty, as the service makes a new workspace's.
		if err := s.makeRepo(in.Handle, false); err != nil {
			refuse(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		s.Mu.Lock()
		s.Workspaces[in.Handle] = ws
		s.Mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(view(in.Handle, ws))
	})
	mux.HandleFunc("GET /api/v1/workspaces/{handle}/watch", func(w http.ResponseWriter, r *http.Request) {
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
			case h := <-s.Pushes:
				if h == r.PathValue("handle") {
					fmt.Fprintf(w, "event: push\ndata: {\"ref\":\"refs/heads/main\",\"commit\":\"c\",\"by\":\"bo@acme.com\"}\n\n")
					w.(http.Flusher).Flush()
				}
			}
		}
	})
	if git, err := exec.LookPath("git"); err == nil {
		backend := &cgi.Handler{Path: git, Args: []string{"http-backend"}, Root: "/git", Env: []string{"GIT_PROJECT_ROOT=" + s.repos, "GIT_HTTP_EXPORT_ALL=1"}}
		mux.HandleFunc("/git/", func(w http.ResponseWriter, r *http.Request) {
			_, pass, _ := r.BasicAuth()
			s.Mu.Lock()
			_, ok := s.Access[pass]
			s.Mu.Unlock()
			if !ok {
				w.Header().Set("WWW-Authenticate", `Basic realm="Blitz"`)
				http.Error(w, "sign in", http.StatusUnauthorized)
				return
			}
			backend.ServeHTTP(w, r)
		})
	}
	mux.HandleFunc("GET /api/v1/terms", func(w http.ResponseWriter, r *http.Request) {
		s.Mu.Lock()
		defer s.Mu.Unlock()
		session, ok := s.Access[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		if !ok {
			refuse(w, http.StatusUnauthorized, "unauthenticated", "Sign in first.")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"version": s.Version, "accepted": s.Version == "" || s.accepts[session+s.Version],
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
			s.accepts[session+in.Version] = true
			w.WriteHeader(http.StatusNoContent)
		}
	})
	run := func(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
		var in map[string]any
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			refuse(w, http.StatusBadRequest, "bad_request", "The request isn't the JSON expected.")
			return nil, false
		}
		id := r.PathValue("id")
		if secret, ok := s.Runs[id]; !ok || secret == "" || in["secret"] != secret {
			refuse(w, http.StatusUnauthorized, "unauthenticated", "That isn't this run's secret, or the run is over.")
			return nil, false
		}
		return in, true
	}
	mux.HandleFunc("POST /api/v1/runs/{id}/token", func(w http.ResponseWriter, r *http.Request) {
		s.Mu.Lock()
		defer s.Mu.Unlock()
		if _, ok := run(w, r); !ok {
			return
		}
		s.n++
		if s.Tokenless {
			_ = json.NewEncoder(w).Encode(map[string]any{"expires_in": s.ExpiresIn})
			return
		}
		access := fmt.Sprintf("at-%d", s.n)
		s.Access[access] = "run:" + r.PathValue("id")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": access, "expires_in": s.ExpiresIn})
	})
	mux.HandleFunc("POST /api/v1/runs/{id}/end", func(w http.ResponseWriter, r *http.Request) {
		s.Mu.Lock()
		defer s.Mu.Unlock()
		in, ok := run(w, r)
		if !ok {
			return
		}
		switch in["status"] {
		case "succeeded", "failed", "stopped":
		case "paused": // waiting for its owner's answer, saying for what
			if in["pending"] == nil || in["session"] == "" || in["session"] == nil {
				refuse(w, http.StatusBadRequest, "bad_request", "A paused run says what it asks for and its session.")
				return
			}
		default:
			refuse(w, http.StatusBadRequest, "bad_request", "A run ends succeeded, failed, stopped or paused.")
			return
		}
		delete(in, "secret")
		if s.Ends == nil {
			s.Ends = map[string]map[string]any{}
		}
		s.Ends[r.PathValue("id")] = in
		s.Runs[r.PathValue("id")] = ""
		w.WriteHeader(http.StatusNoContent)
	})
	// jobs checks the sign-in, then takes s.Mu (held when it answers true).
	jobs := func(w http.ResponseWriter, r *http.Request) bool {
		if !signedIn(w, r) {
			return false
		}
		s.Mu.Lock()
		if s.JobsRefused {
			s.Mu.Unlock()
			refuse(w, http.StatusNotFound, "feature_off", "This service's license doesn't carry jobs.")
			return false
		}
		return true
	}
	job := func(w http.ResponseWriter, r *http.Request) (int, bool) {
		for i, j := range s.ServiceJobs {
			if j.ID == r.PathValue("id") {
				return i, true
			}
		}
		refuse(w, http.StatusNotFound, "not_found", "There's no such job.")
		return 0, false
	}
	mux.HandleFunc("GET /api/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		if jobs(w, r) {
			defer s.Mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"jobs": append([]ServiceJob{}, s.ServiceJobs...)})
		}
	})
	// Read a workspace's main now, as the service does: its WORKER.md files
	// with runs_on: service become jobs; the others are said, with why.
	mux.HandleFunc("POST /api/v1/workspaces/{handle}/jobs/read", func(w http.ResponseWriter, r *http.Request) {
		if !jobs(w, r) {
			return
		}
		defer s.Mu.Unlock()
		handle := r.PathValue("handle")
		if _, ok := s.Workspaces[handle]; !ok {
			refuse(w, http.StatusNotFound, "not_found", "There's no such workspace.")
			return
		}
		files, err := mainJobs(s.Repo(handle))
		if err != nil {
			refuse(w, http.StatusBadGateway, "unreadable", "The service couldn't read the workspace's main: "+err.Error())
			return
		}
		s.Reads = append(s.Reads, handle)
		unrun, out := map[string]string{}, []ServiceJob{}
		for name, data := range files {
			spec, err := jobspec.Parse(data)
			switch {
			case err != nil:
				unrun[name] = "its WORKER.md doesn't read: " + err.Error()
				continue
			case spec.RunsOn != jobspec.Service:
				unrun[name] = "its WORKER.md doesn't say runs_on: service, so it runs on people's computers"
				continue
			}
			if !slices.ContainsFunc(s.ServiceJobs, func(j ServiceJob) bool { return j.Workspace == handle && j.Name == name }) {
				id := "job-" + name
				s.ServiceJobs = append(s.ServiceJobs, ServiceJob{ID: id, URN: "urn:blitz:jobs:" + id, Workspace: handle, Name: name, Schedule: spec.Schedule, Enabled: true})
			}
		}
		for _, j := range s.ServiceJobs {
			if j.Workspace == handle {
				out = append(out, j)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jobs": out, "skipped": unrun})
	})
	mux.HandleFunc("GET /api/v1/runs", func(w http.ResponseWriter, r *http.Request) {
		if jobs(w, r) {
			defer s.Mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"runs": append([]GoingRun{}, s.Going...)})
		}
	})
	admin := func(w http.ResponseWriter, r *http.Request) bool {
		if !signedIn(w, r) {
			return false
		}
		if s.NotAdmin {
			refuse(w, http.StatusForbidden, "forbidden", "That's for the organization's Admins.")
			return false
		}
		return true
	}
	mux.HandleFunc("POST /api/v1/admin/catalog/reviews", func(w http.ResponseWriter, r *http.Request) {
		if !admin(w, r) {
			return
		}
		var in struct {
			Title string
			Files []struct{ Path, Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || len(in.Files) == 0 {
			refuse(w, http.StatusBadRequest, "bad_request", "Publish from 1 to 200 files.")
			return
		}
		s.Mu.Lock()
		defer s.Mu.Unlock()
		if s.Published == nil {
			s.Published = map[string]string{}
		}
		var paths []string
		for _, f := range in.Files {
			s.Published[f.Path] = f.Content
			paths = append(paths, f.Path)
		}
		sort.Strings(paths)
		s.n++
		rev := map[string]any{"id": fmt.Sprintf("rev-%d", s.n), "title": in.Title, "by_email": "ana@acme.com", "branch": "publish/ana/x",
			"paths": paths, "problems": []string{}, "state": "open", "created": time.Now()}
		s.Reviews = append([]map[string]any{rev}, s.Reviews...)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(rev)
	})
	mux.HandleFunc("GET /api/v1/admin/catalog/reviews", func(w http.ResponseWriter, r *http.Request) {
		if !admin(w, r) {
			return
		}
		s.Mu.Lock()
		defer s.Mu.Unlock()
		out := []map[string]any{}
		for _, rev := range s.Reviews {
			if st := r.URL.Query().Get("state"); st == "" || rev["state"] == st {
				out = append(out, rev)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"reviews": out})
	})
	mux.HandleFunc("GET /api/v1/usage", func(w http.ResponseWriter, r *http.Request) {
		if !signedIn(w, r) {
			return
		}
		s.Mu.Lock()
		defer s.Mu.Unlock()
		u := s.Usage
		if u == nil {
			u = map[string]any{"today": map[string]any{}, "month": map[string]any{}, "limits": []any{}, "rate_limits": []any{}, "prompt_logs": false}
		}
		_ = json.NewEncoder(w).Encode(u)
	})
	mux.HandleFunc("GET /api/v1/notices", func(w http.ResponseWriter, r *http.Request) {
		if !signedIn(w, r) {
			return
		}
		s.Mu.Lock()
		defer s.Mu.Unlock()
		n := s.Notices
		if n == nil {
			n = map[string]any{"email": true, "webhook": "", "every_run": false}
		}
		out := maps.Clone(n)
		out["email_available"] = true
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("PUT /api/v1/notices", func(w http.ResponseWriter, r *http.Request) {
		if !signedIn(w, r) {
			return
		}
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		if hook, _ := in["webhook"].(string); hook != "" && !strings.HasPrefix(hook, "https://") {
			refuse(w, http.StatusBadRequest, "bad_request", "a webhook is an https:// address")
			return
		}
		s.Mu.Lock()
		s.Notices = in
		s.Mu.Unlock()
		out := maps.Clone(in)
		out["email_available"] = true
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("GET /api/v1/runs/pending", func(w http.ResponseWriter, r *http.Request) {
		if jobs(w, r) {
			defer s.Mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"runs": append([]GoingRun{}, s.Waiting...)})
		}
	})
	mux.HandleFunc("POST /api/v1/runs/{id}/answer", func(w http.ResponseWriter, r *http.Request) {
		if !jobs(w, r) {
			return
		}
		defer s.Mu.Unlock()
		var in map[string]any
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in["approve"] == nil {
			refuse(w, http.StatusBadRequest, "bad_request", `Send {"approve": true or false, "note": "…"}.`)
			return
		}
		for i, g := range s.Waiting {
			if g.ID == r.PathValue("id") {
				s.Waiting = append(s.Waiting[:i:i], s.Waiting[i+1:]...)
				if s.Answers == nil {
					s.Answers = map[string]map[string]any{}
				}
				s.Answers[g.ID] = in
				g.ServiceRun.Status, g.ServiceRun.Pending = "running", nil
				_ = json.NewEncoder(w).Encode(g.ServiceRun)
				return
			}
		}
		refuse(w, http.StatusConflict, "answered", "That run isn't waiting for an answer.")
	})
	mux.HandleFunc("GET /api/v1/runs/watch", func(w http.ResponseWriter, r *http.Request) {
		if !jobs(w, r) {
			return
		}
		s.Mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(": watching\n\n"))
		w.(http.Flusher).Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-s.quit:
				return
			case <-s.EndRunStreams:
				return
			case c := <-s.RunChanges:
				data, _ := json.Marshal(c)
				fmt.Fprintf(w, "event: run\ndata: %s\n\n", data)
				w.(http.Flusher).Flush()
			}
		}
	})
	mux.HandleFunc("GET /api/v1/jobs/{id}/runs", func(w http.ResponseWriter, r *http.Request) {
		if !jobs(w, r) {
			return
		}
		defer s.Mu.Unlock()
		if _, ok := job(w, r); ok {
			_ = json.NewEncoder(w).Encode(map[string]any{"runs": append([]ServiceRun{}, s.ServiceRuns[r.PathValue("id")]...)})
		}
	})
	mux.HandleFunc("POST /api/v1/jobs/{id}/run", func(w http.ResponseWriter, r *http.Request) {
		if !jobs(w, r) {
			return
		}
		defer s.Mu.Unlock()
		if _, ok := job(w, r); ok {
			s.n++
			run := ServiceRun{ID: fmt.Sprintf("run-%d", s.n), Trigger: "now", Status: "running", Created: time.Now()}
			if s.ServiceRuns == nil {
				s.ServiceRuns = map[string][]ServiceRun{}
			}
			id := r.PathValue("id")
			s.ServiceRuns[id] = append([]ServiceRun{run}, s.ServiceRuns[id]...)
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(run)
		}
	})
	mux.HandleFunc("POST /api/v1/jobs/{id}/runs/{run}/merged", func(w http.ResponseWriter, r *http.Request) {
		if !jobs(w, r) {
			return
		}
		defer s.Mu.Unlock()
		if _, ok := job(w, r); ok {
			runs := s.ServiceRuns[r.PathValue("id")]
			for i := range runs {
				if runs[i].ID == r.PathValue("run") && runs[i].Branch != "" {
					runs[i].Merged, runs[i].MergeNote = true, ""
					_ = json.NewEncoder(w).Encode(runs[i])
					return
				}
			}
			refuse(w, http.StatusNotFound, "not_found", "That job has no such run with changes.")
		}
	})
	mux.HandleFunc("PUT /api/v1/jobs/{id}/enabled", func(w http.ResponseWriter, r *http.Request) {
		if !jobs(w, r) {
			return
		}
		defer s.Mu.Unlock()
		var in struct {
			Enabled *bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Enabled == nil {
			refuse(w, http.StatusBadRequest, "bad_request", "The request isn't the JSON expected.")
			return
		}
		if i, ok := job(w, r); ok {
			s.ServiceJobs[i].Enabled = *in.Enabled
			w.WriteHeader(http.StatusNoContent)
		}
	})
	mux.HandleFunc("GET /api/v1/resources/{urn...}", func(w http.ResponseWriter, r *http.Request) {
		if !signedIn(w, r) {
			return
		}
		s.Mu.Lock()
		defer s.Mu.Unlock()
		name := strings.TrimPrefix(r.URL.EscapedPath(), "/api/v1/resources/")
		s.keptFiles(name)
		switch q := r.URL.Query(); {
		case q.Has("content"):
			body, ok := s.Contents[name]
			if !ok {
				refuse(w, http.StatusNotFound, "not_found", "There's no such resource.")
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = io.WriteString(w, body)
		case q.Has("children"):
			files, ok := s.Files[name]
			if !ok {
				refuse(w, http.StatusNotFound, "not_found", "There's no such resource.")
				return
			}
			// Two at a time, to page.
			from, _ := strconv.Atoi(q.Get("next"))
			from = min(from, len(files))
			to := min(from+2, len(files))
			out := map[string]any{"items": append([]File{}, files[from:to]...)}
			if to < len(files) {
				out["next"] = strconv.Itoa(to)
			}
			_ = json.NewEncoder(w).Encode(out)
		default:
			refuse(w, http.StatusBadRequest, "bad_request", "Ask for children or content.")
		}
	})
	mux.HandleFunc("PUT /api/v1/runs/{id}/artifacts/{path...}", func(w http.ResponseWriter, r *http.Request) {
		s.Mu.Lock()
		defer s.Mu.Unlock()
		id := r.PathValue("id")
		if secret, ok := s.Runs[id]; !ok || secret == "" || r.Header.Get("Blitz-Run-Secret") != secret {
			refuse(w, http.StatusUnauthorized, "unauthenticated", "That isn't this run's secret, or the run is over.")
			return
		}
		if s.KeepRefused {
			refuse(w, http.StatusNotImplemented, "no_store", "This service keeps no artifacts.")
			return
		}
		body, _ := io.ReadAll(r.Body)
		if s.Kept == nil {
			s.Kept = map[string]map[string]Artifact{}
		}
		if s.Kept[id] == nil {
			s.Kept[id] = map[string]Artifact{}
		}
		if _, again := s.Kept[id][r.PathValue("path")]; !again && len(s.Kept[id]) >= cmp.Or(s.MaxRunFiles, 1000) {
			refuse(w, http.StatusRequestEntityTooLarge, "too_many", fmt.Sprintf("A run keeps at most %d files.", cmp.Or(s.MaxRunFiles, 1000)))
			return
		}
		s.Kept[id][r.PathValue("path")] = Artifact{MediaType: r.Header.Get("Content-Type"), Body: string(body)}
		w.WriteHeader(http.StatusCreated)
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

// seal signs v as the service does: its JSON in base64url, signed with
// Ed25519 over "blitz-enterprise/v1", NUL, the key's ID, NUL and the
// payload.
func seal(kid string, key ed25519.PrivateKey, v any) []byte {
	payload, _ := json.Marshal(v)
	signed := append([]byte("blitz-enterprise/v1\x00"+kid+"\x00"), payload...)
	env, _ := json.Marshal(map[string]string{"kid": kid, "payload": base64.RawURLEncoding.EncodeToString(payload),
		"signature": base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, signed))})
	return env
}

// CatalogFile is a file of the fake service's catalog.
type CatalogFile struct {
	Kind, Name, Lock, Data string
	// Groups are who it's for (everyone when none).
	Groups []string
	// Corrupt serves contents that don't match the file's hash; Missing
	// doesn't serve them at all.
	Corrupt, Missing bool
}

// Close ends the watch streams, then the server.
func (s *Service) Close() {
	s.quitOnce.Do(func() { close(s.quit) })
	s.Server.Close()
}

// Workspace is one of the fake service's workspaces.
type Workspace struct {
	Name, Role string
	Sync       bool
}

// AddWorkspace makes a workspace with a first commit (README.md), with
// the person in role.
func (s *Service) AddWorkspace(t testing.TB, handle, name, role string) {
	t.Helper()
	if err := s.addRepo(handle); err != nil {
		t.Fatal(err)
	}
	s.Mu.Lock()
	s.Workspaces[handle] = Workspace{Name: name, Role: role, Sync: true}
	s.Mu.Unlock()
}

// WorkspaceID is the fake service's ID for the workspace handle.
func WorkspaceID(handle string) string {
	return "ws-" + strings.NewReplacer("~", "", "/", "-").Replace(handle)
}

// addRepo makes handle's bare repository, with a first commit.
func (s *Service) addRepo(handle string) error { return s.makeRepo(handle, true) }

// makeRepo makes handle's bare repository, with a first commit when first
// (an empty one, as the service makes for a new workspace, when not).
func (s *Service) makeRepo(handle string, first bool) error {
	bare := filepath.Join(s.repos, filepath.FromSlash(handle)+".git")
	work, err := os.MkdirTemp("", "ws")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	// Each step: the folder it runs in, then git's arguments.
	steps := [][]string{{s.repos, "init", "--bare", bare}}
	if first {
		if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("# "+handle+"\n"), 0o600); err != nil {
			return err
		}
		steps = append(steps, []string{work, "init"}, []string{work, "add", "-A"}, []string{work, "commit", "-m", "First"}, []string{work, "push", bare, "HEAD:main"})
	}
	steps = append(steps, []string{bare, "config", "http.receivepack", "true"}, []string{bare, "symbolic-ref", "HEAD", "refs/heads/main"})
	for _, st := range steps {
		cmd := exec.Command("git", append([]string{"-c", "user.name=Ana", "-c", "user.email=ana@acme.com", "-c", "init.defaultBranch=main"}, st[1:]...)...)
		cmd.Dir = st[0]
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %v: %v: %s", st[1:], err, out)
		}
	}
	return nil
}

// mainJobs are the WORKER.md files on main in the bare repository, by
// their jobs' names.
func mainJobs(bare string) (map[string][]byte, error) {
	out := map[string][]byte{}
	list, err := exec.Command("git", "--git-dir", bare, "ls-tree", "-r", "--name-only", "main", "--", ".agents/workers").Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-tree: %w", err)
	}
	for _, p := range strings.Fields(string(list)) {
		rest, ok := strings.CutPrefix(p, ".agents/workers/")
		name, file, _ := strings.Cut(rest, "/")
		if !ok || file != "WORKER.md" {
			continue
		}
		data, err := exec.Command("git", "--git-dir", bare, "show", "main:"+p).Output()
		if err != nil {
			return nil, fmt.Errorf("git show: %w", err)
		}
		out[name] = data
	}
	return out, nil
}

// Repo is the bare repository of a workspace, for a test to look in.
func (s *Service) Repo(handle string) string {
	return filepath.Join(s.repos, filepath.FromSlash(handle)+".git")
}

// keptFiles adds what a run kept (Kept) under its artifacts' name to Files
// and Contents, as the service serves them back; s.Mu is held.
func (s *Service) keptFiles(name string) {
	n, err := urn.Parse(name)
	if err != nil || n.Kind != urn.Artifacts || len(n.IDs) != 3 {
		return
	}
	set, _ := urn.New(urn.Artifacts, n.IDs...)
	for path, a := range s.Kept[n.IDs[2]] {
		f, err := set.File(path)
		if err != nil {
			continue
		}
		if s.Files == nil {
			s.Files = map[string][]File{}
		}
		if s.Contents == nil {
			s.Contents = map[string]string{}
		}
		if _, ok := s.Contents[f.String()]; !ok {
			s.Files[set.String()] = append(s.Files[set.String()], File{URN: f.String(), Kind: "artifacts", Title: path, MediaType: a.MediaType, Size: int64(len(a.Body))})
		}
		s.Contents[f.String()] = a.Body
	}
}
