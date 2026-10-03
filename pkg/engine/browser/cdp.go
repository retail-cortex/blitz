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

// Package browser drives a Chromium-family browser through the Chrome
// DevTools Protocol for the agent's browser tool (spec_parity_027 §5.2):
// its own throwaway profile, never the user's, with every request it makes
// going through a proxy that applies the web rules.
package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/gorilla/websocket"
)

// conn is a DevTools Protocol connection to one page: commands and their
// replies, and events to the handlers registered for them.
type conn struct {
	ws *websocket.Conn

	writeMu sync.Mutex
	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan reply
	on      map[string][]func(json.RawMessage)
	err     error // why the connection ended
	done    chan struct{}
}

type reply struct {
	Result json.RawMessage
	Err    error
}

// message is anything the browser sends: a reply (ID) or an event (Method).
type message struct {
	ID     int64           `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// errClosed means the browser went away.
var errClosed = errors.New("the browser closed")

func dial(ctx context.Context, wsURL string) (*conn, error) {
	d := websocket.Dialer{ReadBufferSize: 1 << 16, WriteBufferSize: 1 << 16}
	ws, _, err := d.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, err
	}
	ws.SetReadLimit(64 << 20) // screenshots
	c := &conn{ws: ws, pending: map[int64]chan reply{}, on: map[string][]func(json.RawMessage){}, done: make(chan struct{})}
	go c.read()
	return c, nil
}

func (c *conn) read() {
	var err error
	for {
		var m message
		if err = c.ws.ReadJSON(&m); err != nil {
			break
		}
		if m.ID != 0 {
			c.mu.Lock()
			ch := c.pending[m.ID]
			delete(c.pending, m.ID)
			c.mu.Unlock()
			if ch != nil {
				r := reply{Result: m.Result}
				if m.Error != nil {
					r.Err = fmt.Errorf("%s", m.Error.Message)
				}
				ch <- r
			}
			continue
		}
		c.mu.Lock()
		handlers := append([]func(json.RawMessage){}, c.on[m.Method]...)
		c.mu.Unlock()
		for _, h := range handlers {
			h(m.Params)
		}
	}
	c.mu.Lock()
	c.err = fmt.Errorf("%w: %v", errClosed, err)
	for id, ch := range c.pending {
		ch <- reply{Err: c.err}
		delete(c.pending, id)
	}
	c.mu.Unlock()
	close(c.done)
}

// call sends method with params and decodes its result into out (when
// not nil).
func (c *conn) call(ctx context.Context, method string, params, out any) error {
	// Cancelled already: nothing is sent, so a cancelled click or
	// navigation never reaches the browser.
	if err := ctx.Err(); err != nil {
		return err
	}
	ch := make(chan reply, 1)
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return c.err
	}
	c.nextID++
	id := c.nextID
	c.pending[id] = ch
	c.mu.Unlock()

	if params == nil {
		params = struct{}{}
	}
	c.writeMu.Lock()
	err := c.ws.WriteJSON(map[string]any{"id": id, "method": method, "params": params})
	c.writeMu.Unlock()
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return err
	}
	select {
	case r := <-ch:
		if r.Err != nil {
			return fmt.Errorf("%s: %w", method, r.Err)
		}
		if out != nil && len(r.Result) > 0 {
			return json.Unmarshal(r.Result, out)
		}
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return ctx.Err()
	}
}

// handle runs fn for each event named method, on the reading goroutine:
// fn mustn't call back into the connection and wait.
func (c *conn) handle(method string, fn func(json.RawMessage)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.on[method] = append(c.on[method], fn)
}

func (c *conn) close() error {
	err := c.ws.Close()
	<-c.done
	return err
}
