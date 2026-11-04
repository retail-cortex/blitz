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

// Package signedin is the page a browser sign-in ends on, on this
// computer's callback (the organization's, spec_053_enterprise EN-12, and
// an MCP server's): a card on Blitz's glass saying how it went, with Open
// Blitz when the desktop app asked for it.
package signedin

import (
	_ "embed"
	"html/template"
	"net/http"
)

//go:embed page.html
var page string

var tmpl = template.Must(template.New("page").Parse(page))

// Page is what the card says.
type Page struct {
	// Title is the card's heading; Message the line below it.
	Title, Message string
	// Failed shows the sign-in didn't go through.
	Failed bool
	// Open adds Open Blitz, a blitz://show link that brings the desktop
	// app forward: for a sign-in the app started.
	Open bool
}

// Write writes p to w as the answer, with status.
func Write(w http.ResponseWriter, status int, p Page) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	// Nothing to run or fetch: the page is its own styles and picture.
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'none'")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = tmpl.Execute(w, p)
}
