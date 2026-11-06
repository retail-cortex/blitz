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

// Package wirefake has fakes of the model APIs Blitz speaks (Anthropic's
// Messages API, the Gemini API and OpenAI's Responses API) that are as
// strict as the real ones (spec_056_conversation_adapters CA-150): each
// refuses, with the real API's status and words, what its documentation
// says it refuses. They answer from a script, sign their own reasoning as
// the real APIs do, and refuse a signature they didn't give, so a test
// that switches a conversation between them shows what a real switch
// would break.
package wirefake

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"
)

// Reply is one scripted answer, given in each API's own form.
type Reply struct {
	// Thought is the model's reasoning, returned (and signed) only when
	// the request asks for reasoning, as the real APIs do.
	Thought string
	// Text is the answer's text.
	Text string
	// Calls are the tool calls the answer makes.
	Calls []Call
	// Files are files the answer makes (an image model's pictures), which
	// only the Gemini fake returns, as Gemini does: inline data in the
	// model's turn.
	Files []File
	// Usage is what the answer reports; zero gives a small default.
	Usage Usage
	// Status, when set, refuses the request with this HTTP status (a 503:
	// the model is overloaded) instead of answering.
	Status int
	// Search is a web search the model runs with its provider's own tool,
	// before its Text, which cites the results; only when the request
	// offers that tool, as a real model can search only then.
	Search *Search
	// Finish, when set, is why the answer ended, in the API's own words:
	// Anthropic's stop_reason ("refusal"), Gemini's finishReason ("SAFETY",
	// "RECITATION", "MALFORMED_FUNCTION_CALL"), OpenAI's incomplete reason
	// ("content_filter").
	Finish string
	// Extra are parts of the answer Blitz has no form for, as the API
	// sends them: an Anthropic content block (a server_tool_use), an
	// OpenAI output item (a web_search_call).
	Extra []map[string]any
	// TooLong refuses the request as longer than the model's context
	// window, in the API's words.
	TooLong bool
	// Refusal is the refusal's message, in the API's error shape (its
	// status's text when empty): a model refusing what it can't do, in
	// the words its server uses ("… does not support tools").
	Refusal string
}

// Search is a provider web search: its query and what it found.
type Search struct {
	Query   string
	Results []Source
}

// Source is one search result.
type Source struct {
	Title, URL string
}

// File is a file a model makes.
type File struct {
	MIME string
	Data []byte
}

// tooLong is each API's refusal of a request longer than its model reads.
func tooLong(path string) string {
	switch {
	case regexp.MustCompile(`/messages$`).MatchString(path):
		return "prompt is too long: 212000 tokens > 200000 maximum"
	case regexp.MustCompile(`:(generate|streamGenerate)Content$`).MatchString(path):
		return "The input token count (1100000) exceeds the maximum number of tokens allowed (1048576)."
	default:
		return "Your input exceeds the context window of this model. Please adjust your input and try again."
	}
}

// Call is one scripted tool call.
type Call struct {
	Name string
	Args map[string]any
}

// Usage is a reply's token counts, in the neutral sense: Output includes
// Reasoning (each fake reports them as its API does).
type Usage struct {
	Input, Cached, Output, Reasoning int
}

func (u Usage) orDefault() Usage {
	if u == (Usage{}) {
		return Usage{Input: 100, Output: 20}
	}
	return u
}

// Fake is one API's fake: a server, its script, and what it was sent.
type Fake struct {
	t      testing.TB
	srv    *httptest.Server
	mu     sync.Mutex
	script []Reply
	reqs   []map[string]any
	refuse []string
	issued map[string]bool
	n      int
	// streamed counts the answers given as streams.
	streamed int
	check    func(f *Fake, req map[string]any) string
	answer   func(f *Fake, req map[string]any, r Reply) any
	// streamed reports whether a request asks for a stream, and stream
	// writes an answer as the API streams it (server-sent events).
	isStream func(r *http.Request, req map[string]any) bool
	stream   func(w io.Writer, answer any)
	path     *regexp.Regexp
}

func newFake(t testing.TB, path string, check func(*Fake, map[string]any) string, answer func(*Fake, map[string]any, Reply) any) *Fake {
	f := &Fake{t: t, issued: map[string]bool{}, check: check, answer: answer, path: regexp.MustCompile(path)}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

// URL is the fake's address.
func (f *Fake) URL() string { return f.srv.URL }

// Script queues answers, given in order to the requests the fake accepts.
func (f *Fake) Script(rs ...Reply) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.script = append(f.script, rs...)
}

// Remaining is how many scripted answers haven't been given yet.
func (f *Fake) Remaining() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.script)
}

// Streamed is how many answers the fake gave as streams.
func (f *Fake) Streamed() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.streamed
}

// Requests are the bodies the fake was sent, accepted or not.
func (f *Fake) Requests() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.reqs...)
}

// Refusals are why the fake refused requests, in order, in the API's words.
func (f *Fake) Refusals() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.refuse...)
}

// sign gives out a signature the fake will accept back.
func (f *Fake) sign(kind string) string {
	f.n++
	s := fmt.Sprintf("%s-signature-%d", kind, f.n)
	f.issued[s] = true
	return s
}

func (f *Fake) signed(s string) bool { return f.issued[s] }

func (f *Fake) id(prefix string) string {
	f.n++
	return fmt.Sprintf("%s%d", prefix, f.n)
}

func (f *Fake) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !f.path.MatchString(r.URL.Path) {
		http.Error(w, `{"error":{"message":"wirefake: no such endpoint `+r.Method+" "+r.URL.Path+`"}}`, http.StatusNotFound)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, `{"error":{"message":"wirefake: body is not JSON"}}`, http.StatusBadRequest)
		return
	}
	if m := pathModel.FindStringSubmatch(r.URL.Path); m != nil {
		req[pathModelKey] = m[1] // Gemini names the model in the path
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, req)
	if why := f.check(f, req); why != "" {
		f.refuse = append(f.refuse, why)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(errorBody(r.URL.Path, why))
		return
	}
	if len(f.script) == 0 {
		f.t.Errorf("wirefake %s: a request came with nothing left in the script", r.URL.Path)
		http.Error(w, `{"error":{"message":"wirefake: script exhausted"}}`, http.StatusInternalServerError)
		return
	}
	reply := f.script[0]
	f.script = f.script[1:]
	if reply.TooLong {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		body := errorBody(r.URL.Path, tooLong(r.URL.Path))
		if e := obj(obj(body)["error"]); e != nil && !regexp.MustCompile(`(/messages|Content)$`).MatchString(r.URL.Path) {
			e["code"] = "context_length_exceeded"
		}
		_ = json.NewEncoder(w).Encode(body)
		return
	}
	if reply.Status != 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(reply.Status)
		msg := reply.Refusal
		if msg == "" {
			msg = http.StatusText(reply.Status)
		}
		_ = json.NewEncoder(w).Encode(errorBody(r.URL.Path, msg))
		return
	}
	answer := f.answer(f, req, reply)
	if f.isStream != nil && f.isStream(r, req) {
		f.streamed++
		w.Header().Set("Content-Type", "text/event-stream")
		f.stream(w, answer)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(answer)
}

// event writes one server-sent event (name may be empty).
func event(w io.Writer, name string, data any) {
	b, _ := json.Marshal(data)
	if name != "" {
		fmt.Fprintf(w, "event: %s\n", name)
	}
	fmt.Fprintf(w, "data: %s\n\n", b)
}

// errorBody is each API's error shape.
func errorBody(path, why string) any {
	switch {
	case regexp.MustCompile(`/messages$`).MatchString(path):
		return map[string]any{"type": "error", "error": map[string]any{"type": "invalid_request_error", "message": why}}
	case regexp.MustCompile(`:(generate|streamGenerate)Content$`).MatchString(path):
		return map[string]any{"error": map[string]any{"code": 400, "message": why, "status": "INVALID_ARGUMENT"}}
	default:
		return map[string]any{"error": map[string]any{"message": why, "type": "invalid_request_error", "param": nil, "code": nil}}
	}
}

var pathModel = regexp.MustCompile(`/models/([^/:]+):`)

// pathModelKey holds, in a recorded request, the model its path named.
const pathModelKey = "wirefake_path_model"

// ModelOf is the model a recorded request asked for: in its body
// (Anthropic, OpenAI) or its path (Gemini).
func ModelOf(req map[string]any) string {
	if m := str(req[pathModelKey]); m != "" {
		return m
	}
	return str(req["model"])
}

// Helpers for reading a JSON request.

func list(v any) []any {
	l, _ := v.([]any)
	return l
}

func obj(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// toolName is the pattern the three APIs agree on for a tool's name.
var toolName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
