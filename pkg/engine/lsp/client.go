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

// Package lsp talks to language servers for the agent's code intelligence
// (spec_parity_027 PAR-TOOL-03): definitions, references, hover, symbols
// and diagnostics, from servers started lazily per language.
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

// Process is a started language server: its stdin and stdout, and how to
// stop it.
type Process interface {
	Stdin() io.WriteCloser
	Stdout() io.ReadCloser
	Stop()
}

// Launcher starts a server's command in the workspace (the engine's runs it
// guarded, in the OS sandbox).
type Launcher func(ctx context.Context, argv []string) (Process, error)

// conn is a JSON-RPC 2.0 connection over a server's stdio, with LSP's
// Content-Length framing.
type conn struct {
	proc Process

	writeMu sync.Mutex
	mu      sync.Mutex
	next    int64
	pending map[int64]chan response
	notify  func(method string, params json.RawMessage)
	closed  error
	done    chan struct{}
}

type response struct {
	Result json.RawMessage
	Err    error
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// errClosed: the server went away.
var errClosed = errors.New("the language server stopped")

func newConn(p Process, notify func(string, json.RawMessage)) *conn {
	c := &conn{proc: p, pending: map[int64]chan response{}, notify: notify, done: make(chan struct{})}
	go c.read()
	return c
}

func (c *conn) read() {
	r := bufio.NewReaderSize(c.proc.Stdout(), 1<<16)
	var err error
	for {
		var body []byte
		if body, err = readMessage(r); err != nil {
			break
		}
		var m rpcMessage
		if json.Unmarshal(body, &m) != nil {
			continue
		}
		switch {
		case m.Method != "" && len(m.ID) > 0: // a request from the server: answer it plainly
			c.reply(m.ID, m.Method)
		case m.Method != "":
			if c.notify != nil {
				c.notify(m.Method, m.Params)
			}
		case len(m.ID) > 0:
			id, _ := strconv.ParseInt(string(m.ID), 10, 64)
			c.mu.Lock()
			ch := c.pending[id]
			delete(c.pending, id)
			c.mu.Unlock()
			if ch != nil {
				res := response{Result: m.Result}
				if m.Error != nil {
					res.Err = fmt.Errorf("%s (%d)", m.Error.Message, m.Error.Code)
				}
				ch <- res
			}
		}
	}
	c.mu.Lock()
	c.closed = fmt.Errorf("%w: %v", errClosed, err)
	for id, ch := range c.pending {
		ch <- response{Err: c.closed}
		delete(c.pending, id)
	}
	c.mu.Unlock()
	close(c.done)
}

// reply answers requests servers make of their client: configuration
// (none), and the rest with null.
func (c *conn) reply(id json.RawMessage, method string) {
	var result any
	if method == "workspace/configuration" {
		result = []any{}
	}
	c.write(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func readMessage(r *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if v, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			if length, err = strconv.Atoi(strings.TrimSpace(v)); err != nil {
				return nil, fmt.Errorf("bad Content-Length %q", v)
			}
		}
	}
	if length < 0 || length > 64<<20 {
		return nil, fmt.Errorf("bad message length %d", length)
	}
	body := make([]byte, length)
	_, err := io.ReadFull(r, body)
	return body, err
}

func (c *conn) write(v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	w := c.proc.Stdin()
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

// call sends a request and decodes its result into out (nil: ignored).
func (c *conn) call(ctx context.Context, method string, params, out any) error {
	ch := make(chan response, 1)
	c.mu.Lock()
	if c.closed != nil {
		c.mu.Unlock()
		return c.closed
	}
	c.next++
	id := c.next
	c.pending[id] = ch
	c.mu.Unlock()
	if err := c.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
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
		if out != nil && len(r.Result) > 0 && string(r.Result) != "null" {
			return json.Unmarshal(r.Result, out)
		}
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		c.write(map[string]any{"jsonrpc": "2.0", "method": "$/cancelRequest", "params": map[string]any{"id": id}})
		return ctx.Err()
	}
}

// notify sends a notification.
func (c *conn) send(method string, params any) error {
	return c.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}
