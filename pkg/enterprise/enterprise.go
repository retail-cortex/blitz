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

// Package enterprise connects Blitz to an organization's service
// (spec_053_enterprise §3): it finds out what the service offers
// (EN-11), signs the person in with the organization's identity provider
// and exchanges the provider's ID token for the service's own tokens
// (EN-12), and keeps those renewed. The refresh token is kept in the
// secret store (the OS keychain); the access token only in memory. The
// calls are JSON over HTTPS, as the spec gives them: nothing here needs
// the service's code.
package enterprise

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/secrets"
)

// ErrNotSignedIn means no one is signed in to the service.
var ErrNotSignedIn = errors.New("not signed in to the organization's service")

// Refusal is the service's refusal: its status, its code
// (unauthenticated, forbidden, license_lapsed, terms_required…) and its
// message, written for the person.
type Refusal struct {
	Status  int
	Code    string
	Message string
}

// Error is the message, else the status and code.
func (r *Refusal) Error() string {
	if r.Message != "" {
		return r.Message
	}
	return fmt.Sprintf("the organization's service refused: %d %s", r.Status, r.Code)
}

// AppClient is an OAuth client an app signs in with. The secret, when
// the provider gives one to apps, isn't confidential.
type AppClient struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
}

// Info is what the service says about itself (EN-11).
type Info struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	OIDC    struct {
		Issuer   string    `json:"issuer"`
		ClientID string    `json:"client_id"`
		Desktop  AppClient `json:"desktop"`
		Device   AppClient `json:"device"`
	} `json:"oidc"`
	Capabilities []string `json:"capabilities"`
	// CatalogKeys check what the service signs (its branding, its
	// catalog), by key ID: Ed25519 public keys, base64.
	CatalogKeys map[string]string `json:"catalog_keys"`
}

// Has is whether the service offers a capability (gateway, catalog,
// jobs, sharing, workspaces, branding).
func (i Info) Has(capability string) bool { return slices.Contains(i.Capabilities, capability) }

// desktop is the client the desktop app signs in with: its own, else the
// service's.
func (i Info) desktop() AppClient {
	if i.OIDC.Desktop.ClientID != "" {
		return i.OIDC.Desktop
	}
	return AppClient{ClientID: i.OIDC.ClientID}
}

// device is the CLI's.
func (i Info) device() AppClient {
	if i.OIDC.Device.ClientID != "" {
		return i.OIDC.Device
	}
	return i.desktop()
}

// Person is who's signed in, as the service knows them.
type Person struct {
	ID    string   `json:"id"`
	Email string   `json:"email"`
	Name  string   `json:"name"`
	Roles []string `json:"roles"`
}

// Normalize checks a service's address and gives it without a trailing
// slash: https, or http to this computer (for development).
func Normalize(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("%q isn't an address, such as https://blitz.acme.com", raw)
	}
	local := u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"
	if u.Scheme != "https" && (u.Scheme != "http" || !local) {
		return "", fmt.Errorf("%s: the organization's service is reached over https", raw)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%s: the service's address alone, without a query", raw)
	}
	return strings.TrimSuffix(u.Scheme+"://"+u.Host+u.Path, "/"), nil
}

// call sends a JSON request to the service and reads its JSON answer into
// out (when it isn't nil); a refusal is a *Refusal.
func call(ctx context.Context, hc *http.Client, method, u, bearer string, in, out any) error {
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("reaching the organization's service: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return refusal(res)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return err
	}
	if out == nil || res.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("the organization's service answered with something other than JSON: %w", err)
	}
	return nil
}

// refusal is the *Refusal a service's answer that isn't a success says.
func refusal(res *http.Response) error {
	var r struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	data, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	_ = json.Unmarshal(data, &r)
	return &Refusal{Status: res.StatusCode, Code: r.Code, Message: r.Message}
}

// Discover asks the service at u what it offers.
func Discover(ctx context.Context, hc *http.Client, u string) (Info, error) {
	var in Info
	if err := call(ctx, hc, http.MethodGet, u+"/api/v1/info", "", nil, &in); err != nil {
		return Info{}, err
	}
	if in.OIDC.Issuer == "" || in.OIDC.ClientID == "" {
		return Info{}, fmt.Errorf("%s doesn't look like an organization's Blitz service: it names no identity provider", u)
	}
	return in, nil
}

// record is what's kept in the secret store: whom the refresh token is
// for, and what the service said about itself when they signed in.
type record struct {
	URL          string `json:"url"`
	RefreshToken string `json:"refresh_token"`
	Person       Person `json:"person"`
	Info         Info   `json:"info"`
}

// secretName is where the sign-in is kept.
const secretName = "enterprise-session"

// session is /api/v1/session's answer.
type session struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Person       Person `json:"person"`
	Terms        *struct {
		Version string `json:"version"`
	} `json:"terms"`
}

// Client is the connection to one service, for one person on this
// computer. It's safe to use at once from many goroutines; renewals go
// one at a time, since each refresh token is good once.
type Client struct {
	url    string
	store  secrets.Store
	hc     *http.Client
	now    func() time.Time
	client string // what the service records the sessions as from
	brand  string // the folder its branding is kept in, for the sign-in page

	mu      sync.Mutex
	rec     *record
	loaded  bool
	access  string
	expires time.Time
	life    time.Duration // the access token's whole lifetime
	terms   string        // a version to accept, from the last session's answer
	info    Info          // what the service last said about itself, kept past a sign-out

	models models
	run    *runCreds // a job's run's, instead of a person's sign-in
}

// Options are a Client's.
type Options struct {
	// HTTP is the client the calls go through (http.DefaultClient).
	HTTP *http.Client
	// Now is the clock (time.Now).
	Now func() time.Time
	// Client names the app and its version for the service's sessions,
	// such as "blitz 0.9.0 (desktop)".
	Client string
	// BrandingDir is the folder the service's branding is kept in
	// (RefreshBranding's): the browser sign-in's page shows it.
	BrandingDir string
}

// New is the connection to the service at u (already normalized),
// keeping its sign-in in store.
func New(u string, store secrets.Store, o Options) *Client {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.HTTP == nil {
		// Its own client: limits on reaching the service, and once it
		// hasn't answered, calls that fail at once for a while (EN-37).
		o.HTTP = &http.Client{Transport: &reach{base: transport(), now: o.Now}}
	}
	if o.Client == "" {
		o.Client = "blitz"
	}
	return &Client{url: u, store: store, hc: o.HTTP, now: o.Now, client: o.Client, brand: o.BrandingDir}
}

// URL is the service's address.
func (c *Client) URL() string { return c.url }

// load reads the sign-in from the store once; one for another service
// counts as none. Called with c.mu held.
func (c *Client) load() *record {
	if c.loaded {
		return c.rec
	}
	c.loaded = true
	if c.store == nil {
		return nil // a run's, signed in by its secret (NewRun)
	}
	v, err := c.store.Get(secretName)
	if err != nil {
		return nil
	}
	var r record
	if json.Unmarshal([]byte(v), &r) != nil || r.RefreshToken == "" || r.URL != c.url {
		return nil
	}
	c.rec, c.info = &r, r.Info
	return c.rec
}

// keep stores r and the session's access token. Called with c.mu held.
func (c *Client) keep(r *record, s session) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if err := c.store.Set(secretName, string(data)); err != nil {
		return fmt.Errorf("keeping the sign-in in the %s: %w", c.store.Kind(), err)
	}
	c.rec, c.loaded, c.info = r, true, r.Info
	c.life = time.Duration(s.ExpiresIn) * time.Second
	c.access, c.expires = s.AccessToken, c.now().Add(c.life)
	c.terms = ""
	if s.Terms != nil {
		c.terms = s.Terms.Version
	}
	return nil
}

// forget drops the sign-in, here and in the store. Called with c.mu held.
func (c *Client) forget() error {
	c.rec, c.loaded, c.access, c.expires, c.terms = nil, true, "", time.Time{}, ""
	if err := c.store.Delete(secretName); err != nil && !errors.Is(err, secrets.ErrNotFound) {
		return err
	}
	return nil
}

// Status is who's signed in, and until when their access token lasts.
type Status struct {
	URL      string
	SignedIn bool
	Person   Person
	Info     Info
	// Renews is when the access token is next renewed; zero until one is
	// asked for.
	Renews time.Time
	// Terms is a version of the organization's terms to accept, if the
	// service said so at the last sign-in or renewal (EN-53).
	Terms string
}

// Status is the sign-in's state, from what's kept: it calls nothing.
func (c *Client) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := Status{URL: c.url, Terms: c.terms}
	if c.run != nil {
		// A run on the service is signed in as the job's owner, by its
		// secret, for as long as it runs.
		st.SignedIn = true
		return st
	}
	r := c.load()
	st.Info = c.info
	if r != nil {
		st.SignedIn, st.Person = true, r.Person
		if c.access != "" {
			st.Renews = c.renewAt()
		}
	}
	return st
}

// renewAt is when the access token is renewed: a minute before it
// expires, or halfway through a short one. Called with c.mu held.
func (c *Client) renewAt() time.Time {
	margin := time.Minute
	if c.life < 2*margin {
		margin = c.life / 2
	}
	return c.expires.Add(-margin)
}

// AccessToken is a current access token for the service's calls, renewed
// when it's near its end. A renewal the service refuses (the session was
// ended, the person deactivated) signs out here too: the error is the
// service's refusal, and ErrNotSignedIn after it.
func (c *Client) AccessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.run != nil {
		return c.runToken(ctx)
	}
	r := c.load()
	if r == nil {
		return "", ErrNotSignedIn
	}
	if c.access != "" && c.now().Before(c.renewAt()) {
		return c.access, nil
	}
	// Another process on this computer may have renewed since this one
	// read the sign-in: renew with the latest refresh token kept, since
	// an earlier one would end the session.
	c.loaded = false
	if r = c.load(); r == nil {
		c.access = ""
		return "", ErrNotSignedIn
	}
	var s session
	err := call(ctx, c.hc, http.MethodPost, c.url+"/api/v1/session/refresh", "", map[string]string{"refresh_token": r.RefreshToken}, &s)
	var ref *Refusal
	if errors.As(err, &ref) && ref.Status == http.StatusUnauthorized {
		_ = c.forget()
		return "", fmt.Errorf("%w: %w", ErrNotSignedIn, ref)
	}
	if err != nil {
		return "", err
	}
	// An answer without its tokens is kept as nothing: an empty refresh
	// token would read as signed out next time.
	if s.AccessToken == "" || s.RefreshToken == "" {
		return "", errors.New("the organization's service answered the renewal without its tokens")
	}
	// The new refresh token is kept before the access token is used: the
	// old one ends the session if it's sent again.
	next := *r
	next.RefreshToken = s.RefreshToken
	if s.Person.ID != "" || s.Person.Email != "" {
		next.Person = s.Person
	}
	if err := c.keep(&next, s); err != nil {
		return "", err
	}
	return c.access, nil
}

// Invalidate drops the access token, so the next call renews it: after
// the service refused it as expired.
func (c *Client) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.access = ""
}

// exchange trades the provider's ID token for the service's session and
// keeps it. Called with c.mu held.
func (c *Client) exchange(ctx context.Context, info Info, idToken string) error {
	var s session
	if err := call(ctx, c.hc, http.MethodPost, c.url+"/api/v1/session", "", map[string]string{"id_token": idToken, "client": c.client}, &s); err != nil {
		return err
	}
	if s.RefreshToken == "" || s.AccessToken == "" {
		return errors.New("the organization's service answered the sign-in without its tokens")
	}
	return c.keep(&record{URL: c.url, RefreshToken: s.RefreshToken, Person: s.Person, Info: info}, s)
}

// SignOut ends the session at the service, then here. Here it always
// ends: a service that can't be reached ends it when its refresh token
// expires.
func (c *Client) SignOut(ctx context.Context) error {
	c.mu.Lock()
	signedIn := c.load() != nil
	c.mu.Unlock()
	if !signedIn {
		return nil
	}
	// Ending a session needs its access token; renewing for one keeps
	// the same session.
	token, err := c.AccessToken(ctx)
	if errors.Is(err, ErrNotSignedIn) {
		return nil // the service ended it already
	}
	var remote error
	if err == nil {
		remote = call(ctx, c.hc, http.MethodDelete, c.url+"/api/v1/session", token, nil, nil)
	} else {
		remote = err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.forget(); err != nil {
		return err
	}
	var ref *Refusal
	if errors.As(remote, &ref) && ref.Status == http.StatusUnauthorized {
		return nil // over already
	}
	return remote
}
