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
	"context"
	"encoding/json"

	"github.com/retail-cortex/blitz/pkg/api"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// imageTokens is roughly what an image costs in a prompt.
const imageTokens = 1500

// ContextParts estimates what a session's next prompt is made of
// (spec_parity_027 PAR-UI-06): the active agent's system prompt, the
// project instructions and notes, the tool declarations, the prompts,
// replies, tool calls and results, images and summaries of earlier
// conversation. Each is its text's length / 4 (images a flat amount),
// scaled so they add up to total when the model reported one.
func (e *Engine) ContextParts(ctx context.Context, sessionID string, total int64) []api.ContextPart {
	e.mu.RLock()
	spec, ok := e.agentReg.Get(e.active)
	extra := e.extraInstructions
	e.mu.RUnlock()
	chars := map[string]int{}
	order := []string{"system_prompt", "instructions", "tool_declarations", "user_messages", "replies", "tool_calls", "tool_results", "images", "summary"}
	if ok {
		chars["system_prompt"] = len(spec.InterpolatePrompt(e.cfg.Blitz.AgencyLevel))
		for _, t := range e.toolReg.GetToolsForAgent(spec.Tools) {
			if d, ok := t.(interface {
				Declaration() *genai.FunctionDeclaration
			}); ok {
				b, _ := json.Marshal(d.Declaration())
				chars["tool_declarations"] += len(b)
			}
		}
	}
	chars["instructions"] = len(extra)
	images := 0
	if got, err := e.sessions.Get(ctx, &session.GetRequest{AppName: appName, UserID: "user", SessionID: sessionID}); err == nil {
		for _, ev := range contextEvents(finalEvents(got.Session)) {
			if ev.Actions.Compaction != nil {
				if c := ev.Actions.Compaction.CompactedContent; c != nil {
					for _, p := range c.Parts {
						chars["summary"] += len(p.Text)
					}
				}
				continue
			}
			if ev.Content == nil {
				continue
			}
			for _, p := range ev.Content.Parts {
				switch {
				case p.FunctionCall != nil:
					b, _ := json.Marshal(p.FunctionCall.Args)
					chars["tool_calls"] += len(b) + len(p.FunctionCall.Name)
				case p.FunctionResponse != nil:
					b, _ := json.Marshal(p.FunctionResponse.Response)
					chars["tool_results"] += len(b)
				case p.InlineData != nil || p.FileData != nil:
					images++
				case p.Thought:
				case ev.Author == "user":
					chars["user_messages"] += len(p.Text)
				default:
					chars["replies"] += len(p.Text)
				}
			}
		}
	}
	est := map[string]int64{}
	var sum int64
	for k, c := range chars {
		est[k] = int64(c / 4)
		sum += est[k]
	}
	est["images"] = int64(images * imageTokens)
	sum += est["images"]
	var out []api.ContextPart
	for _, name := range order {
		v := est[name]
		if v == 0 {
			continue
		}
		if total > 0 && sum > 0 {
			v = v * total / sum
		}
		out = append(out, api.ContextPart{Name: name, Tokens: v})
	}
	return out
}

// contextEvents are the events a prompt carries: those after the latest
// compaction's summary (the summary itself included).
func contextEvents(events []*session.Event) []*session.Event {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Actions.Compaction != nil {
			return events[i:]
		}
	}
	return events
}
