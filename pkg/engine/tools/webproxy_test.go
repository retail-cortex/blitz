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
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Through a proxy, web_fetch checks where the host resolves before the
// proxy connects; the proxy itself may be on a private address.
func TestWebFetchThroughProxy(t *testing.T) {
	var asked []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.String())
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "via the proxy")
	}))
	defer proxy.Close()
	pu, _ := url.Parse(proxy.URL)
	oldProxy, oldLookup := proxyFromEnv, lookupHost
	t.Cleanup(func() { proxyFromEnv, lookupHost = oldProxy, oldLookup })
	proxyFromEnv = func(*http.Request) (*url.URL, error) { return pu, nil }
	lookupHost = func(_ context.Context, host string) ([]netip.Addr, error) {
		if host == "intranet.example" {
			return []netip.Addr{netip.MustParseAddr("10.1.2.3")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("93.184.215.14")}, nil
	}

	tests := []struct {
		name, url    string
		allowPrivate bool
		want, err    string
	}{
		{name: "a public host", url: "http://site.example/page", want: "via the proxy"},
		{name: "a private one", url: "http://intranet.example/", err: "not a public address"},
		{name: "a private one, allowed", url: "http://intranet.example/", allowPrivate: true, want: "via the proxy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newWebFetcher(WebFetchConfig{AllowNetwork: true, AllowPrivate: tt.allowPrivate, AllowDomains: []string{"*"}})
			out := f.fetch(context.Background(), allowAll(), tt.url)
			if tt.err != "" {
				assert.Contains(t, out.Error, tt.err)
				return
			}
			assert.Empty(t, out.Error)
			assert.Equal(t, tt.want, out.Content)
		})
	}
	assert.Equal(t, []string{"http://site.example/page", "http://intranet.example/"}, asked)
	assert.Equal(t, "127.0.0.1:80", canonicalAddr(&url.URL{Scheme: "http", Host: "127.0.0.1"}))
}
