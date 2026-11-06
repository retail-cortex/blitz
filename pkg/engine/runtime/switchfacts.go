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

	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// SwitchFacts is what the engine can tell of moving a conversation to a
// model, for the switch's preview (spec_056 CA-101).
type SwitchFacts struct {
	// CompactAt is where the model's conversations are compacted (CA-110):
	// past it, the conversation is compacted before the move.
	CompactAt int
	// Unseen names the conversation's attachments the model won't see as
	// they are (CA-63).
	Unseen []string
	// NoTools is set for a model that can't call tools (CA-103).
	NoTools bool
}

// SwitchFacts says what moving sessionID to llm means.
func (e *Engine) SwitchFacts(ctx context.Context, sessionID string, llm model.LLM) SwitchFacts {
	var f SwitchFacts
	f.CompactAt = e.cfg.Context.TokenThreshold
	info, known := infoOf(llm)
	if known {
		f.CompactAt = info.CompactionPoint(f.CompactAt)
		f.NoTools = info.Tools == ToolsNone
	}
	if !e.cfg.Context.Compaction {
		f.CompactAt = 0
	}
	var contents []*genai.Content
	if got, err := e.sessions.Get(ctx, &session.GetRequest{AppName: appName, UserID: "user", SessionID: sessionID}); err == nil {
		for _, ev := range contextEvents(finalEvents(got.Session)) {
			if ev.Content != nil {
				contents = append(contents, ev.Content)
			}
		}
	}
	upload := e.media.uploader() != nil
	f.Unseen = unseenAttachments(contents, e.toolReg.Images(), acceptedFor(llm, upload), upload)
	return f
}
