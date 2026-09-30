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
	"context"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func systemText(c *genai.GenerateContentConfig) string {
	if c == nil || c.SystemInstruction == nil {
		return ""
	}
	s := ""
	for _, p := range c.SystemInstruction.Parts {
		s += p.Text
	}
	return s
}

// The agent remembers with its tool (secrets masked); the notes reach its
// instructions after a reload, and can be listed and forgotten.
func TestAgentNotes(t *testing.T) {
	const key = "sk-test-0123456789abcdefghij"
	remember := toolCall("remember", map[string]any{"note": "The deploy key is " + key + "; deploys go through make ship", "kind": "fact"})
	w, llm := openTestWith(t, func(c *config.Config) { c.LLM.OpenAI.APIKey = key }, remember, text("noted"), text("ok"))
	ctx := context.Background()
	s, _, err := w.OpenSession("", false)
	require.NoError(t, err)
	_, err = w.Run(ctx, s.ID, api.Turn{Text: "we deploy with make ship"}, func(api.Event) {})
	require.NoError(t, err)

	notes, err := w.ListNotes()
	require.NoError(t, err)
	require.Len(t, notes, 1)
	assert.Equal(t, "fact", notes[0].Kind)
	assert.Contains(t, notes[0].Text, "make ship")
	assert.NotContains(t, notes[0].Text, key, "secrets are never kept")

	_, err = w.ReloadMemory(ctx)
	require.NoError(t, err)
	_, err = w.Run(ctx, s.ID, api.Turn{Text: "hi"}, func(api.Event) {})
	require.NoError(t, err)
	sys := systemText(llm.Requests[len(llm.Requests)-1].Config)
	assert.Contains(t, sys, "Notes you saved in earlier sessions")
	assert.Contains(t, sys, "deploys go through make ship")

	assert.ErrorIs(t, w.ForgetNote("nope"), api.ErrNoNote)
	require.NoError(t, w.ForgetNote(notes[0].Name))
	notes, _ = w.ListNotes()
	assert.Empty(t, notes)
}

func TestAgentNotesOff(t *testing.T) {
	off := false
	remember := toolCall("remember", map[string]any{"note": "x"})
	w, _ := openTestWith(t, func(c *config.Config) { c.Memory.Auto = &off }, remember, text("ok"))
	s, _, err := w.OpenSession("", false)
	require.NoError(t, err)
	_, err = w.Run(context.Background(), s.ID, api.Turn{Text: "remember x"}, func(api.Event) {})
	require.NoError(t, err)
	notes, err := w.ListNotes()
	require.NoError(t, err)
	assert.Empty(t, notes)
}
