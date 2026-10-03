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

// Package mcpauth signs Blitz in to MCP servers over HTTP with OAuth
// (spec_parity_027 PAR-MCP-04): the authorization-code flow with PKCE,
// through a local callback or a code pasted over SSH, with the client
// registered dynamically. The tokens and what refreshing them needs are
// kept in the secret store (the OS keychain, else an owner-only file),
// refreshed as they expire, and never shown to the model.
package mcpauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/retail-cortex/blitz/pkg/secrets"
	"golang.org/x/oauth2"
)

// ErrNotSignedIn means no sign-in is stored for the server.
var ErrNotSignedIn = errors.New("not signed in")

// Record is what a server's sign-in keeps: the client Blitz registered,
// the authorization server's endpoints, and the latest token.
type Record struct {
	ClientID     string        `json:"client_id"`
	ClientSecret string        `json:"client_secret,omitempty"`
	AuthURL      string        `json:"auth_url"`
	TokenURL     string        `json:"token_url"`
	RedirectURL  string        `json:"redirect_url"`
	Scopes       []string      `json:"scopes,omitempty"`
	Token        *oauth2.Token `json:"token"`
}

// secretName is where server's sign-in is kept.
func secretName(server string) string { return "mcp-oauth-" + server }

// Load reads server's sign-in (ErrNotSignedIn when there's none).
func Load(store secrets.Store, server string) (*Record, error) {
	v, err := store.Get(secretName(server))
	if errors.Is(err, secrets.ErrNotFound) {
		return nil, ErrNotSignedIn
	}
	if err != nil {
		return nil, err
	}
	var r Record
	if err := json.Unmarshal([]byte(v), &r); err != nil || r.Token == nil {
		return nil, ErrNotSignedIn
	}
	return &r, nil
}

// Save keeps server's sign-in.
func Save(store secrets.Store, server string, r *Record) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return store.Set(secretName(server), string(data))
}

// Forget removes server's sign-in.
func Forget(store secrets.Store, server string) error {
	err := store.Delete(secretName(server))
	if errors.Is(err, secrets.ErrNotFound) {
		return nil
	}
	return err
}

func (r *Record) config() *oauth2.Config {
	return &oauth2.Config{
		ClientID: r.ClientID, ClientSecret: r.ClientSecret, RedirectURL: r.RedirectURL, Scopes: r.Scopes,
		Endpoint: oauth2.Endpoint{AuthURL: r.AuthURL, TokenURL: r.TokenURL},
	}
}

// saving is a token source that keeps each new token (a refresh) in the
// store, so the next process starts from it.
type saving struct {
	mu     sync.Mutex
	base   oauth2.TokenSource
	last   string
	record Record
	store  secrets.Store
	server string
}

func (s *saving) Token() (*oauth2.Token, error) {
	t, err := s.base.Token()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.AccessToken != s.last {
		s.last = t.AccessToken
		s.record.Token = t
		_ = Save(s.store, s.server, &s.record) // the token works either way
	}
	return t, nil
}

func newSaving(ctx context.Context, store secrets.Store, server string, r Record) *saving {
	return &saving{base: r.config().TokenSource(ctx, r.Token), last: r.Token.AccessToken, record: r, store: store, server: server}
}

// Handler is the OAuth handler for server in a running workspace: its
// stored token, refreshed and kept as it expires. A server that asks to
// sign in (again) is told to run blitz mcp login.
func Handler(store secrets.Store, server string, r *Record) auth.OAuthHandler {
	return &storedHandler{server: server, source: newSaving(context.Background(), store, server, *r)}
}

type storedHandler struct {
	server string
	source oauth2.TokenSource
}

func (h *storedHandler) TokenSource(context.Context) (oauth2.TokenSource, error) {
	return h.source, nil
}

func (h *storedHandler) Authorize(_ context.Context, _ *http.Request, resp *http.Response) error {
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	return fmt.Errorf("MCP server %q needs signing in again: blitz mcp login %s", h.server, h.server)
}

// NeedsLogin is the handler for an HTTP server with no stored sign-in: it
// adds nothing to requests, and a server that asks for authorization is
// told to run blitz mcp login.
func NeedsLogin(server string) auth.OAuthHandler {
	return &storedHandler{server: server}
}

// Login signs in to server at serverURL: it registers Blitz with the
// server's authorization server, has the user authorize it in a browser
// (show gets the URL to open), and receives the code on a local callback,
// or from pasted when the browser is elsewhere (an SSH session: paste the
// address the browser ends on, or the code). The sign-in is saved in store.
func Login(ctx context.Context, store secrets.Store, server, serverURL string, show func(authURL string), pasted <-chan string) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer ln.Close()
	redirect := fmt.Sprintf("http://127.0.0.1:%d/callback", ln.Addr().(*net.TCPAddr).Port)

	results := make(chan *auth.AuthorizationResult, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		res := resultFrom(r.URL.Query())
		if res == nil {
			http.Error(w, "The sign-in didn't return a code.", http.StatusBadRequest)
			return
		}
		fmt.Fprintln(w, "Signed in to Blitz. You can close this page.")
		select {
		case results <- res:
		default:
		}
	})}
	go srv.Serve(ln)
	defer srv.Close()

	var saved *Record
	handler, err := auth.NewAuthorizationCodeHandler(&auth.AuthorizationCodeHandlerConfig{
		DynamicClientRegistrationConfig: &auth.DynamicClientRegistrationConfig{Metadata: &oauthex.ClientRegistrationMetadata{
			ClientName: "Blitz", RedirectURIs: []string{redirect},
			GrantTypes: []string{"authorization_code", "refresh_token"}, ResponseTypes: []string{"code"},
			TokenEndpointAuthMethod: "none",
		}},
		RedirectURL:         redirect,
		RequestRefreshToken: true,
		AuthorizationCodeFetcher: func(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
			show(args.URL)
			for {
				select {
				case res := <-results:
					return res, nil
				case p, ok := <-pasted:
					if !ok {
						pasted = nil
						continue
					}
					if res := parsePasted(p); res != nil {
						return res, nil
					}
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
		},
		NewTokenSource: func(ctx context.Context, cfg *oauth2.Config, tok *oauth2.Token) (oauth2.TokenSource, error) {
			saved = &Record{
				ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, AuthURL: cfg.Endpoint.AuthURL, TokenURL: cfg.Endpoint.TokenURL,
				RedirectURL: cfg.RedirectURL, Scopes: cfg.Scopes, Token: tok,
			}
			if err := Save(store, server, saved); err != nil {
				return nil, fmt.Errorf("saving the sign-in: %w", err)
			}
			return cfg.TokenSource(ctx, tok), nil
		},
	})
	if err != nil {
		return err
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "blitz", Version: "1"}, nil)
	cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: serverURL, OAuthHandler: handler}, nil)
	if err != nil {
		return err
	}
	defer cs.Close()
	if saved == nil {
		return errors.New("the server didn't ask to sign in: it needs no OAuth")
	}
	return nil
}

// resultFrom reads the authorization server's redirect.
func resultFrom(q url.Values) *auth.AuthorizationResult {
	if q.Get("code") == "" {
		return nil
	}
	return &auth.AuthorizationResult{Code: q.Get("code"), State: q.Get("state"), Iss: q.Get("iss")}
}

// parsePasted reads what the user pasted: the address the browser ended
// on (with its code and state).
func parsePasted(s string) *auth.AuthorizationResult {
	s = strings.TrimSpace(s)
	if u, err := url.Parse(s); err == nil && u.RawQuery != "" {
		return resultFrom(u.Query())
	}
	return nil
}
