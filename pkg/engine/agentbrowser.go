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
	"fmt"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/browser"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
)

// The agent's browser in the desktop app (spec_agent_browser_050): a
// window watching the workspace (WatchAgentBrowser) hosts a view of its
// own for the agent, a tab the user watches or one off screen, and relays
// the DevTools Protocol between it and the engine's browser package
// (AgentBrowserReply back). No window: the headless browser, as before.

// ErrNoBrowserWindow means no desktop window is watching the workspace.
var ErrNoBrowserWindow = errors.New("no desktop window to browse in")

// agentBrowserOpenWait is how long a window gets to open its view.
var agentBrowserOpenWait = 15 * time.Second

// openedMessage is the window's first reply: its view is ready.
const openedMessage = `{"blitz":"opened"}`

// browserHub is the workspace's windows and the views they host.
type browserHub struct {
	mu      sync.Mutex
	windows []*browserWindow // newest last
	relays  map[string]*browserRelay
	seq     int
}

type browserWindow struct {
	send chan api.AgentBrowserCommand
}

// browserRelay is one view's DevTools connection: browser.Transport, the
// engine's end.
type browserRelay struct {
	id     string
	w      *browserWindow
	in     chan json.RawMessage
	opened chan error
	closed chan struct{}
	once   sync.Once
	why    error
	hub    *browserHub
}

func (h *browserHub) init() {
	if h.relays == nil {
		h.relays = map[string]*browserRelay{}
	}
}

// WatchAgentBrowser makes the caller (a desktop window, through the
// service) a host of the agent's browser views until ctx ends: each
// command for them goes to send, in order. The newest window hosts new
// views; when a window goes, the views it hosted close.
func (w *Workspace) WatchAgentBrowser(ctx context.Context, send func(api.AgentBrowserCommand) error) error {
	h := &w.browsers
	win := &browserWindow{send: make(chan api.AgentBrowserCommand, 256)}
	h.mu.Lock()
	h.init()
	h.windows = append(h.windows, win)
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		for i, x := range h.windows {
			if x == win {
				h.windows = append(h.windows[:i], h.windows[i+1:]...)
				break
			}
		}
		var gone []*browserRelay
		for _, r := range h.relays {
			if r.w == win {
				gone = append(gone, r)
			}
		}
		h.mu.Unlock()
		for _, r := range gone {
			r.shut(errors.New("the desktop window closed"))
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case cmd := <-win.send:
			if err := send(cmd); err != nil {
				return err
			}
		}
	}
}

// AgentBrowserReply is what a window says of a view it hosts: DevTools
// messages from it (replies and events, in order), that it's closed
// (closed; why in reason), or that it couldn't open it.
func (w *Workspace) AgentBrowserReply(id string, messages []string, closed bool, reason string) error {
	h := &w.browsers
	h.mu.Lock()
	r := h.relays[id]
	h.mu.Unlock()
	if r == nil {
		return fmt.Errorf("no agent browser %q", id)
	}
	for _, m := range messages {
		if m == openedMessage {
			select {
			case r.opened <- nil:
			default:
			}
			continue
		}
		select {
		case r.in <- json.RawMessage(m):
		case <-r.closed:
			return nil
		}
	}
	if closed {
		why := errors.New("the view closed")
		if reason != "" {
			why = errors.New(reason)
		}
		select {
		case r.opened <- why:
		default:
		}
		r.shut(why)
	}
	return nil
}

// openAgentBrowser asks the newest window for a view (tools.AgentBrowserHost).
func (w *Workspace) openAgentBrowser(ctx context.Context, o tools.AgentBrowserOptions) (browser.Transport, error) {
	h := &w.browsers
	h.mu.Lock()
	h.init()
	if len(h.windows) == 0 {
		h.mu.Unlock()
		return nil, ErrNoBrowserWindow
	}
	win := h.windows[len(h.windows)-1]
	h.seq++
	r := &browserRelay{id: fmt.Sprintf("agent-%d-%d", time.Now().UnixNano()%1e9, h.seq), w: win, in: make(chan json.RawMessage, 1024), opened: make(chan error, 1), closed: make(chan struct{}), hub: h}
	h.relays[r.id] = r
	h.mu.Unlock()
	cmd := api.AgentBrowserCommand{BrowserID: r.id, Kind: "open", Show: o.Show, Profile: o.Profile, AllowLocal: o.AllowLocal, DenyDomains: o.DenyDomains}
	if err := r.push(ctx, cmd); err != nil {
		r.shut(err)
		return nil, err
	}
	wait, cancel := context.WithTimeout(ctx, agentBrowserOpenWait)
	defer cancel()
	select {
	case err := <-r.opened:
		if err != nil {
			r.shut(err)
			return nil, err
		}
		return r, nil
	case <-wait.Done():
		r.shut(errors.New("the desktop window didn't open the view"))
		return nil, fmt.Errorf("%w: the desktop window didn't open the view", ErrNoBrowserWindow)
	}
}

// push hands cmd to the window, unless the view or ctx ends first.
func (r *browserRelay) push(ctx context.Context, cmd api.AgentBrowserCommand) error {
	// Closed wins: a select of ready cases picks at random.
	select {
	case <-r.closed:
		return r.why
	default:
	}
	select {
	case r.w.send <- cmd:
		return nil
	case <-r.closed:
		return r.why
	case <-ctx.Done():
		return ctx.Err()
	}
}

// shut ends the browserRelay once: readers and writers get why.
func (r *browserRelay) shut(why error) {
	r.once.Do(func() {
		r.why = why
		close(r.closed)
		r.hub.mu.Lock()
		delete(r.hub.relays, r.id)
		r.hub.mu.Unlock()
	})
}

// WriteJSON sends a DevTools command to the view.
func (r *browserRelay) WriteJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return r.push(context.Background(), api.AgentBrowserCommand{BrowserID: r.id, Kind: "message", Message: string(data)})
}

// ReadJSON is the view's next message.
func (r *browserRelay) ReadJSON(v any) error {
	select {
	case m := <-r.in:
		return json.Unmarshal(m, v)
	case <-r.closed:
		return r.why
	}
}

// Close closes the view.
func (r *browserRelay) Close() error {
	select {
	case <-r.closed:
		return nil
	default:
	}
	// Best effort: a window gone has closed it already.
	select {
	case r.w.send <- api.AgentBrowserCommand{BrowserID: r.id, Kind: "close"}:
	default:
	}
	r.shut(errors.New("closed"))
	return nil
}
