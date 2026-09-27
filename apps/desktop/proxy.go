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

package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/retail-cortex/blitz/pkg/socket"
)

// apiPrefix is where the service's API lives; the page's requests there go
// to the service.
const apiPrefix = "/blitz.v1."

// serviceProxy forwards the page's API requests to the service's Unix
// socket (a web view can't open one itself), streaming responses as they
// come. Anything else is not found: the page's own files are served by
// Wails.
func serviceProxy(path string) http.Handler {
	target, _ := url.Parse(socket.BaseURL)
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.Host = target.Host
		},
		Transport:     socket.Client(path).Transport,
		FlushInterval: -1, // turns stream their events
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			// Connect's JSON error shape, so the page's client reads it.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"code":"unavailable","message":"the Blitz service isn't answering"}`))
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, apiPrefix) {
			http.NotFound(w, r)
			return
		}
		proxy.ServeHTTP(w, r)
	})
}
