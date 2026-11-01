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
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/enterprise/enterprisetest"
	"github.com/retail-cortex/blitz/pkg/secrets"
)

// browse is enterprisetest.Browse for a Prompt.
func browse(t *testing.T) func(Prompt) {
	b := enterprisetest.Browse(t)
	return func(p Prompt) { b(p.URL) }
}

func setup(t *testing.T) (*enterprisetest.Provider, *enterprisetest.Service, *Client, *secrets.Memory, *time.Time) {
	p := enterprisetest.NewProvider(t)
	s := enterprisetest.NewService(t, p.URL)
	store := &secrets.Memory{}
	now := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	c := New(s.URL, store, Options{Now: func() time.Time { return now }, Client: "blitz 1.0 (test)"})
	return p, s, c, store, &now
}

func TestNormalize(t *testing.T) {
	cases := map[string]struct{ in, want, err string }{
		"https":             {"https://blitz.acme.com/", "https://blitz.acme.com", ""},
		"a path":            {" https://acme.com/blitz/ ", "https://acme.com/blitz", ""},
		"http on this host": {"http://127.0.0.1:8443", "http://127.0.0.1:8443", ""},
		"localhost":         {"http://localhost:8443", "http://localhost:8443", ""},
		"http elsewhere":    {"http://blitz.acme.com", "", "over https"},
		"no host":           {"blitz.acme.com", "", "isn't an address"},
		"a query":           {"https://blitz.acme.com/?x=1", "", "without a query"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := Normalize(c.in)
			if c.err != "" {
				assert.ErrorContains(t, err, c.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestDiscover(t *testing.T) {
	p := enterprisetest.NewProvider(t)
	s := enterprisetest.NewService(t, p.URL)
	info, err := Discover(context.Background(), http.DefaultClient, s.URL)
	require.NoError(t, err)
	assert.Equal(t, "Acme Assist", info.Name)
	assert.Equal(t, AppClient{ClientID: "desktop", ClientSecret: "d-secret"}, info.desktop())
	assert.Equal(t, AppClient{ClientID: "device", ClientSecret: "v-secret"}, info.device())
	assert.True(t, info.Has("gateway"))
	assert.False(t, info.Has("catalog"))

	t.Run("one client for every app", func(t *testing.T) {
		var one Info
		one.OIDC.ClientID = "only"
		assert.Equal(t, AppClient{ClientID: "only"}, one.desktop())
		assert.Equal(t, AppClient{ClientID: "only"}, one.device())
	})
	t.Run("not a Blitz service", func(t *testing.T) {
		other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
		t.Cleanup(other.Close)
		_, err := Discover(context.Background(), http.DefaultClient, other.URL)
		assert.ErrorContains(t, err, "names no identity provider")
	})
	t.Run("not JSON", func(t *testing.T) {
		html := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`<html>`)) }))
		t.Cleanup(html.Close)
		_, err := Discover(context.Background(), http.DefaultClient, html.URL)
		assert.ErrorContains(t, err, "other than JSON")
	})
	t.Run("unreachable", func(t *testing.T) {
		_, err := Discover(context.Background(), http.DefaultClient, "http://127.0.0.1:1")
		assert.ErrorContains(t, err, "reaching the organization's service")
	})
}

// The desktop app's sign-in, start to end: the browser, the code with
// PKCE, the service's session kept in the store, renewed when it's near
// its end, and signed out.
func TestSignInBrowser(t *testing.T) {
	_, s, c, store, now := setup(t)
	ctx := context.Background()
	assert.False(t, c.Status().SignedIn)
	_, err := c.AccessToken(ctx)
	assert.ErrorIs(t, err, ErrNotSignedIn)

	require.NoError(t, c.SignInBrowser(ctx, browse(t), nil))
	st := c.Status()
	assert.True(t, st.SignedIn)
	assert.Equal(t, "ana@acme.com", st.Person.Email)
	assert.Equal(t, "Acme Assist", st.Info.Name)
	assert.Equal(t, now.Add(9*time.Minute), st.Renews, "a minute before the 10 minutes end")
	assert.Equal(t, []string{"blitz 1.0 (test)"}, s.SignIns)

	tok, err := c.AccessToken(ctx)
	require.NoError(t, err)
	assert.Equal(t, "at-1", tok, "the session's own, until it's near its end")
	assert.Zero(t, s.Refreshes)

	// Another process (the CLI, or blitzd started again) finds the sign-in.
	again := New(s.URL, store, Options{Now: func() time.Time { return *now }})
	assert.True(t, again.Status().SignedIn)
	assert.True(t, again.Status().Renews.IsZero(), "no access token yet in this process")
	tok, err = again.AccessToken(ctx)
	require.NoError(t, err)
	assert.Equal(t, "at-2", tok)
	assert.Equal(t, 1, s.Refreshes)

	// The second renewed with the first's refresh token: the first renews
	// with the newer one it kept, not its own spent one.
	*now = now.Add(9 * time.Minute)
	tok, err = c.AccessToken(ctx)
	require.NoError(t, err)
	assert.Equal(t, "at-3", tok)

	// The session ended at the service (an admin signed them out): the
	// next renewal is refused, and it's signed out here.
	s.Ended["s1"] = true
	*now = now.Add(9 * time.Minute)
	_, err = c.AccessToken(ctx)
	assert.ErrorIs(t, err, ErrNotSignedIn)
	assert.ErrorContains(t, err, "Sign in again.")
	assert.False(t, c.Status().SignedIn)
	_, err = store.Get(secretName)
	assert.ErrorIs(t, err, secrets.ErrNotFound)

	// Signed out by another process: this one finds the sign-in gone.
	require.NoError(t, c.SignInBrowser(ctx, browse(t), nil))
	require.NoError(t, New(s.URL, store, Options{Now: func() time.Time { return *now }}).SignOut(ctx))
	*now = now.Add(9 * time.Minute)
	_, err = c.AccessToken(ctx)
	assert.ErrorIs(t, err, ErrNotSignedIn)
}

func TestRenewal(t *testing.T) {
	_, s, c, store, now := setup(t)
	ctx := context.Background()
	require.NoError(t, c.SignInBrowser(ctx, browse(t), nil))

	*now = now.Add(9 * time.Minute)
	tok, err := c.AccessToken(ctx)
	require.NoError(t, err)
	assert.Equal(t, "at-2", tok)
	kept, err := store.Get(secretName)
	require.NoError(t, err)
	assert.Contains(t, kept, `"refresh_token":"rt-2"`, "the new refresh token is kept before it's used")

	t.Run("at once from many places, one renewal", func(t *testing.T) {
		*now = now.Add(9 * time.Minute)
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				tok, err := c.AccessToken(ctx)
				assert.NoError(t, err)
				assert.Equal(t, "at-3", tok)
			})
		}
		wg.Wait()
		assert.Equal(t, 2, s.Refreshes)
	})
	t.Run("invalidated, renewed", func(t *testing.T) {
		c.Invalidate()
		tok, err := c.AccessToken(ctx)
		require.NoError(t, err)
		assert.Equal(t, "at-4", tok)
	})
	t.Run("a short token renews halfway", func(t *testing.T) {
		s.ExpiresIn = 60
		c.Invalidate()
		_, err := c.AccessToken(ctx)
		require.NoError(t, err)
		assert.Equal(t, now.Add(30*time.Second), c.Status().Renews)
	})
	t.Run("the service down keeps the sign-in", func(t *testing.T) {
		s.Offline = true
		c.Invalidate()
		_, err := c.AccessToken(ctx)
		var ref *Refusal
		require.ErrorAs(t, err, &ref)
		assert.Equal(t, http.StatusServiceUnavailable, ref.Status)
		assert.Contains(t, ref.Error(), "503")
		assert.True(t, c.Status().SignedIn)
		s.Offline = false
	})
	t.Run("terms to accept", func(t *testing.T) {
		s.Terms = "2026-11"
		c.Invalidate()
		_, err := c.AccessToken(ctx)
		require.NoError(t, err)
		assert.Equal(t, "2026-11", c.Status().Terms)
	})
}

func TestSignOut(t *testing.T) {
	ctx := context.Background()
	t.Run("ends the session at the service", func(t *testing.T) {
		_, s, c, store, _ := setup(t)
		require.NoError(t, c.SignInBrowser(ctx, browse(t), nil))
		require.NoError(t, c.SignOut(ctx))
		assert.True(t, s.Ended["s1"])
		assert.False(t, c.Status().SignedIn)
		assert.Equal(t, "Acme Assist", c.Status().Info.Name, "what the service said, kept")
		_, err := store.Get(secretName)
		assert.ErrorIs(t, err, secrets.ErrNotFound)
		assert.NoError(t, c.SignOut(ctx), "signed out already")
	})
	t.Run("from another process, renewing for its access token", func(t *testing.T) {
		_, s, c, store, now := setup(t)
		require.NoError(t, c.SignInBrowser(ctx, browse(t), nil))
		other := New(s.URL, store, Options{Now: func() time.Time { return *now }})
		require.NoError(t, other.SignOut(ctx))
		assert.True(t, s.Ended["s1"])
	})
	t.Run("ended at the service already", func(t *testing.T) {
		_, s, c, _, _ := setup(t)
		require.NoError(t, c.SignInBrowser(ctx, browse(t), nil))
		s.Ended["s1"] = true
		assert.NoError(t, c.SignOut(ctx))
		assert.False(t, c.Status().SignedIn)
	})
	t.Run("the service unreachable: signed out here", func(t *testing.T) {
		_, s, c, _, _ := setup(t)
		require.NoError(t, c.SignInBrowser(ctx, browse(t), nil))
		s.Close()
		assert.Error(t, c.SignOut(ctx))
		assert.False(t, c.Status().SignedIn)
	})
}

func TestSignInDevice(t *testing.T) {
	p, s, c, _, _ := setup(t)
	var shown Prompt
	require.NoError(t, c.SignInDevice(context.Background(), func(pr Prompt) { shown = pr }))
	assert.Equal(t, Prompt{URL: "https://www.google.com/device", Code: "ABCD-EFGH"}, shown)
	assert.Equal(t, 2, p.Polls, "pending, then approved")
	assert.True(t, c.Status().SignedIn)
	assert.Len(t, s.SignIns, 1)

	t.Run("a provider without it", func(t *testing.T) {
		p, _, c, _, _ := setup(t)
		p.NoDevice = true
		err := c.SignInDevice(context.Background(), func(Prompt) {})
		assert.ErrorContains(t, err, "sign in from Blitz's desktop app")
	})
	t.Run("the wrong client", func(t *testing.T) {
		p, _, c, _, _ := setup(t)
		delete(p.Clients, "device")
		p.Clients["device"] = "another secret"
		err := c.SignInDevice(context.Background(), func(Prompt) {})
		assert.ErrorContains(t, err, "the identity provider")
	})
}

func TestSignInBrowserPasted(t *testing.T) {
	_, _, c, _, _ := setup(t)
	pasted := make(chan string, 2)
	show := func(pr Prompt) {
		// The browser is on another computer: it ends on an address that
		// doesn't load, and the person pastes it.
		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		res, err := client.Get(pr.URL)
		require.NoError(t, err)
		res.Body.Close()
		pasted <- "not an address"
		pasted <- res.Header.Get("Location")
		close(pasted)
	}
	require.NoError(t, c.SignInBrowser(context.Background(), show, pasted))
	assert.True(t, c.Status().SignedIn)
}

func TestSignInRefused(t *testing.T) {
	ctx := context.Background()
	t.Run("the provider refuses", func(t *testing.T) {
		_, _, c, _, _ := setup(t)
		show := func(pr Prompt) {
			u, _ := url.Parse(pr.URL)
			q := u.Query()
			go func() {
				res, err := http.Get(q.Get("redirect_uri") + "?" + url.Values{"error": {"access_denied"}, "state": {q.Get("state")}}.Encode())
				if err == nil {
					res.Body.Close()
				}
			}()
		}
		err := c.SignInBrowser(ctx, show, nil)
		assert.ErrorContains(t, err, "refused the sign-in: access_denied")
	})
	t.Run("a callback with another state is ignored", func(t *testing.T) {
		_, _, c, _, _ := setup(t)
		ctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
		show := func(pr Prompt) {
			u, _ := url.Parse(pr.URL)
			go func() {
				res, err := http.Get(u.Query().Get("redirect_uri") + "?code=stolen&state=other")
				if assert.NoError(t, err) {
					res.Body.Close()
					assert.Equal(t, http.StatusNotFound, res.StatusCode)
				}
			}()
		}
		assert.ErrorIs(t, c.SignInBrowser(ctx, show, nil), context.DeadlineExceeded)
	})
	t.Run("the service refuses the ID token", func(t *testing.T) {
		p, _, c, _, _ := setup(t)
		p.IDToken = "for-someone-else"
		err := c.SignInBrowser(ctx, browse(t), nil)
		var ref *Refusal
		require.ErrorAs(t, err, &ref)
		assert.Equal(t, "unauthenticated", ref.Code)
		assert.Equal(t, "The sign-in couldn't be checked.", err.Error())
		assert.False(t, c.Status().SignedIn)
	})
	t.Run("no ID token", func(t *testing.T) {
		p, _, c, _, _ := setup(t)
		p.IDToken = "none"
		assert.ErrorContains(t, c.SignInBrowser(ctx, browse(t), nil), "without an ID token")
	})
	t.Run("the provider is unreachable", func(t *testing.T) {
		p, _, c, _, _ := setup(t)
		p.Close()
		assert.ErrorContains(t, c.SignInBrowser(ctx, browse(t), nil), "the identity provider")
	})
}

// failing is a secret store that refuses to keep anything.
type failing struct{ secrets.Memory }

func (f *failing) Set(string, string) error { return fmt.Errorf("the keychain is locked") }
func (f *failing) Delete(string) error      { return fmt.Errorf("the keychain is locked") }

func TestEdges(t *testing.T) {
	ctx := context.Background()
	t.Run("defaults", func(t *testing.T) {
		c := New("https://blitz.acme.com", &secrets.Memory{}, Options{})
		assert.Equal(t, "https://blitz.acme.com", c.URL())
		assert.False(t, c.Status().SignedIn)
	})
	t.Run("a sign-in kept for another service is none", func(t *testing.T) {
		_, s, c, store, _ := setup(t)
		require.NoError(t, c.SignInBrowser(ctx, browse(t), nil))
		other := New("https://other.example", store, Options{})
		assert.False(t, other.Status().SignedIn)
		require.NoError(t, store.Set(secretName, "not json"))
		assert.False(t, New(s.URL, store, Options{}).Status().SignedIn)
	})
	t.Run("a keychain that won't keep it", func(t *testing.T) {
		_, s, _, _, _ := setup(t)
		c := New(s.URL, &failing{}, Options{})
		err := c.SignInBrowser(ctx, browse(t), nil)
		assert.ErrorContains(t, err, "keeping the sign-in in the memory: the keychain is locked")
	})
	t.Run("a keychain that won't keep the renewal", func(t *testing.T) {
		_, s, c, store, _ := setup(t)
		require.NoError(t, c.SignInBrowser(ctx, browse(t), nil))
		kept, _ := store.Get(secretName)
		locked := &failing{}
		require.NoError(t, locked.Memory.Set(secretName, kept))
		other := New(s.URL, locked, Options{})
		_, err := other.AccessToken(ctx)
		assert.ErrorContains(t, err, "the keychain is locked")
		assert.NoError(t, other.SignOut(ctx), "its refresh token was spent: the service ended the session")
	})
	t.Run("a keychain that won't forget it", func(t *testing.T) {
		_, s, c, store, _ := setup(t)
		require.NoError(t, c.SignInBrowser(ctx, browse(t), nil))
		kept, _ := store.Get(secretName)
		locked := &failing{}
		require.NoError(t, locked.Memory.Set(secretName, kept))
		c.store = locked
		assert.ErrorContains(t, c.SignOut(ctx), "the keychain is locked")
		assert.True(t, s.Ended["s1"], "ended at the service all the same")
	})
	t.Run("a sign-in answered without tokens", func(t *testing.T) {
		_, s, c, _, _ := setup(t)
		s.Tokenless = true
		assert.ErrorContains(t, c.SignInBrowser(ctx, browse(t), nil), "without its tokens")
	})
	t.Run("signing out from another process when the service ended it", func(t *testing.T) {
		_, s, c, store, _ := setup(t)
		require.NoError(t, c.SignInBrowser(ctx, browse(t), nil))
		s.Ended["s1"] = true
		assert.NoError(t, New(s.URL, store, Options{}).SignOut(ctx))
	})
	t.Run("signing out from another process when the service is down", func(t *testing.T) {
		_, s, c, store, _ := setup(t)
		require.NoError(t, c.SignInBrowser(ctx, browse(t), nil))
		s.Offline = true
		other := New(s.URL, store, Options{})
		assert.Error(t, other.SignOut(ctx))
		assert.False(t, other.Status().SignedIn, "signed out here regardless")
	})
	t.Run("a provider that names no endpoints", func(t *testing.T) {
		p, _, c, _, _ := setup(t)
		p.Bare = true
		assert.ErrorContains(t, c.SignInBrowser(ctx, browse(t), nil), "names no authorization or token endpoint")
	})
	t.Run("the service unreachable", func(t *testing.T) {
		_, s, c, _, _ := setup(t)
		s.Close()
		assert.ErrorContains(t, c.SignInBrowser(ctx, browse(t), nil), "reaching the organization's service")
		assert.ErrorContains(t, c.SignInDevice(ctx, func(Prompt) {}), "reaching the organization's service")
	})
	t.Run("the provider refuses the code", func(t *testing.T) {
		p, _, c, _, _ := setup(t)
		show := func(pr Prompt) {
			p.Mu.Lock()
			p.Clients["desktop"] = "rotated"
			p.Mu.Unlock()
			browse(t)(pr)
		}
		assert.ErrorContains(t, c.SignInBrowser(ctx, show, nil), "the identity provider")
	})
	t.Run("the device client unknown", func(t *testing.T) {
		p, _, c, _, _ := setup(t)
		delete(p.Clients, "device")
		assert.ErrorContains(t, c.SignInDevice(ctx, func(Prompt) {}), "the identity provider")
	})
	t.Run("the device code refused", func(t *testing.T) {
		p, _, c, _, _ := setup(t)
		p.Denied = true
		assert.ErrorContains(t, c.SignInDevice(ctx, func(Prompt) {}), "access_denied")
	})
	t.Run("nothing to paste", func(t *testing.T) {
		_, _, c, _, _ := setup(t)
		pasted := make(chan string)
		close(pasted)
		require.NoError(t, c.SignInBrowser(ctx, browse(t), pasted))
	})
	t.Run("not an address", func(t *testing.T) {
		_, err := Discover(ctx, http.DefaultClient, "http://bad\x7f host")
		assert.Error(t, err)
	})
}

// The organization's models, asked again with their ETag (EN-21).
func TestModels(t *testing.T) {
	_, s, c, _, _ := setup(t)
	ctx := context.Background()
	_, err := c.Models(ctx)
	assert.ErrorIs(t, err, ErrNotSignedIn)
	require.NoError(t, c.SignInBrowser(ctx, browse(t), nil))

	list, err := c.Models(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, Model{ID: "fast", DisplayName: "Fast", Vendor: "google", API: APIResponses, Price: &Price{0.75, 3.75}}, list[0])
	again, err := c.Models(ctx)
	require.NoError(t, err)
	assert.Equal(t, list, again, "unchanged: 304, the list kept")
	assert.Equal(t, 2, s.ModelCalls)

	cases := map[string]struct{ id, want string }{
		"offered":     {"fast", "fast"},
		"not offered": {"gpt-9", "smart"},
		"none named":  {"", "smart"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			m, ok := Find(list, tc.id)
			assert.True(t, ok)
			assert.Equal(t, tc.want, m.ID)
		})
	}
	t.Run("no default: the first", func(t *testing.T) {
		m, ok := Find(list[:1], "gpt-9")
		assert.True(t, ok)
		assert.Equal(t, "fast", m.ID)
		_, ok = Find(nil, "fast")
		assert.False(t, ok)
	})
	t.Run("refused", func(t *testing.T) {
		s.ModelsFail = true
		defer func() { s.ModelsFail = false }()
		_, err := c.Models(ctx)
		var ref *Refusal
		require.ErrorAs(t, err, &ref)
		assert.Equal(t, "terms_required", ref.Code)
	})
	t.Run("an access token the service no longer takes is renewed next time", func(t *testing.T) {
		s.Mu.Lock()
		s.Access = map[string]string{}
		s.Mu.Unlock()
		_, err := c.Models(ctx)
		assert.Error(t, err)
		_, err = c.Models(ctx)
		assert.NoError(t, err)
	})
	t.Run("unreachable", func(t *testing.T) {
		s.Close()
		_, err := c.Models(ctx)
		assert.ErrorContains(t, err, "reaching the organization's service")
	})
}

func TestShared(t *testing.T) {
	made := 0
	store := func() secrets.Store { made++; return &secrets.Memory{} }
	a := Shared("https://shared.example", store, Options{})
	assert.Same(t, a, Shared("https://shared.example", store, Options{}))
	assert.Equal(t, 1, made)
	mine := New("https://shared.example", &secrets.Memory{}, Options{})
	Register(mine)
	assert.Same(t, mine, Shared("https://shared.example", store, Options{}))
}
