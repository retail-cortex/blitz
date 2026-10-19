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

package engine

import (
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/session"
)

// maxToolText bounds each string in a saved tool call's arguments and
// result: a reopened chat shows the call, not every file it read.
const maxToolText = 4 << 10

// SessionTools are the tool calls session id made, in order, each with its
// result and the prompt it ran under, read from its event log (a session
// with none, or none recorded, has none).
func (w *Workspace) SessionTools(id string) ([]api.SessionTool, error) {
	events, err := session.ReadEvents(w.cfg.Session.StorageDir, id)
	if err != nil {
		return nil, err
	}
	var out []api.SessionTool
	turn, prompt := -1, ""
	byID := map[string]int{}
	for _, ev := range events {
		if ev == nil || ev.Content == nil {
			continue
		}
		var text strings.Builder
		for _, part := range ev.Content.Parts {
			switch {
			case part.FunctionCall != nil:
				fc := part.FunctionCall
				byID[fc.ID] = len(out)
				out = append(out, api.SessionTool{Turn: max(turn, 0), Prompt: prompt, ID: fc.ID, Name: fc.Name, Args: clipValues(fc.Args)})
			case part.FunctionResponse != nil:
				fr := part.FunctionResponse
				if i, ok := byID[fr.ID]; ok && out[i].Result == nil {
					out[i].Result = clipValues(fr.Response)
				}
			case part.Text != "" && !part.Thought:
				text.WriteString(part.Text)
			}
		}
		// A prompt starts a turn: the user's text, not a tool's answer.
		if ev.Author == "user" && text.Len() > 0 {
			turn++
			prompt = clip(text.String(), 200)
		}
	}
	return out, nil
}

// clipValues is m with long strings cut, at any depth.
func clipValues(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = clipValue(v)
	}
	return out
}

func clipValue(v any) any {
	switch x := v.(type) {
	case string:
		return clip(x, maxToolText)
	case map[string]any:
		return clipValues(x)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = clipValue(e)
		}
		return out
	}
	return v
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
