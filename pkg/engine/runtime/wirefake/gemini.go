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
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Gemini fakes the Gemini API's generateContent and
// streamGenerateContent (POST /v1beta/models/<model>:…; the stream as
// server-sent events, the whole answer in one chunk). Point the genai
// client at it with GOOGLE_GEMINI_BASE_URL.
func Gemini(t testing.TB) *Fake {
	f := newFake(t, `^/v1beta/models/[^/:]+:(generate|streamGenerate)Content$`, checkGemini, answerGemini)
	f.streamed = func(r *http.Request, _ map[string]any) bool {
		return strings.HasSuffix(r.URL.Path, ":streamGenerateContent")
	}
	f.stream = func(w io.Writer, answer any) { event(w, "", answer) }
	return f
}

// checkGemini is why the Gemini API would refuse req ("" if it wouldn't),
// from its documentation for thinking models (Gemini 3 and later): a
// function response turn comes right after its function call turn, with as
// many responses as calls; a thought signature is one Gemini gave; and in
// the current turn (since the last user message with text), each step's
// first function call carries its thought signature. Tool names match
// ^[a-zA-Z0-9_-]{1,64}$.
func checkGemini(f *Fake, req map[string]any) string {
	for _, tl := range list(req["tools"]) {
		for i, d := range list(obj(tl)["functionDeclarations"]) {
			if n := str(obj(d)["name"]); !toolName.MatchString(n) {
				return fmt.Sprintf("Invalid function name %q at tools[0].function_declarations[%d]: must match [a-zA-Z0-9_-]{1,64}", n, i)
			}
		}
	}
	contents := list(req["contents"])
	if len(contents) == 0 {
		return "contents is not specified"
	}
	current := 0 // where the current turn starts
	for i, raw := range contents {
		c := obj(raw)
		if str(c["role"]) != "user" {
			continue
		}
		for _, p := range list(c["parts"]) {
			if str(obj(p)["text"]) != "" {
				current = i
			}
		}
	}
	calls := 0
	for i, raw := range contents {
		c := obj(raw)
		role := str(c["role"])
		if role != "user" && role != "model" {
			return fmt.Sprintf("Please use a valid role: user, model. (contents[%d].role %q)", i, role)
		}
		responses, firstCall := 0, true
		for j, rp := range list(c["parts"]) {
			p := obj(rp)
			if sig := str(p["thoughtSignature"]); sig != "" {
				raw, err := base64.StdEncoding.DecodeString(sig)
				if err != nil || !(f.signed(string(raw)) || skipSignatures[string(raw)]) {
					return fmt.Sprintf("Unable to submit request because Thought signature is not valid. (contents[%d].parts[%d])", i, j)
				}
			}
			if p["functionCall"] != nil {
				if role != "model" {
					return fmt.Sprintf("contents[%d].parts[%d]: a function call must be in a model turn", i, j)
				}
				if firstCall && i >= current && str(p["thoughtSignature"]) == "" {
					return fmt.Sprintf("Function call is missing a thought_signature in functionCall parts. This is required for tools to work correctly (function call `%s`, content %d).", str(obj(p["functionCall"])["name"]), i)
				}
				firstCall = false
			}
			if p["functionResponse"] != nil {
				responses++
			}
		}
		if responses > 0 {
			if calls == 0 {
				return "Please ensure that function response turn comes immediately after a function call turn."
			}
			if responses != calls {
				return "Please ensure that the number of function response parts is equal to the number of function call parts of the function call turn."
			}
		} else if calls > 0 {
			return "Please ensure that function response turn comes immediately after a function call turn."
		}
		calls = 0
		if role == "model" {
			for _, rp := range list(c["parts"]) {
				if obj(rp)["functionCall"] != nil {
					calls++
				}
			}
		}
	}
	return ""
}

// skipSignatures are the placeholders Google documents for function calls
// Gemini didn't make (another model's, or history written by hand), to use
// as a last resort since they cost the model some quality (Vertex AI's
// "Thought signatures" page, seen 2026-11-05).
var skipSignatures = map[string]bool{"skip_thought_signature_validator": true, "context_engineering_is_the_way_to_go": true}

func answerGemini(f *Fake, _ map[string]any, r Reply) any {
	// Gemini 3 thinks on every call and signs the first function call it
	// makes (or its text, when it makes none).
	var parts []any
	signed := false
	sign := func(p map[string]any) map[string]any {
		if !signed {
			p["thoughtSignature"] = base64.StdEncoding.EncodeToString([]byte(f.sign("gemini")))
			signed = true
		}
		return p
	}
	var calls []any
	for _, c := range r.Calls {
		calls = append(calls, sign(map[string]any{"functionCall": map[string]any{"name": c.Name, "args": orEmpty(c.Args)}}))
	}
	for _, file := range r.Files {
		parts = append(parts, sign(map[string]any{"inlineData": map[string]any{"mimeType": file.MIME, "data": base64.StdEncoding.EncodeToString(file.Data)}}))
	}
	if r.Text != "" {
		p := map[string]any{"text": r.Text}
		if len(calls) == 0 {
			p = sign(p)
		}
		parts = append(parts, p)
	}
	parts = append(parts, calls...)
	u := r.Usage.orDefault()
	return map[string]any{
		"candidates": []any{map[string]any{
			"content":      map[string]any{"role": "model", "parts": parts},
			"finishReason": "STOP", "index": 0,
		}},
		"usageMetadata": map[string]any{
			"promptTokenCount": u.Input, "cachedContentTokenCount": u.Cached,
			"candidatesTokenCount": u.Output - u.Reasoning, "thoughtsTokenCount": u.Reasoning,
			"totalTokenCount": u.Input + u.Output,
		},
		"modelVersion": "gemini-fake",
	}
}
