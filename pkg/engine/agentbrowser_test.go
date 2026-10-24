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

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/browser"
	"github.com/retail-cortex/blitz/pkg/engine/browser/browsertest"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeWindow is a desktop window hosting the agent's browser views: each
// view it opens is a connection to a fake DevTools page, its messages
// relayed as the app relays webContents.debugger's.
type fakeWindow struct {
	t      *testing.T
	w      *Workspace
	page   string
	mu     sync.Mutex
	opened []api.AgentBrowserCommand
	views  map[string]*websocket.Conn
	refuse bool
}

func (f *fakeWindow) serve(ctx context.Context) {
	f.views = map[string]*websocket.Conn{}
	// A window's views close with it.
	defer func() {
		for _, ws := range f.views {
			ws.Close()
		}
	}()
	_ = f.w.WatchAgentBrowser(ctx, func(c api.AgentBrowserCommand) error {
		switch c.Kind {
		case "open":
			f.mu.Lock()
			f.opened = append(f.opened, c)
			f.mu.Unlock()
			if f.refuse {
				return f.w.AgentBrowserReply(c.BrowserID, nil, true, "the window refused")
			}
			ws, _, err := websocket.DefaultDialer.Dial(f.page, nil)
			require.NoError(f.t, err)
			f.views[c.BrowserID] = ws
			go func(id string) {
				for {
					_, data, err := ws.ReadMessage()
					if err != nil {
						_ = f.w.AgentBrowserReply(id, nil, true, "")
						return
					}
					_ = f.w.AgentBrowserReply(id, []string{string(data)}, false, "")
				}
			}(c.BrowserID)
			return f.w.AgentBrowserReply(c.BrowserID, []string{openedMessage}, false, "")
		case "message":
			return f.views[c.BrowserID].WriteMessage(websocket.TextMessage, []byte(c.Message))
		case "close":
			if ws := f.views[c.BrowserID]; ws != nil {
				ws.Close()
			}
		}
		return nil
	})
}

// The agent's browser in the app (AB-01): with a window watching, the
// view it opens is driven through the relay, as the profile and options
// say; without one, there's none to open.
func TestAgentBrowserRelay(t *testing.T) {
	w := openTest(t)
	ctx := context.Background()
	_, err := w.openAgentBrowser(ctx, tools.AgentBrowserOptions{Show: "watch"})
	assert.ErrorIs(t, err, ErrNoBrowserWindow, "no window")

	page := browsertest.New(t)
	win := &fakeWindow{t: t, w: w, page: page.URL}
	wctx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { win.serve(wctx); close(done) }()
	require.Eventually(t, func() bool {
		w.browsers.mu.Lock()
		defer w.browsers.mu.Unlock()
		return len(w.browsers.windows) == 1
	}, 2*time.Second, 5*time.Millisecond)

	tr, err := w.openAgentBrowser(ctx, tools.AgentBrowserOptions{Show: "silent", Profile: "work", AllowLocal: true, DenyDomains: []string{"*.blocked.example"}})
	require.NoError(t, err)
	b, err := browser.AttachTransport(ctx, tr, browser.Options{KeepViewport: false})
	require.NoError(t, err)
	p, err := b.Navigate(ctx, "https://shop.example/")
	require.NoError(t, err)
	assert.Equal(t, browsertest.Title, p.Title, "driven through the relay")
	win.mu.Lock()
	open := win.opened[0]
	win.mu.Unlock()
	assert.Equal(t, "silent", open.Show)
	assert.Equal(t, "work", open.Profile)
	assert.True(t, open.AllowLocal)
	assert.Equal(t, []string{"*.blocked.example"}, open.DenyDomains)

	// The window goes: its views close, and the browser says so.
	stop()
	<-done
	_, err = b.Navigate(ctx, "https://shop.example/again")
	assert.Error(t, err)
	b.Close()
	_, err = w.openAgentBrowser(ctx, tools.AgentBrowserOptions{Show: "watch"})
	assert.ErrorIs(t, err, ErrNoBrowserWindow)
}

// A window that refuses, or doesn't answer, leaves the agent to the
// headless browser.
func TestAgentBrowserNotOpened(t *testing.T) {
	w := openTest(t)
	ctx := context.Background()
	win := &fakeWindow{t: t, w: w, refuse: true}
	wctx, stop := context.WithCancel(ctx)
	defer stop()
	go win.serve(wctx)
	require.Eventually(t, func() bool {
		w.browsers.mu.Lock()
		defer w.browsers.mu.Unlock()
		return len(w.browsers.windows) == 1
	}, 2*time.Second, 5*time.Millisecond)
	_, err := w.openAgentBrowser(ctx, tools.AgentBrowserOptions{Show: "watch"})
	assert.ErrorContains(t, err, "the window refused")

	old := agentBrowserOpenWait
	agentBrowserOpenWait = 20 * time.Millisecond
	t.Cleanup(func() { agentBrowserOpenWait = old })
	silent := &Workspace{}
	go func() {
		_ = silent.WatchAgentBrowser(wctx, func(api.AgentBrowserCommand) error { return nil }) // never answers
	}()
	require.Eventually(t, func() bool {
		silent.browsers.mu.Lock()
		defer silent.browsers.mu.Unlock()
		return len(silent.browsers.windows) == 1
	}, 2*time.Second, 5*time.Millisecond)
	_, err = silent.openAgentBrowser(ctx, tools.AgentBrowserOptions{Show: "watch"})
	assert.ErrorIs(t, err, ErrNoBrowserWindow)
	assert.ErrorContains(t, err, "didn't open")
	assert.Error(t, silent.AgentBrowserReply("nope", nil, false, ""), "an unknown view")
}

// A relay's own ends: a message reaches the window as JSON, and closing
// tells it to close the view.
func TestBrowserRelayEnds(t *testing.T) {
	h := &browserHub{}
	h.init()
	win := &browserWindow{send: make(chan api.AgentBrowserCommand, 4)}
	r := &browserRelay{id: "agent-1", w: win, in: make(chan json.RawMessage, 1), opened: make(chan error, 1), closed: make(chan struct{}), hub: h}
	h.relays[r.id] = r
	require.NoError(t, r.WriteJSON(map[string]any{"id": 1, "method": "Page.navigate"}))
	c := <-win.send
	assert.Equal(t, "message", c.Kind)
	assert.JSONEq(t, `{"id":1,"method":"Page.navigate"}`, c.Message)
	r.in <- json.RawMessage(`{"id":1,"result":{}}`)
	var m map[string]any
	require.NoError(t, r.ReadJSON(&m))
	assert.EqualValues(t, 1, m["id"])
	require.NoError(t, r.Close())
	assert.Equal(t, "close", (<-win.send).Kind)
	assert.Error(t, r.ReadJSON(&m), "closed")
	assert.True(t, errors.Is(r.WriteJSON(1), r.why))
	require.NoError(t, r.Close(), "twice")
}

// A command that can't be written as JSON never reaches the window.
func TestBrowserRelayRefusesWhatIsntJSON(t *testing.T) {
	assert.Error(t, (&browserRelay{}).WriteJSON(make(chan int)))
}
