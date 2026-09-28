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

	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	adksession "google.golang.org/adk/v2/session"
)

// requestText joins the text the model was sent in its last request.
func requestText(llm *runtime.MockLLM) string { return requestTextAt(llm, len(llm.Requests)-1) }

// requestTextAt joins the text of the model's i-th request.
func requestTextAt(llm *runtime.MockLLM, i int) string {
	var b strings.Builder
	for _, c := range llm.Requests[i].Contents {
		for _, p := range c.Parts {
			b.WriteString(p.Text + "\n")
		}
	}
	return b.String()
}

// A snapshot restores what the model sees, not just the transcript, and
// later turns in the original session don't leak into it.
func TestSessionSaveAndLoadRestoreTheModelsContext(t *testing.T) {
	app, llm := newCommandApp(t, "")
	// The workspace keeps transcripts and the model's events side by side,
	// which snapshots copy together.
	st, eng := local(app).Storage(), local(app).Engine()
	ctx := context.Background()
	run := func(cmd string) string { return captureStdout(t, func() { HandleCommand(ctx, cmd, app) }) }
	turn := func(prompt string) {
		t.Helper()
		st.AddMessage("user", prompt)
		require.NoError(t, eng.Execute(ctx, st.Active().ID, prompt, func(*adksession.Event) error { return nil }))
	}

	assert.Contains(t, run("/session save early"), "No active session", "no session")
	orig, _ := st.CreateSession("", "fruit talk", "blitz")
	assert.Contains(t, run("/session save empty"), "nothing to save", "empty session")
	turn("remember pineapple")

	assert.Contains(t, run("/session save"), "Usage: /session save", "usage")
	require.Contains(t, run("/session save fruit"), "Saved snapshot fruit (1 message)", "save")
	assert.Contains(t, run("/session save fruit"), "--force replaces it", "taken")
	assert.Contains(t, run("/session save ../x"), "invalid snapshot name", "bad name")
	require.Equal(t, orig.ID, st.Active().ID, "saving switched sessions")
	turn("actually, mango") // continues the original only

	out := run("/session load fruit")
	branch := st.Active()
	require.Contains(t, out, "from snapshot fruit", "load:\n%s", out)
	require.NotEqual(t, orig.ID, branch.ID, "load:\n%s", out)
	require.Contains(t, out, "remember pineapple", "load:\n%s", out)
	turn("which fruit?")
	sent := requestText(llm)
	require.Contains(t, sent, "remember pineapple", "the model saw:\n%s", sent)
	require.NotContains(t, sent, "mango", "the model saw:\n%s", sent)

	out = run("/session list")
	assert.Contains(t, out, "snapshot fruit", "list:\n%s", out)
	// /resume takes names too, and a second load branches again.
	out = run("/resume fruit")
	assert.Contains(t, out, "from snapshot fruit", "/resume <name>:\n%s", out)
	assert.NotEqual(t, branch.ID, st.Active().ID, "/resume <name>:\n%s", out)
	// An ID still resumes in place, with everything said since.
	out = run("/resume " + orig.ID)
	assert.Contains(t, out, "Resumed session "+orig.ID, "/resume <id>:\n%s", out)
	turn("and now?")
	sent = requestText(llm)
	require.Contains(t, sent, "mango", "the original lost its history:\n%s", sent)
	out = run("/session save fruit --force")
	assert.Contains(t, out, "Saved snapshot fruit (3 messages)", "replace:\n%s", out)
}
