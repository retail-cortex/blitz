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

package tray

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/retail-cortex/blitz/pkg/pageserver"
)

// Link is what Serve writes for each link it makes: one JSON line.
type Link struct {
	URL string `json:"url"`
	// Fingerprint is the page server's certificate's (SHA-256, colon-
	// separated hex): the one certificate the window trusts for it.
	Fingerprint string `json:"fingerprint"`
}

// Serve serves the desktop page for a window that shows it (the Electron
// app, which runs blitz-tray --serve): it writes a one-time link to the
// page to out as a JSON line, a new one for each "link" line it reads
// from in, and stops serving when in ends, so it never outlives the
// window that started it. With relay, the host's pings go out through
// the app too: a "ping" line out, its "pinged" line back.
func Serve(o pageserver.Options, in io.Reader, out io.Writer, relay *Relay) error {
	srv, err := pageserver.Start(o)
	if err != nil {
		return err
	}
	defer srv.Close()
	w := &lineWriter{enc: json.NewEncoder(out)}
	if relay != nil { // before the link: the page may ping once it opens
		relay.attach(w)
		defer relay.detach()
	}
	if err := w.write(Link{URL: srv.OpenURL(), Fingerprint: srv.Fingerprint()}); err != nil {
		return err
	}
	lines := bufio.NewScanner(in)
	for lines.Scan() {
		line := strings.TrimSpace(lines.Text())
		switch {
		case line == "link":
			if err := w.write(Link{URL: srv.OpenURL(), Fingerprint: srv.Fingerprint()}); err != nil {
				return err
			}
		case relay != nil && strings.HasPrefix(line, "{"):
			relay.answer(line)
		}
	}
	return lines.Err()
}

// lineWriter writes Serve's JSON lines, one at a time.
type lineWriter struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func (w *lineWriter) write(v any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.enc.Encode(v)
}

// Relay sends the host's daily ping (spec_054_active_installs AI-14)
// through the desktop app, whose network is the system's: its proxy
// settings, PAC file and proxy sign-in, and the certificates the system
// trusts, which Go's HTTP client doesn't read. Its Send is an
// activity.Recorder's.
type Relay struct {
	mu      sync.Mutex
	w       *lineWriter
	next    int
	waiting map[string]chan relayed
}

// relayPing is a "ping" line: the app posts Body (JSON, with its proxy
// field set) to URL and answers with a "pinged" line for ID.
type relayPing struct {
	Ping struct {
		ID   string            `json:"id"`
		URL  string            `json:"url"`
		Body map[string]string `json:"body"`
	} `json:"ping"`
}

// relayed is a "pinged" line: the answer's HTTP status, or why there was
// none.
type relayed struct {
	ID     string `json:"pinged"`
	Status int    `json:"status"`
	Error  string `json:"error"`
	gone   bool   // the app went before it answered
}

// errNoApp is a ping with no app to send it.
var errNoApp = errors.New("the desktop app isn't connected")

func (r *Relay) attach(w *lineWriter) {
	r.mu.Lock()
	r.w = w
	r.mu.Unlock()
}

// detach ends the pings in flight: the app is gone.
func (r *Relay) detach() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.w = nil
	for id, ch := range r.waiting {
		ch <- relayed{ID: id, gone: true}
		delete(r.waiting, id)
	}
}

// answer hands a "pinged" line to the ping waiting for it.
func (r *Relay) answer(line string) {
	var a relayed
	if json.Unmarshal([]byte(line), &a) != nil || a.ID == "" {
		return
	}
	r.mu.Lock()
	ch := r.waiting[a.ID]
	delete(r.waiting, a.ID)
	r.mu.Unlock()
	if ch != nil {
		ch <- a
	}
}

// Send asks the app to post ping to url and waits for its answer, until
// ctx ends.
func (r *Relay) Send(ctx context.Context, url string, ping map[string]string) (int, error) {
	r.mu.Lock()
	if r.w == nil {
		r.mu.Unlock()
		return 0, errNoApp
	}
	r.next++
	id := strconv.Itoa(r.next)
	ch := make(chan relayed, 1)
	if r.waiting == nil {
		r.waiting = map[string]chan relayed{}
	}
	r.waiting[id] = ch
	w := r.w
	r.mu.Unlock()
	var line relayPing
	line.Ping.ID, line.Ping.URL, line.Ping.Body = id, url, ping
	if err := w.write(line); err != nil {
		r.forget(id)
		return 0, err
	}
	select {
	case a := <-ch:
		if a.gone {
			return 0, errNoApp
		}
		if a.Error != "" {
			return 0, errors.New(a.Error)
		}
		return a.Status, nil
	case <-ctx.Done():
		r.forget(id)
		return 0, ctx.Err()
	}
}

func (r *Relay) forget(id string) {
	r.mu.Lock()
	delete(r.waiting, id)
	r.mu.Unlock()
}

// PageDir is the built page in dir, checked to be one (it has index.html).
func PageDir(dir string) (fs.FS, error) {
	page := os.DirFS(dir)
	if _, err := fs.Stat(page, "index.html"); err != nil {
		return nil, fmt.Errorf("%s isn't the desktop page (no index.html): %w", dir, err)
	}
	return page, nil
}
