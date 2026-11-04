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
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/retail-cortex/blitz/pkg/signedin"
	"golang.org/x/oauth2"
)

// Prompt is what the person does to sign in: open URL in a browser, and,
// for the device code, enter Code there.
type Prompt struct {
	URL  string
	Code string
}

// provider is the identity provider's endpoints, from its discovery
// document.
type provider struct {
	Authorization string `json:"authorization_endpoint"`
	Token         string `json:"token_endpoint"`
	Device        string `json:"device_authorization_endpoint"`
}

func discoverProvider(ctx context.Context, hc *http.Client, issuer string) (provider, error) {
	var p provider
	u := strings.TrimSuffix(issuer, "/") + "/.well-known/openid-configuration"
	if err := call(ctx, hc, http.MethodGet, u, "", nil, &p); err != nil {
		return provider{}, fmt.Errorf("the identity provider %s: %w", issuer, err)
	}
	if p.Authorization == "" || p.Token == "" {
		return provider{}, fmt.Errorf("the identity provider %s names no authorization or token endpoint", issuer)
	}
	return p, nil
}

// scopes are what Blitz asks the provider for: who the person is.
var scopes = []string{"openid", "email", "profile"}

func (c *Client) oauth(app AppClient, p provider, redirect string) *oauth2.Config {
	return &oauth2.Config{
		ClientID: app.ClientID, ClientSecret: app.ClientSecret, RedirectURL: redirect, Scopes: scopes,
		// In the body: an app's client is public (RFC 6749 §2.3.1), and
		// guessing the style spends the one-time code on the wrong guess.
		Endpoint: oauth2.Endpoint{AuthURL: p.Authorization, TokenURL: p.Token, DeviceAuthURL: p.Device, AuthStyle: oauth2.AuthStyleInParams},
	}
}

// idToken is the ID token in the provider's token answer.
func idToken(t *oauth2.Token) (string, error) {
	id, _ := t.Extra("id_token").(string)
	if id == "" {
		return "", errors.New("the identity provider answered without an ID token")
	}
	return id, nil
}

// start discovers the service and its provider: the first steps of both
// sign-ins.
// The context carries c's HTTP client to oauth2's calls.
func (c *Client) start(ctx context.Context) (context.Context, Info, provider, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, c.hc)
	info, err := Discover(ctx, c.hc, c.url)
	if err != nil {
		return ctx, Info{}, provider{}, err
	}
	p, err := discoverProvider(ctx, c.hc, info.OIDC.Issuer)
	return ctx, info, p, err
}

// finish exchanges the provider's token for the service's session.
func (c *Client) finish(ctx context.Context, info Info, t *oauth2.Token) error {
	id, err := idToken(t)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.exchange(ctx, info, id)
}

// signInBrand is the organization's look for the sign-in's page, from
// the branding kept; nil for Blitz's own.
func (c *Client) signInBrand() *signedin.Brand {
	if c.brand == "" {
		return nil
	}
	b := LoadBranding(c.brand)
	if b == nil {
		return nil
	}
	return &signedin.Brand{Name: b.Name, Accent: b.Accent, AccentDark: b.AccentDark,
		Logo: cmp.Or(b.Images["logo.svg"], b.Images["logo.png"], b.Images["icon.png"]), LogoDark: b.Images["logo-dark.svg"]}
}

// SignInBrowser signs in through a browser, for the desktop app and the
// tray: the authorization code with PKCE, received on a redirect to this
// computer (EN-12). show gets the address to open; pasted takes the
// address the browser ended on when it's on another computer (an SSH
// session).
func (c *Client) SignInBrowser(ctx context.Context, show func(Prompt), pasted <-chan string) error {
	ctx, info, p, err := c.start(ctx)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer ln.Close()
	cfg := c.oauth(info.desktop(), p, fmt.Sprintf("http://127.0.0.1:%d/callback", ln.Addr().(*net.TCPAddr).Port))

	state := random()
	verifier := oauth2.GenerateVerifier()
	codes := make(chan url.Values, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" || r.URL.Query().Get("state") != state {
			http.NotFound(w, r)
			return
		}
		page := signedin.Page{Title: "You're signed in", Open: true, Brand: c.signInBrand(),
			Message: "Blitz is signed in to your organization. Go back to Blitz to carry on."}
		if page.Brand != nil && page.Brand.Name != "" {
			page.Message = "Blitz is signed in to " + page.Brand.Name + ". Go back to Blitz to carry on."
		}
		if e := r.URL.Query().Get("error"); e != "" {
			page.Title, page.Failed = "The sign-in didn't go through", true
			page.Message = "Your organization's sign-in refused it (" + e + "). Try again from Blitz."
		}
		signedin.Write(w, http.StatusOK, page)
		select {
		case codes <- r.URL.Query():
		default:
		}
	})}
	go srv.Serve(ln)
	defer srv.Close()

	show(Prompt{URL: cfg.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("nonce", random()))})
	var q url.Values
	for q == nil {
		select {
		case q = <-codes:
		case s, ok := <-pasted:
			if !ok {
				pasted = nil
				continue
			}
			if u, err := url.Parse(strings.TrimSpace(s)); err == nil && u.Query().Get("state") == state {
				q = u.Query()
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if e := q.Get("error"); e != "" {
		return fmt.Errorf("the identity provider refused the sign-in: %s %s", e, q.Get("error_description"))
	}
	t, err := cfg.Exchange(ctx, q.Get("code"), oauth2.VerifierOption(verifier))
	if err != nil {
		return fmt.Errorf("the identity provider: %w", err)
	}
	return c.finish(ctx, info, t)
}

// SignInDevice signs in with a code entered in a browser anywhere, for
// the CLI (EN-12): show gets the address and the code.
func (c *Client) SignInDevice(ctx context.Context, show func(Prompt)) error {
	ctx, info, p, err := c.start(ctx)
	if err != nil {
		return err
	}
	if p.Device == "" {
		return fmt.Errorf("the identity provider %s doesn't offer sign-in with a code: sign in from Blitz's desktop app", info.OIDC.Issuer)
	}
	cfg := c.oauth(info.device(), p, "")
	da, err := cfg.DeviceAuth(ctx)
	if err != nil {
		return fmt.Errorf("the identity provider: %w", err)
	}
	show(Prompt{URL: da.VerificationURI, Code: da.UserCode})
	t, err := cfg.DeviceAccessToken(ctx, da)
	if err != nil {
		return fmt.Errorf("the identity provider: %w", err)
	}
	return c.finish(ctx, info, t)
}

func random() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
