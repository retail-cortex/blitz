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

package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// /btw answers from the session's context and leaves nothing behind: not
// in the transcript, not in what the next prompt sends.
func TestBtwIsAnsweredAndForgotten(t *testing.T) {
	app, llm := newCommandApp(t, "remember pineapple\n/btw\n/btw what was the word?\ncarry on\n/exit\n",
		genai.NewContentFromText("noted", genai.RoleModel),
		genai.NewContentFromText("it was pineapple", genai.RoleModel),
		genai.NewContentFromText("carrying on", genai.RoleModel))
	out := captureStdout(t, func() { RunREPL(context.Background(), app) })

	require.Contains(t, out, "Usage: /btw <question>", "output:\n%s", out)
	require.Contains(t, out, "Side question", "output:\n%s", out)
	require.Equal(t, 3, llm.Calls(), "%d model calls", llm.Calls())
	aside := requestTextAt(llm, 1)
	require.Contains(t, aside, "remember pineapple", "the side question lacked context:\n%s", aside)
	require.Contains(t, aside, "what was the word?", "the side question lacked context:\n%s", aside)
	next := requestTextAt(llm, 2)
	require.NotContains(t, next, "what was the word", "the next prompt saw the side question:\n%s", next)
	require.NotContains(t, next, "it was pineapple", "the next prompt saw the side question:\n%s", next)
	var recorded []string
	for _, m := range local(app).Storage().Active().Messages {
		recorded = append(recorded, m.Content)
	}
	got := strings.Join(recorded, "|")
	require.Equal(t, "remember pineapple|noted|carry on|carrying on", got, "transcript: %s", got)
}
