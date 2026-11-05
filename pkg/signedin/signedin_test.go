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

package signedin

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWrite(t *testing.T) {
	cases := map[string]struct {
		status   int
		page     Page
		want     []string
		dontWant []string
	}{
		"signed in, from the app": {http.StatusOK, Page{Title: "You're signed in", Message: "Go back to Blitz.", Open: true},
			[]string{"<h1>You&#39;re signed in</h1>", "Go back to Blitz.", `href="blitz://show"`, "Open Blitz", ">Signed in</span>"},
			[]string{"card failed", "Not signed in"}},
		"signed in, from a terminal": {http.StatusOK, Page{Title: "You're signed in", Message: "Go back to your terminal."},
			[]string{"Go back to your terminal."}, []string{"blitz://show"}},
		"refused": {http.StatusBadRequest, Page{Title: "Not this time", Message: "<script>x</script>", Failed: true},
			[]string{`class="card failed"`, "Not signed in", "&lt;script&gt;"}, []string{"<script>"}},
		"an organization's": {http.StatusOK, Page{Title: "Hi", Brand: &Brand{Name: "Acme", Logo: "data:image/png;base64,AAAA",
			LogoDark: "data:image/svg+xml;base64,BBBB", Accent: "#1a56db", AccentDark: "#7aa7ff"}},
			[]string{"<title>Hi · Acme</title>", `src="data:image/png;base64,AAAA"`, `class="org-logo light-only"`, `src="data:image/svg&#43;xml;base64,BBBB"`,
				"(prefers-color-scheme: light) { :root { --primary: #1a56db; --on-primary: #ffffff; } }",
				"(prefers-color-scheme: dark) { :root { --primary: #7aa7ff; --on-primary: #000000; } }", `class="by"`},
			[]string{`class="logo"`}},
		"an organization's name alone": {http.StatusOK, Page{Title: "Hi", Brand: &Brand{Name: "Acme", Logo: "javascript:alert(1)", LogoDark: "data:text/html,x", Accent: "#ffff00"}},
			[]string{`<p class="org-name">Acme</p>`, "(prefers-color-scheme: dark) { :root { --primary: #ffff00"},
			[]string{"javascript", "text/html", "light) { :root { --primary: #ffff00"}},
		"only a dark logo": {http.StatusOK, Page{Title: "Hi", Brand: &Brand{Name: "Acme", LogoDark: "data:image/png;base64,DDDD", Accent: "red"}},
			[]string{`class="org-logo"`, "DDDD"}, []string{"dark-only\" src", "red;"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			Write(w, c.status, c.page)
			assert.Equal(t, c.status, w.Code)
			assert.Equal(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"))
			assert.Contains(t, w.Header().Get("Content-Security-Policy"), "default-src 'none'")
			for _, s := range c.want {
				assert.Contains(t, w.Body.String(), s)
			}
			for _, s := range c.dontWant {
				assert.NotContains(t, w.Body.String(), s)
			}
		})
	}
}

// Close lets the page being written go out whole: the sign-in moves on
// once the handler has the code, before the page is sent (the browser got
// half a page, or none, now and then).
func TestCloseSendsThePage(t *testing.T) {
	got := make(chan struct{})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Write(w, http.StatusOK, Page{Title: "Signed in", Message: "All done."})
		close(got) // the sign-in carries on from here
		time.Sleep(50 * time.Millisecond)
	})}
	go srv.Serve(ln)
	pages := make(chan string, 1)
	go func() {
		res, err := http.Get("http://" + ln.Addr().String() + "/")
		if assert.NoError(t, err) {
			defer res.Body.Close()
			body, err := io.ReadAll(res.Body)
			assert.NoError(t, err, "the page read whole")
			pages <- string(body)
		}
	}()
	<-got
	Close(srv)
	page := <-pages
	assert.Contains(t, page, "All done.")
	assert.Contains(t, page, "</html>")
}
