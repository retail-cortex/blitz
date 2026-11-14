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

package wirefake

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// LivePath is the Gemini API's Live socket, as genai dials it.
const LivePath = "/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent"

// Live fakes the Gemini API's Live socket (BidiGenerateContent) as strictly
// as the API (spec_063_voice VOX-60): the API key in x-goog-api-key, setup
// first (a model, audio answers, a prebuilt voice) and answered with
// setupComplete; then only realtime input (16 kHz PCM, activity start and
// end), client content and tool responses, each tool response answering a
// call the fake made. Anything else fails the test and closes the socket
// with 1007, as the API does. The test plays the server: Send pushes a
// message to the open connection, Next waits for the client's next
// message; GoAway and Drop end a connection, and a client resuming must
// name the last handle given (Resume).
type Live struct {
	// URL is the client's base URL (ws://host), for genai's HTTPOptions.
	URL string

	t      testing.TB
	srv    *httptest.Server
	key    string
	mu     sync.Mutex
	conn   *websocket.Conn
	setups []map[string]any
	calls  map[string]bool // tool calls awaiting their response, by id
	handle string          // the last resumption handle given
	in     chan map[string]any
	opened chan struct{}
}

// NewLive starts a fake Live API that wants key.
func NewLive(t testing.TB, key string) *Live {
	f := &Live{t: t, key: key, calls: map[string]bool{}, in: make(chan map[string]any, 256), opened: make(chan struct{}, 8)}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	f.URL = "ws" + strings.TrimPrefix(f.srv.URL, "http")
	t.Cleanup(f.Close)
	return f
}

// Close ends the connection and the server.
func (f *Live) Close() {
	f.mu.Lock()
	if f.conn != nil {
		_ = f.conn.Close()
	}
	f.mu.Unlock()
	f.srv.Close()
}

var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

func (f *Live) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != LivePath {
		f.t.Errorf("live: dialled %s, not %s", r.URL.Path, LivePath)
		http.NotFound(w, r)
		return
	}
	if got := r.Header.Get("x-goog-api-key"); got != f.key {
		f.t.Errorf("live: x-goog-api-key %q", got)
		http.Error(w, "API key not valid", http.StatusUnauthorized)
		return
	}
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		f.t.Errorf("live: upgrade: %v", err)
		return
	}
	defer c.Close()
	setup, ok := f.read(c)
	if !ok {
		return
	}
	if why := checkSetup(setup, f.lastHandle()); why != "" {
		f.refuse(c, why)
		return
	}
	f.mu.Lock()
	f.conn = c
	f.setups = append(f.setups, setup)
	f.mu.Unlock()
	if err := c.WriteJSON(map[string]any{"setupComplete": map[string]any{}}); err != nil {
		return
	}
	f.opened <- struct{}{}
	for {
		msg, ok := f.read(c)
		if !ok {
			return
		}
		if why := f.check(msg); why != "" {
			f.refuse(c, why)
			return
		}
		f.in <- msg
	}
}

func (f *Live) read(c *websocket.Conn) (map[string]any, bool) {
	_, data, err := c.ReadMessage()
	if err != nil {
		return nil, false
	}
	var msg map[string]any
	if err := json.Unmarshal(data, &msg); err != nil {
		f.refuse(c, "not JSON: "+err.Error())
		return nil, false
	}
	return msg, true
}

func (f *Live) refuse(c *websocket.Conn, why string) {
	f.t.Errorf("live: %s", why)
	_ = c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInvalidFramePayloadData, why))
}

func (f *Live) lastHandle() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.handle
}

// checkSetup is why setup isn't one the API takes, or "".
func checkSetup(msg map[string]any, handle string) string {
	s, ok := msg["setup"].(map[string]any)
	if !ok || len(msg) != 1 {
		return fmt.Sprintf("the first message must be setup alone, not %v", keys(msg))
	}
	if m, _ := s["model"].(string); !strings.HasPrefix(m, "models/") {
		return fmt.Sprintf("setup's model %q isn't models/<name>", m)
	}
	gc, _ := s["generationConfig"].(map[string]any)
	if mods, _ := gc["responseModalities"].([]any); len(mods) != 1 || mods[0] != "AUDIO" {
		return fmt.Sprintf("responseModalities %v: a native-audio model answers in AUDIO alone", gc["responseModalities"])
	}
	voice, _ := dig(gc, "speechConfig", "voiceConfig", "prebuiltVoiceConfig", "voiceName").(string)
	if voice == "" {
		return "no prebuilt voice in speechConfig"
	}
	if handle != "" {
		if got, _ := dig(s, "sessionResumption", "handle").(string); got != handle {
			return fmt.Sprintf("resumed with handle %q, not the last given, %q", got, handle)
		}
	}
	return ""
}

// check is why msg isn't one the API takes after setup, or "".
func (f *Live) check(msg map[string]any) string {
	if len(msg) != 1 {
		return fmt.Sprintf("one kind of message at a time, not %v", keys(msg))
	}
	switch {
	case msg["realtimeInput"] != nil:
		in, _ := msg["realtimeInput"].(map[string]any)
		for k, v := range in {
			switch k {
			case "audio", "mediaChunks":
				blobs := []any{v}
				if k == "mediaChunks" {
					blobs, _ = v.([]any)
				}
				for _, b := range blobs {
					blob, _ := b.(map[string]any)
					if mt, _ := blob["mimeType"].(string); mt != "audio/pcm;rate=16000" {
						return fmt.Sprintf("audio as %q, not audio/pcm;rate=16000", mt)
					}
					if _, err := base64.StdEncoding.DecodeString(fmt.Sprint(blob["data"])); err != nil {
						return "audio data isn't base64"
					}
				}
			case "activityStart", "activityEnd", "text", "audioStreamEnd":
			default:
				return fmt.Sprintf("realtimeInput has %q", k)
			}
		}
	case msg["clientContent"] != nil:
	case msg["toolResponse"] != nil:
		res, _ := dig(msg, "toolResponse", "functionResponses").([]any)
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, r := range res {
			id, _ := r.(map[string]any)["id"].(string)
			if !f.calls[id] {
				return fmt.Sprintf("a tool response to %q, which no call asked for", id)
			}
			delete(f.calls, id)
		}
	case msg["setup"] != nil:
		return "setup again on an open connection"
	default:
		return fmt.Sprintf("unknown message %v", keys(msg))
	}
	return ""
}

// Opened waits for a connection's setup (VOX-07's resumption opens
// another) and returns it.
func (f *Live) Opened(d time.Duration) map[string]any {
	f.t.Helper()
	select {
	case <-f.opened:
	case <-time.After(d):
		f.t.Fatalf("live: no connection in %v", d)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.setups[len(f.setups)-1]["setup"].(map[string]any)
}

// Next waits for the client's next message that has key (others are
// skipped: audio streams constantly), within d.
func (f *Live) Next(key string, d time.Duration) map[string]any {
	f.t.Helper()
	deadline := time.After(d)
	for {
		select {
		case msg := <-f.in:
			if v, ok := msg[key].(map[string]any); ok {
				return v
			}
		case <-deadline:
			f.t.Fatalf("live: no %s in %v", key, d)
			return nil
		}
	}
}

// Send pushes msg to the open connection, as the server sends it.
func (f *Live) Send(msg map[string]any) {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.conn == nil {
		f.t.Fatal("live: no connection to send on")
	}
	if c, ok := dig(msg, "toolCall", "functionCalls").([]any); ok {
		for _, call := range c {
			f.calls[call.(map[string]any)["id"].(string)] = true
		}
	}
	if h, ok := dig(msg, "sessionResumptionUpdate", "newHandle").(string); ok {
		f.handle = h
	}
	if err := f.conn.WriteJSON(msg); err != nil {
		f.t.Errorf("live: send: %v", err)
	}
}

// Say is the server speaking: the transcript of what it says and its
// audio (24 kHz PCM), then the turn's end.
func (f *Live) Say(text string, pcm []byte) {
	f.t.Helper()
	f.Send(map[string]any{"serverContent": map[string]any{"outputTranscription": map[string]any{"text": text}}})
	f.Send(map[string]any{"serverContent": map[string]any{"modelTurn": map[string]any{"parts": []any{
		map[string]any{"inlineData": map[string]any{"mimeType": "audio/pcm;rate=24000", "data": base64.StdEncoding.EncodeToString(pcm)}},
	}}}})
	f.Send(map[string]any{"serverContent": map[string]any{"turnComplete": true}})
}

// Heard is the server's transcript of what the person said.
func (f *Live) Heard(text string) {
	f.t.Helper()
	f.Send(map[string]any{"serverContent": map[string]any{"inputTranscription": map[string]any{"text": text}}})
}

// Call is the model calling a tool, and the response the client sends
// back, within d.
func (f *Live) Call(id, name string, args map[string]any, d time.Duration) map[string]any {
	f.t.Helper()
	f.Send(map[string]any{"toolCall": map[string]any{"functionCalls": []any{map[string]any{"id": id, "name": name, "args": args}}}})
	res := f.Next("toolResponse", d)
	list, _ := res["functionResponses"].([]any)
	for _, r := range list {
		if m := r.(map[string]any); m["id"] == id {
			out, _ := m["response"].(map[string]any)
			return out
		}
	}
	f.t.Fatalf("live: no response to %s in %v", id, res)
	return nil
}

// Resume gives the client a handle to resume with.
func (f *Live) Resume(handle string) {
	f.Send(map[string]any{"sessionResumptionUpdate": map[string]any{"newHandle": handle, "resumable": true}})
}

// GoAway warns the client the connection ends soon, then ends it, as the
// API does at its session limit.
func (f *Live) GoAway() {
	f.Send(map[string]any{"goAway": map[string]any{"timeLeft": "1s"}})
	f.Drop()
}

// Drop ends the connection with no warning (a network that went away).
func (f *Live) Drop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.conn != nil {
		_ = f.conn.Close()
		f.conn = nil
	}
}

// Setups are the setups received, one a connection.
func (f *Live) Setups() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]map[string]any, len(f.setups))
	for i, s := range f.setups {
		out[i] = s["setup"].(map[string]any)
	}
	return out
}

func dig(m map[string]any, path ...string) any {
	var v any = m
	for _, p := range path {
		mm, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = mm[p]
	}
	return v
}

func keys(m map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
