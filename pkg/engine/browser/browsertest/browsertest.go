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

// Package browsertest is a fake DevTools page, to test code driving a
// browser without one: it answers the commands package browser sends as a
// simple page would, and records them.
package browsertest

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
)

// PNG is the screenshot the fake takes: a PNG signature and nothing more.
var PNG = []byte("\x89PNG\r\n\x1a\nfake")

// Page is a fake page. Navigating loads Title at the URL (after sending a
// main-frame document request through Fetch.requestPaused, answered by the
// browser's navigation decision); Away is a link going to another URL.
type Page struct {
	// URL is the DevTools WebSocket address to Attach to.
	URL string

	mu      sync.Mutex
	methods []string
	page    struct{ URL, Title string }
	paused  map[string]string // request ID → URL waiting for a decision
	decided map[string]string // request ID → continue or fail
}

// Title is every page's title.
const Title = "Fake page"

// New starts a fake page, closed with the test.
func New(t testing.TB) *Page {
	p := &Page{paused: map[string]string{}, decided: map[string]string{}}
	p.page.URL, p.page.Title = "about:blank", ""
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		p.serve(ws)
	}))
	t.Cleanup(srv.Close)
	p.URL = "ws" + strings.TrimPrefix(srv.URL, "http")
	return p
}

// Methods are the commands received so far.
func (p *Page) Methods() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string{}, p.methods...)
}

// Decided is what the browser decided about each page load: "continue" or
// "fail", by URL.
func (p *Page) Decided() map[string]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := map[string]string{}
	for id, d := range p.decided {
		out[p.paused[id]] = d
	}
	return out
}

type msg struct {
	ID     int64           `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func (p *Page) serve(ws *websocket.Conn) {
	send := func(v any) { ws.WriteJSON(v) }
	event := func(method string, params any) { send(map[string]any{"method": method, "params": params}) }
	reply := func(id int64, result any) { send(map[string]any{"id": id, "result": result}) }
	load := func(url string) {
		p.mu.Lock()
		p.page.URL, p.page.Title = url, Title
		p.mu.Unlock()
		event("Page.frameStartedNavigating", map[string]any{"frameId": "main"})
		event("Page.frameNavigated", map[string]any{"frame": map[string]any{"id": "main"}})
		event("Runtime.consoleAPICalled", map[string]any{"type": "log", "args": []any{map[string]any{"type": "string", "value": "loaded"}, map[string]any{"type": "number", "value": 1}}})
		event("Page.loadEventFired", map[string]any{})
	}
	requestID := 0
	pause := func(url string) {
		requestID++
		id := "r" + strconv.Itoa(requestID)
		p.mu.Lock()
		p.paused[id] = url
		p.mu.Unlock()
		event("Fetch.requestPaused", map[string]any{"requestId": id, "frameId": "main", "resourceType": "Document", "request": map[string]any{"url": url}})
	}
	for {
		var m msg
		if ws.ReadJSON(&m) != nil {
			return
		}
		p.mu.Lock()
		p.methods = append(p.methods, m.Method)
		p.mu.Unlock()
		var params map[string]any
		json.Unmarshal(m.Params, &params)
		switch m.Method {
		case "Page.getFrameTree":
			reply(m.ID, map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "main"}}})
		case "Page.navigate":
			url, _ := params["url"].(string)
			reply(m.ID, map[string]any{"frameId": "main"})
			pause(url)
			load(url)
		case "Fetch.continueRequest", "Fetch.failRequest":
			id, _ := params["requestId"].(string)
			p.mu.Lock()
			p.decided[id] = map[string]string{"Fetch.continueRequest": "continue", "Fetch.failRequest": "fail"}[m.Method]
			p.mu.Unlock()
			reply(m.ID, map[string]any{})
		case "Runtime.evaluate":
			reply(m.ID, p.evaluate(params["expression"].(string), pause, load))
		case "Page.captureScreenshot":
			reply(m.ID, map[string]any{"data": base64.StdEncoding.EncodeToString(PNG)})
		case "Input.dispatchMouseEvent":
			reply(m.ID, map[string]any{})
			if params["type"] == "mouseReleased" {
				pause("https://away.example/")
			}
		default:
			reply(m.ID, map[string]any{})
		}
	}
}

// evaluate answers the scripts package browser runs, by what they contain.
func (p *Page) evaluate(expr string, pause func(string), load func(string)) map[string]any {
	value := func(v any) map[string]any {
		t := "string"
		switch v.(type) {
		case bool:
			t = "boolean"
		case float64, int:
			t = "number"
		case nil:
			t = "object"
		}
		return map[string]any{"result": map[string]any{"type": t, "value": v}}
	}
	missing := strings.Contains(expr, `"#missing"`)
	switch {
	case strings.Contains(expr, "throw"):
		return map[string]any{"result": map[string]any{"type": "object"}, "exceptionDetails": map[string]any{"text": "Uncaught", "exception": map[string]any{"description": "Error: boom"}}}
	case strings.Contains(expr, "location.href"):
		p.mu.Lock()
		defer p.mu.Unlock()
		j, _ := json.Marshal(map[string]string{"url": p.page.URL, "title": p.page.Title})
		return value(string(j))
	case strings.Contains(expr, "document.readyState"):
		return value("complete")
	case strings.Contains(expr, "history.back"):
		load("https://back.example/")
		return value(true)
	case strings.Contains(expr, "r.left + r.width"):
		if missing {
			return value("")
		}
		return value(`{"x":10,"y":20}`)
	case strings.Contains(expr, "e.focus()"):
		return value(!missing)
	case strings.Contains(expr, "e.options"):
		switch {
		case missing:
			return value("none")
		case strings.Contains(expr, `"Huge"`):
			return value("no option")
		}
		return value("ok")
	case strings.Contains(expr, "outerHTML"):
		return value("<h1>Fake</h1>")
	case strings.Contains(expr, "innerText"):
		if missing {
			return value(nil)
		}
		return value(strings.Repeat("Hello from the fake page. ", 10))
	case strings.Contains(expr, "undefined"):
		return map[string]any{"result": map[string]any{"type": "undefined"}}
	}
	return value(float64(2))
}
