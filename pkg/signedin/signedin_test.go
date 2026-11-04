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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
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
