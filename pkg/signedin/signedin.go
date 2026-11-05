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
// Blitz when the desktop app asked for it, in the organization's branding
// when it has one (EN-50).
package signedin

import (
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
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
	// Brand is the organization's look; nil for Blitz's own.
	Brand *Brand
}

// Brand is an organization's look on the page: its name, its logo for
// each theme (data URLs of pictures), and its accent for each theme
// ("#rrggbb"), each left out when it isn't one or wouldn't read.
type Brand struct {
	Name, Logo, LogoDark, Accent, AccentDark string
}

// view is what the template shows: the brand's parts that passed.
type view struct {
	Page
	Name           string
	Logo, LogoDark template.URL
	Accent         template.CSS
}

// picture is a data URL of a picture a page can show.
var picture = regexp.MustCompile(`^data:image/(png|jpeg|gif|webp|svg\+xml)[;,]`)

func pictureURL(s string) template.URL {
	if !picture.MatchString(s) {
		return ""
	}
	return template.URL(s) // a picture, safe in an img (scripts don't run there)
}

var hexColour = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// luminance is WCAG's relative luminance of "#rrggbb".
func luminance(hex string) float64 {
	var l float64
	for i, w := range []float64{0.2126, 0.7152, 0.0722} {
		v, _ := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		c := float64(v) / 255
		if c <= 0.03928 {
			c /= 12.92
		} else {
			c = math.Pow((c+0.055)/1.055, 2.4)
		}
		l += w * c
	}
	return l
}

// contrast is WCAG's contrast ratio of two "#rrggbb" colours, 1 to 21.
func contrast(a, b string) float64 {
	x, y := luminance(a), luminance(b)
	return (max(x, y) + 0.05) / (min(x, y) + 0.05)
}

// accentRules sets the page's primary for a theme to accent, with the ink
// on it, as the app does (orgBranding.tsx accentRules): "" when it isn't a
// colour or reads poorly on that theme's background.
func accentRules(accent, scheme, background string) string {
	if !hexColour.MatchString(accent) || contrast(accent, background) < 4.5 {
		return ""
	}
	on := "#000000"
	if contrast(accent, "#ffffff") >= contrast(accent, "#000000") {
		on = "#ffffff"
	}
	rule := fmt.Sprintf(":root { --primary: %s; --on-primary: %s; }", accent, on)
	return "@media (prefers-color-scheme: " + scheme + ") { " + rule + " }"
}

func (p Page) view() view {
	v := view{Page: p}
	if b := p.Brand; b != nil {
		v.Name = b.Name
		v.Logo, v.LogoDark = pictureURL(b.Logo), pictureURL(b.LogoDark)
		if v.Logo == "" {
			v.Logo, v.LogoDark = v.LogoDark, ""
		}
		dark := b.AccentDark
		if dark == "" {
			dark = b.Accent
		}
		css := strings.TrimSpace(accentRules(b.Accent, "light", "#ffffff") + "\n" + accentRules(dark, "dark", "#131314"))
		v.Accent = template.CSS(css) // built here from checked colours only
	}
	return v
}

// closeWait is how long Close waits for a page being written.
var closeWait = time.Second

// Close stops the sign-in's local server once the page it's writing has
// gone out (at most closeWait), so the browser never gets half a page: the
// sign-in carries on as soon as the page's handler has the code, before
// the page is sent.
func Close(srv *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), closeWait)
	defer cancel()
	if srv.Shutdown(ctx) != nil {
		_ = srv.Close()
	}
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
	_ = tmpl.Execute(w, p.view())
}
