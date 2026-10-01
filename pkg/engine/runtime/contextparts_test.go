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
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// partsByName indexes context parts by name.
func partsByName(parts []api.ContextPart) map[string]int64 {
	out := map[string]int64{}
	for _, p := range parts {
		out[p.Name] = p.Tokens
	}
	return out
}

// ContextParts splits a session's next prompt into its kinds of content,
// scaled to the total the provider reported.
func TestContextParts(t *testing.T) {
	thought := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{Text: "thinking hard", Thought: true}, {Text: "a reply long enough to count for something"}}}
	f := newEngineWith(t, fixtureOpts{cfg: imagesDir(t)},
		toolCall("list_files", map[string]any{"path": "."}), thought, textContent("a cat"))
	require.NoError(t, f.eng.SetInstructions(context.Background(), "project memory that the agent always reads"))
	_, err := collect(t, f.eng, "s", "list the files in this directory, please")
	require.NoError(t, err)
	img, err := f.tools.AddImage("cat.png", testPNG(t, 8, 8))
	require.NoError(t, err)
	require.NoError(t, f.eng.Execute(context.Background(), "s", "what is this?", nil, WithAttachments(images.Part(img))))

	got := partsByName(f.eng.ContextParts(context.Background(), "s", 0))
	for _, name := range []string{"system_prompt", "instructions", "tool_declarations", "user_messages", "replies", "tool_calls", "tool_results"} {
		t.Run(name, func(t *testing.T) {
			assert.Positive(t, got[name])
		})
	}
	assert.Equal(t, int64(imageTokens), got["images"])
	assert.Zero(t, got["summary"], "nothing was compacted")

	scaled := f.eng.ContextParts(context.Background(), "s", 100)
	var sum int64
	for _, p := range scaled {
		sum += p.Tokens
	}
	assert.InDelta(t, 100, sum, float64(len(scaled)), "parts are scaled to the reported total")

	assert.Empty(t, partsByName(f.eng.ContextParts(context.Background(), "missing", 0))["user_messages"], "an unknown session has no messages")
}

// After a compaction only the summary and what follows it count.
func TestContextPartsAfterCompaction(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{}, textContent("r1"), textContent("r2"), textContent("THE SUMMARY OF EVERYTHING SO FAR"), textContent("r3"))
	runTurns(t, f.eng, "s", "t1", "t2")
	_, err := f.eng.Compact(context.Background(), "s", "", 1)
	require.NoError(t, err)
	got := partsByName(f.eng.ContextParts(context.Background(), "s", 0))
	assert.Positive(t, got["summary"])
}

// An unavailable model answers every call with why.
func TestUnavailableModel(t *testing.T) {
	m := NewUnavailableModel("gone", "no key")
	assert.Equal(t, "gone", m.Name())
	for _, err := range m.GenerateContent(context.Background(), nil, false) {
		assert.ErrorIs(t, err, ErrModelUnavailable)
		assert.ErrorContains(t, err, "no key")
	}
}
