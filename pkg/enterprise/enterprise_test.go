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
	"crypto/ed25519"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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
	_, known := list[0].Caps()
	assert.False(t, known, "the service says nothing of what it can do")
	caps, known := list[1].Caps()
	assert.True(t, known)
	assert.Equal(t, Capabilities{Tools: true, Images: true, Thinking: true, Context: 200000}, caps)
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

// The organization's branding: fetched without sign-in, its signature
// checked, kept on disk and checked again when read, asked again with its
// ETag, a forged copy refused (the last one kept), gone when the service
// has none, and removed on disconnecting (EN-50, EN-54).
func TestBranding(t *testing.T) {
	_, s, c, _, _ := setup(t)
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "enterprise", "blitz.acme.com")

	b, err := c.RefreshBranding(ctx, dir)
	require.NoError(t, err)
	assert.Nil(t, b, "none published")
	assert.Nil(t, LoadBranding(dir))

	s.Branding = map[string]any{"name": "Acme Assist", "short_name": "Acme", "accent": "#1a56db", "accent_dark": "#7aa7ff",
		"images": map[string]string{"logo.svg": "data:image/svg+xml;base64,PHN2Zy8+"}, "terms": []map[string]string{{"document": "terms", "version": "v1", "text": "Be kind."}}}
	b, err = c.RefreshBranding(ctx, dir)
	require.NoError(t, err)
	require.NotNil(t, b)
	assert.Equal(t, "Acme Assist", b.Name)
	assert.Equal(t, "#7aa7ff", b.AccentDark)
	assert.Equal(t, "Be kind.", b.Terms[0].Text)
	assert.Equal(t, b, LoadBranding(dir), "kept, and checked again when read")
	assert.Equal(t, "Acme Assist", KeptName(dir))
	assert.Empty(t, KeptName(""), "no service")
	assert.Empty(t, KeptName(t.TempDir()), "nothing kept")
	info, err := os.Stat(filepath.Join(dir, "branding.json"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	again, err := c.RefreshBranding(ctx, dir)
	require.NoError(t, err)
	assert.Equal(t, b, again, "unchanged: 304")

	t.Run("the sign-in's page shows it", func(t *testing.T) {
		signIn := New(s.URL, &secrets.Memory{}, Options{BrandingDir: dir})
		pages := make(chan string, 1)
		show := func(p Prompt) {
			go func() {
				res, err := http.Get(p.URL)
				if assert.NoError(t, err) {
					defer res.Body.Close()
					body, _ := io.ReadAll(res.Body)
					pages <- string(body)
				}
			}()
		}
		require.NoError(t, signIn.SignInBrowser(ctx, show, nil))
		page := <-pages
		assert.Contains(t, page, "Blitz is signed in to Acme Assist.")
		assert.Contains(t, page, `src="data:image/svg&#43;xml;base64,PHN2Zy8&#43;"`)
		assert.Contains(t, page, "--primary: #1a56db")
		assert.Contains(t, page, `href="blitz://show"`)
	})

	t.Run("a copy changed on disk isn't shown", func(t *testing.T) {
		other := t.TempDir()
		require.NoError(t, os.CopyFS(other, os.DirFS(dir)))
		data, _ := os.ReadFile(filepath.Join(other, "branding.json"))
		require.NoError(t, os.WriteFile(filepath.Join(other, "branding.json"), []byte(strings.Replace(string(data), `"payload":"`, `"payload":"x`, 1)), 0o600))
		assert.Nil(t, LoadBranding(other))
		require.NoError(t, os.Remove(filepath.Join(other, "keys.json")))
		assert.Nil(t, LoadBranding(other), "no keys")
	})
	t.Run("a copy signed by another key is refused, the last one kept", func(t *testing.T) {
		s.Forged = true
		s.Branding["name"] = "Evil Corp"
		got, err := c.RefreshBranding(ctx, dir)
		assert.ErrorIs(t, err, ErrSignature)
		assert.Equal(t, "Acme Assist", got.Name)
		s.Forged = false
	})
	t.Run("a new key, published since", func(t *testing.T) {
		_, s.Key, _ = ed25519.GenerateKey(nil)
		s.KeyID = "catalog-2"
		s.Branding["name"] = "Acme Assist 2"
		got, err := c.RefreshBranding(ctx, dir)
		require.NoError(t, err)
		assert.Equal(t, "Acme Assist 2", got.Name)
	})
	t.Run("the service unreachable: the copy kept", func(t *testing.T) {
		gone := New("http://127.0.0.1:1", &secrets.Memory{}, Options{})
		got, err := gone.RefreshBranding(ctx, dir)
		assert.Error(t, err)
		assert.Equal(t, "Acme Assist 2", got.Name)
	})
	t.Run("a refusal: the copy kept", func(t *testing.T) {
		odd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
		t.Cleanup(odd.Close)
		got, err := New(odd.URL, &secrets.Memory{}, Options{}).RefreshBranding(ctx, dir)
		var ref *Refusal
		assert.ErrorAs(t, err, &ref)
		assert.Equal(t, "Acme Assist 2", got.Name)
	})

	s.Branding = nil
	b, err = c.RefreshBranding(ctx, dir)
	require.NoError(t, err)
	assert.Nil(t, b, "withdrawn: Blitz's look")
	assert.Nil(t, LoadBranding(dir))

	require.NoError(t, ForgetBranding(dir))
	assert.NoDirExists(t, dir)
	assert.NoError(t, ForgetBranding(""))
}

// The terms: the version in force, accepted or not, and accepting it
// (EN-53).
func TestTerms(t *testing.T) {
	_, s, c, _, _ := setup(t)
	ctx := context.Background()
	_, err := c.Terms(ctx)
	assert.ErrorIs(t, err, ErrNotSignedIn)
	assert.ErrorIs(t, c.AcceptTerms(ctx, "v1"), ErrNotSignedIn)
	require.NoError(t, c.SignInBrowser(ctx, browse(t), nil))

	got, err := c.Terms(ctx)
	require.NoError(t, err)
	assert.True(t, got.Accepted, "none in force")

	s.Version = "v2"
	got, err = c.Terms(ctx)
	require.NoError(t, err)
	assert.Equal(t, Terms{Version: "v2", Text: "Be kind."}, got)
	assert.Equal(t, "v2", c.Status().Terms)

	var ref *Refusal
	require.ErrorAs(t, c.AcceptTerms(ctx, "v1"), &ref)
	assert.Equal(t, http.StatusConflict, ref.Status)
	require.NoError(t, c.AcceptTerms(ctx, "v2"))
	assert.Empty(t, c.Status().Terms)
	got, err = c.Terms(ctx)
	require.NoError(t, err)
	assert.True(t, got.Accepted)

	s.Close()
	_, err = c.Terms(ctx)
	assert.Error(t, err)
	assert.Error(t, c.AcceptTerms(ctx, "v2"))
}

// The organization's catalog: fetched once signed in, checked, its files
// fetched by hash and laid out as default and locked layers with the
// policy beside them, asked again with its ETag, files kept across
// versions, a file that doesn't match its hash refused (the last catalog
// kept), and removed when the service withdraws it (EN-30 to EN-33).
func TestCatalog(t *testing.T) {
	_, s, c, _, _ := setup(t)
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "blitz.acme.com")
	_, _, err := c.RefreshCatalog(ctx, dir)
	assert.ErrorIs(t, err, ErrNotSignedIn)
	require.NoError(t, c.SignInBrowser(ctx, browse(t), nil))

	m, changed, err := c.RefreshCatalog(ctx, dir)
	require.NoError(t, err)
	assert.Nil(t, m, "none published")
	assert.False(t, changed)

	s.Catalog = map[string]enterprisetest.CatalogFile{
		"agents/reviewer.md":     {Kind: "agent", Name: "reviewer", Lock: "locked", Data: "---\nname: reviewer\n---\nReview."},
		"rules/style.md":         {Kind: "rule", Name: "style", Lock: "default", Data: "Use Go."},
		"skills/deploy/SKILL.md": {Kind: "skill", Name: "deploy", Lock: "default", Data: "---\nname: deploy\n---\nDeploy."},
		"skills/deploy/run.sh":   {Kind: "skill_file", Name: "deploy", Lock: "default", Data: "echo hi"},
	}
	s.CatalogPolicy = map[string]any{"models": map[string]any{"own_providers": true}, "tools": map[string]any{"off": []string{"web_fetch"}}}
	s.CatalogExpiry = time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	m, changed, err = c.RefreshCatalog(ctx, dir)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.True(t, changed)
	assert.Len(t, m.Files, 4)
	assert.True(t, m.Policy.Models.OwnProviders)
	assert.Equal(t, []string{"web_fetch"}, m.Policy.Tools.Off)
	assert.Equal(t, s.CatalogExpiry, m.Expires.UTC())
	defaults, locked := LayerDirs(dir)
	data, err := os.ReadFile(filepath.Join(locked, "agents", "reviewer.md"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "Review.")
	assert.FileExists(t, filepath.Join(defaults, "rules", "style.md"))
	assert.FileExists(t, filepath.Join(defaults, "skills", "deploy", "run.sh"))
	assert.NoFileExists(t, filepath.Join(defaults, "agents", "reviewer.md"))
	policy, err := os.ReadFile(filepath.Join(dir, PolicyFile))
	require.NoError(t, err)
	assert.Contains(t, string(policy), `"own_providers":true`)
	assert.Equal(t, m, LoadCatalog(dir))

	again, changed, err := c.RefreshCatalog(ctx, dir)
	require.NoError(t, err)
	assert.False(t, changed, "unchanged: 304")
	assert.Equal(t, m, again)

	t.Run("a new version: only its new files fetched", func(t *testing.T) {
		calls := s.FileCalls
		s.Catalog["commands/ship.md"] = enterprisetest.CatalogFile{Kind: "command", Name: "ship", Lock: "default", Data: "Ship it."}
		delete(s.Catalog, "rules/style.md")
		m, changed, err := c.RefreshCatalog(ctx, dir)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Len(t, m.Files, 4)
		assert.Equal(t, calls+1, s.FileCalls)
		assert.NoFileExists(t, filepath.Join(defaults, "rules", "style.md"), "gone from the layers")
		assert.FileExists(t, filepath.Join(defaults, "commands", "ship.md"))
	})
	t.Run("a file that doesn't match its hash: the last catalog kept", func(t *testing.T) {
		s.Catalog["rules/bad.md"] = enterprisetest.CatalogFile{Kind: "rule", Name: "bad", Lock: "default", Data: "Bad.", Corrupt: true}
		got, _, err := c.RefreshCatalog(ctx, dir)
		assert.ErrorIs(t, err, ErrSignature)
		assert.Len(t, got.Files, 4)
		assert.NoFileExists(t, filepath.Join(defaults, "rules", "bad.md"))
		delete(s.Catalog, "rules/bad.md")
	})
	t.Run("a path outside the catalog is refused", func(t *testing.T) {
		s.Catalog["../escape.md"] = enterprisetest.CatalogFile{Kind: "rule", Name: "escape", Lock: "default", Data: "Out."}
		_, _, err := c.RefreshCatalog(ctx, dir)
		assert.ErrorContains(t, err, "outside it")
		assert.NoFileExists(t, filepath.Join(dir, "catalog", "escape.md"))
		delete(s.Catalog, "../escape.md")
	})
	t.Run("forged: the last catalog kept", func(t *testing.T) {
		s.Forged = true
		s.Catalog["rules/evil.md"] = enterprisetest.CatalogFile{Kind: "rule", Name: "evil", Lock: "locked", Data: "Evil."}
		got, _, err := c.RefreshCatalog(ctx, dir)
		assert.ErrorIs(t, err, ErrSignature)
		assert.Len(t, got.Files, 4)
		s.Forged = false
		delete(s.Catalog, "rules/evil.md")
	})
	t.Run("the stream announces a version", func(t *testing.T) {
		wctx, cancel := context.WithCancel(ctx)
		defer cancel()
		seen := make(chan struct{}, 1)
		done := make(chan error, 1)
		go func() { done <- c.WatchCatalog(wctx, func() { seen <- struct{}{} }) }()
		s.Announce <- 7
		select {
		case <-seen:
		case <-time.After(5 * time.Second):
			t.Fatal("no event")
		}
		cancel()
		assert.Error(t, <-done)
	})

	s.Catalog = nil
	m, changed, err = c.RefreshCatalog(ctx, dir)
	require.NoError(t, err)
	assert.Nil(t, m, "withdrawn")
	assert.True(t, changed)
	assert.Nil(t, LoadCatalog(dir))
	assert.NoDirExists(t, filepath.Join(dir, "catalog"))

	t.Run("signed out of the stream", func(t *testing.T) {
		_, _, c2, _, _ := setup(t)
		assert.ErrorIs(t, c2.WatchCatalog(ctx, func() {}), ErrNotSignedIn)
	})
	t.Run("the service down: the kept catalog", func(t *testing.T) {
		s.Close()
		_, _, err := c.RefreshCatalog(ctx, dir)
		assert.Error(t, err)
		assert.Error(t, c.WatchCatalog(ctx, func() {}))
	})
}

// What goes wrong with the catalog: a kept copy without its keys or
// changed on disk isn't read, an access token the service no longer takes
// is refused (and renewed next time), a file the service won't give stops
// the version, and the stream refused says so.
func TestCatalogFailures(t *testing.T) {
	_, s, c, _, _ := setup(t)
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "blitz.acme.com")
	require.NoError(t, c.SignInBrowser(ctx, browse(t), nil))
	s.Catalog = map[string]enterprisetest.CatalogFile{"rules/a.md": {Kind: "rule", Name: "a", Lock: "default", Data: "A."}}
	_, _, err := c.RefreshCatalog(ctx, dir)
	require.NoError(t, err)

	t.Run("a kept copy that doesn't check", func(t *testing.T) {
		other := t.TempDir()
		require.NoError(t, os.CopyFS(other, os.DirFS(dir)))
		data, _ := os.ReadFile(filepath.Join(other, "catalog.json"))
		require.NoError(t, os.WriteFile(filepath.Join(other, "catalog.json"), []byte(strings.Replace(string(data), `"payload":"`, `"payload":"x`, 1)), 0o600))
		assert.Nil(t, LoadCatalog(other))
		require.NoError(t, os.Remove(filepath.Join(other, "keys.json")))
		assert.Nil(t, LoadCatalog(other))
		assert.Nil(t, LoadCatalog(t.TempDir()))
	})
	t.Run("an access token the service stopped taking", func(t *testing.T) {
		s.Mu.Lock()
		s.Access = map[string]string{}
		s.Mu.Unlock()
		got, _, err := c.RefreshCatalog(ctx, dir)
		var ref *Refusal
		require.ErrorAs(t, err, &ref)
		assert.Equal(t, http.StatusUnauthorized, ref.Status)
		assert.NotNil(t, got, "the kept one")
		_, _, err = c.RefreshCatalog(ctx, dir)
		assert.NoError(t, err, "renewed")
		s.Mu.Lock()
		s.Access = map[string]string{}
		s.Mu.Unlock()
		assert.Error(t, c.WatchCatalog(ctx, func() {}), "the stream refused")
		c.Invalidate()
	})
	t.Run("a file the service won't give", func(t *testing.T) {
		s.Catalog["rules/b.md"] = enterprisetest.CatalogFile{Kind: "rule", Name: "b", Lock: "default", Data: "B.", Missing: true}
		got, _, err := c.RefreshCatalog(ctx, dir)
		assert.ErrorContains(t, err, "rules/b.md")
		assert.Len(t, got.Files, 1)
		delete(s.Catalog, "rules/b.md")
	})
	t.Run("forgetting what isn't there", func(t *testing.T) {
		assert.NoError(t, ForgetCatalog(t.TempDir()))
	})
	t.Run("the organization's workspaces, signed out", func(t *testing.T) {
		_, _, c2, _, _ := setup(t)
		_, err := c2.Workspaces(ctx)
		assert.ErrorIs(t, err, ErrNotSignedIn)
		_, err = c2.CreateWorkspace(ctx, "x", "", false)
		assert.ErrorIs(t, err, ErrNotSignedIn)
		assert.ErrorIs(t, c2.WatchWorkspace(ctx, "x", func(string, string, string) {}), ErrNotSignedIn)
		assert.Equal(t, "", New("::", &secrets.Memory{}, Options{}).Host())
	})
	t.Run("a workspace's stream", func(t *testing.T) {
		s.AddWorkspace(t, "plans", "Plans", "editor")
		wctx, cancel := context.WithCancel(ctx)
		defer cancel()
		seen := make(chan string, 1)
		done := make(chan error, 1)
		go func() { done <- c.WatchWorkspace(wctx, "plans", func(ref, _, by string) { seen <- ref + " " + by }) }()
		s.Pushes <- "plans"
		select {
		case got := <-seen:
			assert.Equal(t, "refs/heads/main bo@acme.com", got)
		case <-time.After(5 * time.Second):
			t.Fatal("no push")
		}
		cancel()
		assert.Error(t, <-done)
		s.Mu.Lock()
		s.Workspaces["../escape"] = enterprisetest.Workspace{Name: "Escape", Role: "owner"}
		s.Mu.Unlock()
		list, err := c.Workspaces(ctx)
		require.NoError(t, err)
		require.Len(t, list, 1, "never a handle that isn't one")
		assert.Equal(t, "plans", list[0].Handle)
		made, err := c.CreateWorkspace(ctx, "Q4", "q4", false)
		require.NoError(t, err)
		assert.Equal(t, "q4", made.Handle)
		assert.NotEmpty(t, s.Repo("q4"))
	})
	t.Run("the service down", func(t *testing.T) {
		s.Close()
		_, err := c.Workspaces(ctx)
		assert.Error(t, err)
		assert.Error(t, c.WatchWorkspace(ctx, "plans", func(string, string, string) {}))
	})
}

// A handle is a team's word or a person's ~<person>/<name>, nothing that
// could be a folder elsewhere.
func TestValidHandle(t *testing.T) {
	for h, want := range map[string]bool{"plans": true, "~ana/home": true, "~ana-2/q4-plan": true,
		"": false, "../escape": false, "~ana/../x": false, "~ana/home/more": false, "Plans": false, "/abs": false, "~/home": false} {
		assert.Equal(t, want, ValidHandle(h), h)
	}
}

// A folder that can't be written: the branding and catalog say so.
func TestUnwritable(t *testing.T) {
	_, s, c, _, _ := setup(t)
	ctx := context.Background()
	require.NoError(t, c.SignInBrowser(ctx, browse(t), nil))
	file := filepath.Join(t.TempDir(), "a-file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	s.Branding = map[string]any{"name": "Acme Assist"}
	_, err := c.RefreshBranding(ctx, filepath.Join(file, "dir"))
	assert.Error(t, err)
	s.Catalog = map[string]enterprisetest.CatalogFile{"rules/a.md": {Kind: "rule", Name: "a", Lock: "default", Data: "A."}}
	_, _, err = c.RefreshCatalog(ctx, filepath.Join(file, "dir"))
	assert.Error(t, err)
	assert.Error(t, writeFile(filepath.Join(file, "dir"), "x", []byte("x")))
}
