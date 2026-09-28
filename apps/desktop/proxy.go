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

package main

import (
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/socket"
)

// apiPrefix is where the service's API lives; the page's requests there go
// to the service.
const apiPrefix = "/blitz.v1."

// serviceProxy forwards the page's API requests to the service's Unix
// socket (a web view can't open one itself), streaming responses as they
// come. Anything else is not found: the page's own files are served by
// Wails.
func serviceProxy(path string) http.Handler {
	target, _ := url.Parse(socket.BaseURL)
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.Host = target.Host
		},
		Transport:     socket.Client(path).Transport,
		FlushInterval: -1, // turns stream their events
		ModifyResponse: func(res *http.Response) error {
			if empty, ok := emptyMessage(res.Header.Get("Content-Type")); ok {
				res.Body = newKeepalive(res.Body, keepaliveInterval, empty)
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			// Connect's JSON error shape, so the page's client reads it.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"code":"unavailable","message":"the Blitz service isn't answering"}`))
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, apiPrefix) {
			http.NotFound(w, r)
			return
		}
		proxy.ServeHTTP(w, r)
	})
}

// keepaliveInterval is how long a stream may be quiet before the proxy
// writes an empty message into it.
const keepaliveInterval = 250 * time.Millisecond

// emptyMessage is an empty Connect stream message for a response's content
// type, if it's a Connect stream: {} in JSON, no bytes in protobuf.
func emptyMessage(contentType string) ([]byte, bool) {
	var payload []byte
	switch {
	case strings.HasPrefix(contentType, "application/connect+json"):
		payload = []byte("{}")
	case strings.HasPrefix(contentType, "application/connect+proto"):
	default:
		return nil, false
	}
	frame := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(payload)))
	copy(frame[5:], payload)
	return frame, true
}

// keepalive is a Connect stream's body that passes the service's messages
// through whole and adds an empty one whenever the stream has been quiet
// for interval. WebKitGTK, the Linux web view, sometimes holds back the
// last data written before a pause until more arrives: an approval
// request, after which the service waits for the answer, could stay
// unseen and the turn stuck at "Working". The page skips empty messages.
type keepalive struct {
	src      io.ReadCloser
	messages chan []byte // whole messages read from src
	err      error       // src's error, once messages is closed
	empty    []byte
	interval time.Duration
	pending  []byte
	ended    bool // the end-of-stream message has passed: nothing may follow
}

func newKeepalive(src io.ReadCloser, interval time.Duration, empty []byte) *keepalive {
	k := &keepalive{src: src, messages: make(chan []byte), empty: empty, interval: interval}
	go k.readMessages()
	return k
}

// readMessages reads src a whole message at a time: a flags byte, a
// 4-byte length and the payload.
func (k *keepalive) readMessages() {
	defer close(k.messages)
	for {
		head := make([]byte, 5)
		if _, err := io.ReadFull(k.src, head); err != nil {
			k.err = err
			return
		}
		msg := make([]byte, 5+int(binary.BigEndian.Uint32(head[1:5])))
		copy(msg, head)
		if _, err := io.ReadFull(k.src, msg[5:]); err != nil {
			k.err = err
			return
		}
		k.messages <- msg
	}
}

func (k *keepalive) Read(p []byte) (int, error) {
	if len(k.pending) == 0 {
		var quiet <-chan time.Time
		if !k.ended {
			t := time.NewTimer(k.interval)
			defer t.Stop()
			quiet = t.C
		}
		select {
		case msg, ok := <-k.messages:
			if !ok {
				return 0, k.err // io.EOF at the stream's end
			}
			k.ended = msg[0]&2 != 0 // Connect's end-of-stream flag
			k.pending = msg
		case <-quiet:
			k.pending = k.empty
		}
	}
	n := copy(p, k.pending)
	k.pending = k.pending[n:]
	return n, nil
}

func (k *keepalive) Close() error { return k.src.Close() }
