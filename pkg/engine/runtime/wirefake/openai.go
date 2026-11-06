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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
)

// OpenAI fakes the Responses API (POST <base>/responses), streamed when
// asked ("stream": true) as the API streams: the response created, each
// output item added and done, then the response completed.
func OpenAI(t testing.TB) *Fake {
	f := newFake(t, `/responses$`, checkResponses, answerResponses)
	f.streamed = func(_ *http.Request, req map[string]any) bool { return req["stream"] == true }
	f.stream = streamResponses
	return f
}

func streamResponses(w io.Writer, answer any) {
	resp := answer.(map[string]any)
	seq := 0
	send := func(typ string, data map[string]any) {
		data["type"], data["sequence_number"] = typ, seq
		seq++
		event(w, typ, data)
	}
	created := map[string]any{}
	for k, v := range resp {
		created[k] = v
	}
	created["status"], created["output"] = "in_progress", []any{}
	send("response.created", map[string]any{"response": created})
	for i, it := range list(resp["output"]) {
		item := obj(it)
		send("response.output_item.added", map[string]any{"output_index": i, "item": item})
		if str(item["type"]) == "message" {
			for j, c := range list(item["content"]) {
				send("response.output_text.delta", map[string]any{"output_index": i, "content_index": j, "item_id": item["id"], "delta": obj(c)["text"]})
				send("response.output_text.done", map[string]any{"output_index": i, "content_index": j, "item_id": item["id"], "text": obj(c)["text"]})
			}
		}
		send("response.output_item.done", map[string]any{"output_index": i, "item": item})
	}
	send("response.completed", map[string]any{"response": resp})
}

// checkResponses is why the Responses API would refuse req ("" if it
// wouldn't), from its documentation: input items are messages, function
// calls, their outputs and reasoning; a function_call_output answers a
// function_call earlier in the input by its call_id, and every
// function_call is answered; an assistant message holds output_text or a
// refusal, not input parts; tool names match ^[a-zA-Z0-9_-]{1,64}$.
func checkResponses(f *Fake, req map[string]any) string {
	for i, tl := range list(req["tools"]) {
		if n := str(obj(tl)["name"]); str(obj(tl)["type"]) == "function" && !toolName.MatchString(n) {
			return fmt.Sprintf("Invalid 'tools[%d].name': string does not match pattern. Expected a string that matches the pattern '^[a-zA-Z0-9_-]+$'.", i)
		}
	}
	input := list(req["input"])
	if s, ok := req["input"].(string); ok && s != "" {
		return ""
	}
	calls, answered := map[string]bool{}, map[string]bool{}
	for i, raw := range input {
		it := obj(raw)
		typ := str(it["type"])
		if typ == "" && it["role"] != nil {
			typ = "message"
		}
		switch typ {
		case "message":
			role := str(it["role"])
			for j, c := range list(it["content"]) {
				ct := str(obj(c)["type"])
				if role == "assistant" && ct != "output_text" && ct != "refusal" {
					return fmt.Sprintf("Invalid value: '%s'. Supported values are: 'output_text' and 'refusal'. (input[%d].content[%d])", ct, i, j)
				}
				if role != "assistant" && (ct == "output_text" || ct == "refusal") {
					return fmt.Sprintf("Invalid value: '%s'. Supported values are: 'input_text', 'input_image', and 'input_file'. (input[%d].content[%d])", ct, i, j)
				}
			}
		case "function_call":
			id := str(it["call_id"])
			if id == "" {
				return fmt.Sprintf("Missing required parameter: 'input[%d].call_id'.", i)
			}
			calls[id] = true
		case "function_call_output":
			id := str(it["call_id"])
			if !calls[id] {
				return fmt.Sprintf("No tool call found for function call output with call_id %s.", id)
			}
			answered[id] = true
		case "reasoning":
			if enc := str(it["encrypted_content"]); enc != "" && !f.signed(enc) {
				return fmt.Sprintf("The encrypted content for item input[%d] could not be verified.", i)
			}
		default:
			return fmt.Sprintf("Invalid value: '%s'. (input[%d].type)", typ, i)
		}
	}
	for id := range calls {
		if !answered[id] {
			return fmt.Sprintf("No tool output found for function call %s.", id)
		}
	}
	return ""
}

func answerResponses(f *Fake, req map[string]any, r Reply) any {
	var out []any
	if r.Thought != "" && obj(req["reasoning"]) != nil {
		out = append(out, map[string]any{"type": "reasoning", "id": f.id("rs_"), "summary": []any{map[string]any{"type": "summary_text", "text": r.Thought}}})
	}
	if r.Text != "" {
		out = append(out, map[string]any{
			"type": "message", "id": f.id("msg_"), "role": "assistant", "status": "completed",
			"content": []any{map[string]any{"type": "output_text", "text": r.Text, "annotations": []any{}}},
		})
	}
	for _, c := range r.Calls {
		args, _ := json.Marshal(orEmpty(c.Args))
		out = append(out, map[string]any{"type": "function_call", "id": f.id("fc_"), "call_id": f.id("call_"), "name": c.Name, "arguments": string(args), "status": "completed"})
	}
	u := r.Usage.orDefault()
	return map[string]any{
		"id": f.id("resp_"), "object": "response", "created_at": 0, "status": "completed", "model": str(req["model"]),
		"output": out,
		"usage": map[string]any{
			"input_tokens": u.Input, "input_tokens_details": map[string]any{"cached_tokens": u.Cached},
			"output_tokens": u.Output, "output_tokens_details": map[string]any{"reasoning_tokens": u.Reasoning},
			"total_tokens": u.Input + u.Output,
		},
	}
}
