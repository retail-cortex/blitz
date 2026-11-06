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
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Anthropic fakes the Messages API (POST /v1/messages), streamed when
// asked ("stream": true) as the API streams: each block started, its
// deltas, stopped, then the message's stop reason and usage.
func Anthropic(t testing.TB) *Fake {
	f := newFake(t, `/v1/messages$`, checkMessages, answerMessages)
	f.streamed = func(_ *http.Request, req map[string]any) bool { return req["stream"] == true }
	f.stream = streamMessages
	return f
}

func streamMessages(w io.Writer, answer any) {
	msg := answer.(map[string]any)
	start := map[string]any{}
	for k, v := range msg {
		start[k] = v
	}
	start["content"], start["stop_reason"] = []any{}, nil
	event(w, "message_start", map[string]any{"type": "message_start", "message": start})
	for i, b := range list(msg["content"]) {
		block := obj(b)
		var empty map[string]any
		var delta map[string]any
		switch str(block["type"]) {
		case "thinking":
			empty = map[string]any{"type": "thinking", "thinking": ""}
			delta = map[string]any{"type": "thinking_delta", "thinking": block["thinking"]}
		case "text":
			empty = map[string]any{"type": "text", "text": ""}
			delta = map[string]any{"type": "text_delta", "text": block["text"]}
		case "tool_use":
			empty = map[string]any{"type": "tool_use", "id": block["id"], "name": block["name"], "input": map[string]any{}}
			in, _ := json.Marshal(block["input"])
			delta = map[string]any{"type": "input_json_delta", "partial_json": string(in)}
		}
		event(w, "content_block_start", map[string]any{"type": "content_block_start", "index": i, "content_block": empty})
		event(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": delta})
		if str(block["type"]) == "thinking" {
			event(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "signature_delta", "signature": block["signature"]}})
		}
		event(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": i})
	}
	event(w, "message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": msg["stop_reason"], "stop_sequence": nil}, "usage": msg["usage"]})
	event(w, "message_stop", map[string]any{"type": "message_stop"})
}

var toolUseID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// checkMessages is why the Messages API would refuse req ("" if it
// wouldn't), from its documentation: the conversation starts with the
// user; a tool_use's ID matches ^[a-zA-Z0-9_-]+$; each tool_use is
// answered by a tool_result in the next message, which comes first in it,
// and each tool_result answers a tool_use in the message before; a
// thinking block carries a signature Anthropic gave; with thinking on, the
// assistant message a tool loop continues from starts with its thinking;
// images and documents are the user's; tool names match
// ^[a-zA-Z0-9_-]{1,64}$. Consecutive messages of one role are combined, as
// the API combines them.
func checkMessages(f *Fake, req map[string]any) string {
	if str(req["model"]) == "" {
		return "model: Field required"
	}
	for i, tl := range list(req["tools"]) {
		if n := str(obj(tl)["name"]); !toolName.MatchString(n) {
			return fmt.Sprintf("tools.%d.custom.name: String should match pattern '^[a-zA-Z0-9_-]{1,64}$'", i)
		}
	}
	msgs := list(req["messages"])
	if len(msgs) == 0 {
		return "messages: at least one message is required"
	}
	if str(obj(msgs[0])["role"]) != "user" {
		return "messages.0.role: the first message must use the \"user\" role"
	}
	thinkingOn := thinks(req)
	var pending []string // tool_use IDs the next message must answer
	lastAssistant := -1
	for i, raw := range msgs {
		m := obj(raw)
		role := str(m["role"])
		blocks := list(m["content"])
		if s, ok := m["content"].(string); ok {
			blocks = []any{map[string]any{"type": "text", "text": s}}
		}
		answered := map[string]bool{}
		seenOther := false
		var uses []string
		for j, b := range blocks {
			block := obj(b)
			switch typ := str(block["type"]); typ {
			case "tool_use":
				if role != "assistant" {
					return fmt.Sprintf("messages.%d.content.%d: tool_use blocks may only be in assistant messages", i, j)
				}
				id := str(block["id"])
				if !toolUseID.MatchString(id) {
					return fmt.Sprintf("messages.%d.content.%d.tool_use.id: String should match pattern '^[a-zA-Z0-9_-]+$'", i, j)
				}
				uses = append(uses, id)
			case "tool_result":
				if role != "user" {
					return fmt.Sprintf("messages.%d.content.%d: tool_result blocks may only be in user messages", i, j)
				}
				if seenOther {
					return fmt.Sprintf("messages.%d: tool_result blocks must come first in the message content, before any other blocks", i)
				}
				id := str(block["tool_use_id"])
				if !slices.Contains(pending, id) {
					return fmt.Sprintf("messages.%d.content.%d: unexpected `tool_use_id` found in `tool_result` blocks: %s. Each `tool_result` block must have a corresponding `tool_use` block in the previous message.", i, j, id)
				}
				answered[id] = true
			case "thinking", "redacted_thinking":
				if role != "assistant" {
					return fmt.Sprintf("messages.%d.content.%d: %s blocks may only be in assistant messages", i, j, typ)
				}
				sig := str(block["signature"])
				if typ == "redacted_thinking" {
					sig = str(block["data"])
				}
				if !f.signed(sig) {
					return fmt.Sprintf("messages.%d.content.%d: Invalid `signature` in `%s` block", i, j, typ)
				}
				seenOther = true
			case "image", "document":
				if role != "user" {
					return fmt.Sprintf("messages.%d.content.%d: %s blocks may only be in user messages", i, j, typ)
				}
				seenOther = true
			case "text":
				seenOther = true
			default:
				return fmt.Sprintf("messages.%d.content.%d.type: Input tag '%s' found using 'type' does not match any of the expected tags", i, j, typ)
			}
		}
		if role == "user" && len(pending) > 0 {
			var missing []string
			for _, id := range pending {
				if !answered[id] {
					missing = append(missing, id)
				}
			}
			if len(missing) > 0 {
				return fmt.Sprintf("messages.%d: `tool_use` ids were found without `tool_result` blocks immediately after: %v. Each `tool_use` block must have a corresponding `tool_result` block in the next message.", i-1, missing)
			}
			pending = nil
		}
		if role == "assistant" {
			pending = append(pending, uses...)
			lastAssistant = i
		}
	}
	if len(pending) > 0 {
		return fmt.Sprintf("messages: `tool_use` ids were found without `tool_result` blocks immediately after: %v", pending)
	}
	// With thinking on, a tool loop goes on from an assistant message that
	// starts with its thinking.
	if thinkingOn && lastAssistant >= 0 && lastAssistant == len(msgs)-2 {
		blocks := list(obj(msgs[lastAssistant])["content"])
		hasUse := false
		for _, b := range blocks {
			hasUse = hasUse || str(obj(b)["type"]) == "tool_use"
		}
		if first := str(obj(firstOf(blocks))["type"]); hasUse && first != "thinking" && first != "redacted_thinking" {
			return fmt.Sprintf("messages.%d.content.0.type: Expected `thinking` or `redacted_thinking`, but found `%s`. When `thinking` is enabled, a final `assistant` message must start with a thinking block (preceeding the lastmost set of `tool_use` and `tool_result` blocks).", lastAssistant, first)
		}
	}
	return ""
}

// thinks reports whether a request has thinking on: asked for, or a model
// that always thinks (Opus 5.5, Fable), which thinks adaptively unasked.
func thinks(req map[string]any) bool {
	switch str(obj(req["thinking"])["type"]) {
	case "enabled", "adaptive":
		return true
	case "disabled":
		return false
	}
	m := str(req["model"])
	return strings.HasPrefix(m, "claude-opus-5-5") || strings.HasPrefix(m, "claude-fable")
}

func answerMessages(f *Fake, req map[string]any, r Reply) any {
	var content []any
	if r.Thought != "" && thinks(req) {
		content = append(content, map[string]any{"type": "thinking", "thinking": r.Thought, "signature": f.sign("anthropic")})
	}
	if r.Text != "" {
		content = append(content, map[string]any{"type": "text", "text": r.Text})
	}
	stop := "end_turn"
	for _, c := range r.Calls {
		content = append(content, map[string]any{"type": "tool_use", "id": f.id("toolu_"), "name": c.Name, "input": orEmpty(c.Args)})
		stop = "tool_use"
	}
	u := r.Usage.orDefault()
	return map[string]any{
		"id": f.id("msg_"), "type": "message", "role": "assistant", "model": str(req["model"]),
		"content": content, "stop_reason": stop, "stop_sequence": nil,
		"usage": map[string]any{"input_tokens": u.Input - u.Cached, "cache_read_input_tokens": u.Cached, "cache_creation_input_tokens": 0, "output_tokens": u.Output},
	}
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func firstOf(l []any) any {
	if len(l) == 0 {
		return nil
	}
	return l[0]
}
