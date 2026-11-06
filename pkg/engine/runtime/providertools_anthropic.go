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
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"google.golang.org/genai"
)

// Claude's server tools in a reply (spec_056 CA-80, CA-82): each
// server_tool_use and its result block stored as a provider tool's call
// and result, the block itself kept (as JSON) for Claude to get back.

// anthropicServerToolUse is a server_tool_use block as a stored call.
func anthropicServerToolUse(b anthropic.BetaServerToolUseBlock) *genai.Part {
	args := map[string]any{}
	if raw := b.JSON.Input.Raw(); raw != "" {
		_ = json.Unmarshal([]byte(raw), &args)
	}
	return providerCall(serverToolKind(string(b.Name)), b.ID, args, []byte(b.RawJSON()))
}

// serverToolKind is the purpose of one of Claude's server tools.
func serverToolKind(name string) string {
	switch {
	case strings.Contains(name, "web_search"):
		return ptWebSearch
	case strings.Contains(name, "web_fetch"):
		return ptWebFetch
	}
	return ptCodeExecution
}

// anthropicServerToolResult is a server tool's result block (its type
// ends in _tool_result) as a stored result, the result as text; nil for
// any other block.
func anthropicServerToolResult(typ, raw string) *genai.Part {
	if !strings.HasSuffix(typ, "_tool_result") || raw == "" {
		return nil
	}
	var block struct {
		ToolUseID string          `json:"tool_use_id"`
		Content   json.RawMessage `json:"content"`
	}
	if json.Unmarshal([]byte(raw), &block) != nil {
		return nil
	}
	return providerResult(serverToolKind(typ), block.ToolUseID, serverToolText(block.Content), []byte(raw))
}

// serverToolText is a server tool result's content as text another model
// can read: a search's results (title, URL), a fetched page's address and
// text, code's output, or an error's code.
func serverToolText(content json.RawMessage) string {
	var items []map[string]any
	if json.Unmarshal(content, &items) == nil {
		var lines []string
		for _, it := range items {
			title, _ := it["title"].(string)
			url, _ := it["url"].(string)
			lines = append(lines, strings.TrimSpace(title+" — "+url))
		}
		return strings.Join(lines, "\n")
	}
	var one map[string]any
	if json.Unmarshal(content, &one) != nil {
		return string(content)
	}
	if code, ok := one["error_code"].(string); ok {
		return "Error: " + code
	}
	var out []string
	if url, ok := one["url"].(string); ok {
		out = append(out, url)
	}
	if doc, ok := one["content"].(map[string]any); ok {
		if src, ok := doc["source"].(map[string]any); ok {
			if data, ok := src["data"].(string); ok {
				out = append(out, data)
			}
		}
	}
	for _, k := range []string{"stdout", "stderr"} {
		if v, ok := one[k].(string); ok && v != "" {
			out = append(out, fmt.Sprintf("%s:\n%s", k, v))
		}
	}
	if len(out) == 0 {
		return string(content)
	}
	return strings.Join(out, "\n")
}

// anthropicCitations is a text block's web citations.
func anthropicCitations(b anthropic.BetaTextBlock) []Citation {
	var out []Citation
	for _, c := range b.Citations {
		if c.URL != "" {
			out = append(out, Citation{Title: c.Title, URL: c.URL, Text: c.CitedText})
		}
	}
	return out
}
