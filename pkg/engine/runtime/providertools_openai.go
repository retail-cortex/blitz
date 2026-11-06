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

package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3/option"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// OpenAI's provider tools (spec_056 CA-80). ADK's Responses adapter sends
// function tools only and refuses output items other than messages,
// function calls and reasoning; so a call that offers the provider's tools
// carries them to the client's middleware in its context (openAISink),
// which adds them to the request, and takes their items and the answer's
// URL citations out of the reply before ADK reads it, for the settings
// wrapper to store (sink.apply).

type openAISinkKey struct{}

// openAISink is one call's provider tools: what it offers, and what the
// reply did with them.
type openAISink struct {
	kinds []string

	mu        sync.Mutex
	parts     []*genai.Part
	citations []Citation
	requests  map[string]int
}

// openAIProviderToolsMiddleware adds the provider tools a call offers to
// its request, and keeps what the reply did with them.
func openAIProviderToolsMiddleware(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
	sink, _ := req.Context().Value(openAISinkKey{}).(*openAISink)
	if sink == nil || req.Body == nil || !strings.HasSuffix(req.URL.Path, "/responses") {
		return next(req)
	}
	body, err := io.ReadAll(req.Body)
	req.Body.Close()
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("openai: provider tools: %w", err)
	}
	tools, _ := doc["tools"].([]any)
	for _, kind := range sink.kinds {
		if kind == ptWebSearch {
			tools = append(tools, map[string]any{"type": "web_search"})
		}
	}
	doc["tools"] = tools
	if body, err = json.Marshal(doc); err != nil {
		return nil, err
	}
	setBody(req, body)
	resp, err := next(req)
	if err != nil || resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		return resp, err // a stream (the gateway's) isn't offered these tools
	}
	out, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	out = sink.take(out)
	resp.Body = io.NopCloser(bytes.NewReader(out))
	resp.ContentLength = int64(len(out))
	resp.Header.Set("Content-Length", fmt.Sprint(len(out)))
	return resp, nil
}

func setBody(req *http.Request, body []byte) {
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Length", fmt.Sprint(len(body)))
}

// take returns a Responses API reply without its provider tool items,
// keeping them, and its messages' URL citations, in the sink. A reply it
// can't read goes on as it came.
func (s *openAISink) take(body []byte) []byte {
	var doc map[string]any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if dec.Decode(&doc) != nil {
		return body
	}
	items, _ := doc["output"].([]any)
	kept := items[:0:0]
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range items {
		item, _ := it.(map[string]any)
		parts, cites, ok := providerToolResult(item)
		s.citations = append(s.citations, cites...)
		if !ok {
			kept = append(kept, it)
			continue
		}
		s.parts = append(s.parts, parts...)
		if s.requests == nil {
			s.requests = map[string]int{}
		}
		s.requests[ptWebSearch]++
	}
	doc["output"] = kept
	out, err := json.Marshal(doc)
	if err != nil {
		return body
	}
	return out
}

// providerToolResult reads one Responses API output item: a provider tool
// call (web_search_call) as its stored call and result (ok); or a message,
// for its URL citations (not ok: the message goes on to ADK).
func providerToolResult(item map[string]any) (parts []*genai.Part, cites []Citation, ok bool) {
	switch item["type"] {
	case "web_search_call":
		action, _ := item["action"].(map[string]any)
		query, _ := action["query"].(string)
		var sources []string
		for _, s := range asList(action["sources"]) {
			src, _ := s.(map[string]any)
			if u, _ := src["url"].(string); u != "" {
				sources = append(sources, u)
			}
		}
		id, _ := item["id"].(string)
		if id == "" {
			id = uuid.NewString()
		}
		status, _ := item["status"].(string)
		result := strings.Join(sources, "\n")
		if result == "" {
			result = "Search " + status + "."
		}
		return []*genai.Part{
			providerCall(ptWebSearch, id, map[string]any{"query": query}, nil),
			providerResult(ptWebSearch, id, result, nil),
		}, nil, true
	case "message":
		for _, c := range asList(item["content"]) {
			content, _ := c.(map[string]any)
			text, _ := content["text"].(string)
			for _, a := range asList(content["annotations"]) {
				ann, _ := a.(map[string]any)
				if ann["type"] != "url_citation" {
					continue
				}
				url, _ := ann["url"].(string)
				title, _ := ann["title"].(string)
				cites = append(cites, Citation{Title: title, URL: url, Text: span(text, ann["start_index"], ann["end_index"])})
			}
		}
	}
	return nil, cites, false
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

// span is text[start:end] by a citation's indexes (JSON numbers), "" if
// they don't fit.
func span(text string, start, end any) string {
	a, b := index(start), index(end)
	r := []rune(text)
	if a < 0 || b <= a || b > len(r) {
		return ""
	}
	return string(r[a:b])
}

func index(v any) int {
	switch n := v.(type) {
	case json.Number:
		i, err := n.Int64()
		if err == nil {
			return int(i)
		}
	case float64:
		return int(n)
	}
	return -1
}

// apply stores what the reply's provider tools did in resp: their calls
// and results before its own parts, its citations, and the requests made.
func (s *openAISink) apply(resp *model.LLMResponse) {
	if s == nil || resp == nil || resp.Partial {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.parts) > 0 && resp.Content != nil {
		resp.Content.Parts = append(append([]*genai.Part(nil), s.parts...), resp.Content.Parts...)
	}
	addCitations(resp, s.citations)
	for kind, n := range s.requests {
		countRequests(resp, kind, n)
	}
	s.parts, s.citations, s.requests = nil, nil, nil
}
