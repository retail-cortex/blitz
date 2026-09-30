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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// rewindSession runs two prompts: the first creates notes.txt, the second
// edits it. It returns the workspace and its config.
func rewindSession(t *testing.T, extra ...*genai.Content) (*Workspace, *config.Config, *runtime.MockLLM, string) {
	t.Helper()
	var cfg *config.Config
	replies := []*genai.Content{
		toolCall("create_file", map[string]any{"path": "notes.txt", "content": "v1\n"}), text("created"),
		toolCall("replace_in_file", map[string]any{"path": "notes.txt", "target_content": "v1", "replacement_content": "v2"}), text("edited"),
	}
	w, llm := openTestWith(t, func(c *config.Config) { cfg = c }, append(replies, extra...)...)
	w.SetUI(func(context.Context, api.ApprovalRequest) (api.Decision, error) { return api.DecisionOnce, nil }, nil)
	s, _, err := w.OpenSession("", false)
	require.NoError(t, err)
	for _, p := range []string{"make notes", "second draft"} {
		t.Run(p, func(t *testing.T) {
			_, err := w.Run(context.Background(), s.ID, api.Turn{Text: p}, func(api.Event) {})
			require.NoError(t, err)
		})
	}
	return w, cfg, llm, s.ID
}

func notes(t *testing.T, cfg *config.Config) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(cfg.Tools.WorkspaceDir, "notes.txt"))
	if err != nil {
		return "<none>"
	}
	return string(b)
}

func TestRewindPoints(t *testing.T) {
	w, _, _, id := rewindSession(t)
	// No turn runs: it's too late for one, but recorded (BL-SVC-10).
	require.ErrorIs(t, w.Steer(context.Background(), id, "a steer message"), api.ErrSteerTooLate)
	points, err := w.RewindPoints()
	require.NoError(t, err)
	require.Len(t, points, 2, "points %+v", points)
	require.Equal(t, "make notes", points[0].Text, "points %+v", points)
	require.Equal(t, "second draft", points[1].Text, "points %+v", points)
	require.Equal(t, 0, points[0].Index, "points %+v", points)
	require.Equal(t, 2, points[1].Index, "points %+v", points)
	for _, p := range points {
		assert.True(t, p.Conversation, "point %+v", p)
		assert.Len(t, p.Files, 1, "point %+v", p)
		assert.Equal(t, "notes.txt", p.Files[0], "point %+v", p)
	}
}

func TestRewindCodeAndConversation(t *testing.T) {
	w, cfg, llm, id := rewindSession(t, text("redone"))
	res, err := w.Rewind(context.Background(), 2, api.RewindBoth, false)
	require.NoError(t, err)
	require.Equal(t, "second draft", res.Prompt, "rewind %+v, notes %q", res, notes(t, cfg))
	require.Len(t, res.Restored, 1, "rewind %+v, notes %q", res, notes(t, cfg))
	require.Equal(t, "v1\n", notes(t, cfg), "rewind %+v, notes %q", res, notes(t, cfg))
	a, _ := w.ActiveSession()
	assert.Equal(t, 2, a.MessageCount, "transcript has %d messages, want 2", a.MessageCount)
	_, err = w.Run(context.Background(), id, api.Turn{Text: "third try"}, func(api.Event) {})
	require.NoError(t, err)
	var sent strings.Builder
	for _, c := range llm.Requests[len(llm.Requests)-1].Contents {
		for _, p := range c.Parts {
			sent.WriteString(p.Text + "|")
		}
	}
	assert.NotContains(t, sent.String(), "second draft", "the model still sees the rewound prompt: %s", sent.String())
	assert.Contains(t, sent.String(), "make notes", "the model still sees the rewound prompt: %s", sent.String())
	points, _ := w.RewindPoints()
	assert.Len(t, points, 2, "points after rewinding %+v", points)
	assert.Equal(t, "third try", points[1].Text, "points after rewinding %+v", points)
}

func TestRewindConversationOnlyKeepsFiles(t *testing.T) {
	w, cfg, _, id := rewindSession(t, text("nothing to change"))
	_, err := w.Rewind(context.Background(), 2, api.RewindConversation, false)
	require.NoError(t, err)
	assert.Equal(t, "v2\n", notes(t, cfg), "files changed: %q", notes(t, cfg))
	// A new prompt takes the rewound one's place; rewinding its code leaves
	// the kept change, which came before it.
	_, err = w.Run(context.Background(), id, api.Turn{Text: "look around"}, func(api.Event) {})
	require.NoError(t, err)
	res, err := w.Rewind(context.Background(), 2, api.RewindCode, false)
	require.NoError(t, err, "rewinding the new prompt: %+v %v, notes %q", res, err, notes(t, cfg))
	require.Len(t, res.Restored, 0, "rewinding the new prompt: %+v %v, notes %q", res, err, notes(t, cfg))
	require.Equal(t, "v2\n", notes(t, cfg), "rewinding the new prompt: %+v %v, notes %q", res, err, notes(t, cfg))
	// The kept change now belongs before the next prompt: rewinding the
	// first prompt's code still restores through it.
	_, err = w.Rewind(context.Background(), 0, api.RewindCode, false)
	require.NoError(t, err)
	assert.Equal(t, "<none>", notes(t, cfg), "notes after rewinding the first prompt: %q", notes(t, cfg))
}

func TestRewindCodeOnlyKeepsTheConversation(t *testing.T) {
	w, cfg, _, _ := rewindSession(t)
	_, err := w.Rewind(context.Background(), 2, api.RewindCode, false)
	require.NoError(t, err)
	a, _ := w.ActiveSession()
	assert.Equal(t, 4, a.MessageCount, "messages %d, notes %q", a.MessageCount, notes(t, cfg))
	assert.Equal(t, "v1\n", notes(t, cfg), "messages %d, notes %q", a.MessageCount, notes(t, cfg))
}

func TestRewindRefusals(t *testing.T) {
	w, cfg, _, id := rewindSession(t)
	ctx := context.Background()
	for _, c := range []struct {
		index int
		mode  api.RewindMode
		want  error
	}{
		{1, api.RewindBoth, api.ErrNotRewindPoint}, // a reply
		{9, api.RewindBoth, api.ErrNotRewindPoint},
		{0, "sideways", api.ErrUnknownRewindMode},
	} {
		_, err := w.Rewind(ctx, c.index, c.mode, false)
		assert.ErrorIs(t, err, c.want, "%d %s: %v", c.index, c.mode, err)
	}
	// A file changed since: nothing happens, not even to the conversation.
	os.WriteFile(filepath.Join(cfg.Tools.WorkspaceDir, "notes.txt"), []byte("mine\n"), 0o644)
	_, err := w.Rewind(ctx, 2, api.RewindBoth, false)
	require.ErrorIs(t, err, api.ErrUndoConflict, "conflict: %v", err)
	a, _ := w.ActiveSession()
	assert.Equal(t, 4, a.MessageCount, "a refused rewind changed the conversation")
	w.turnStarted(id)
	_, err = w.Rewind(ctx, 2, api.RewindCode, true)
	assert.ErrorIs(t, err, api.ErrSessionBusy, "during a turn: %v", err)
	w.turnEnded(id)
	_, err = w.Rewind(ctx, 2, api.RewindBoth, true)
	assert.NoError(t, err, "forced: %v %q", err, notes(t, cfg))
	assert.Equal(t, "v1\n", notes(t, cfg), "forced: %v %q", err, notes(t, cfg))
}

// The conversation can be rewound after the workspace was closed and the
// session resumed.
func TestRewindAfterResume(t *testing.T) {
	w, cfg, _, id := rewindSession(t)
	w.Close()
	w2, err := Open(context.Background(), cfg, Options{Model: runtime.NewMockLLM("gemini-3.8-flash"), NewModel: mockModels})
	require.NoError(t, err)
	defer w2.Close()
	_, _, openErr := w2.OpenSession(id, false)
	require.NoError(t, openErr)
	res, err := w2.Rewind(context.Background(), 2, api.RewindBoth, false)
	require.NoError(t, err, "%+v %v %q", res, err, notes(t, cfg))
	require.Equal(t, "v1\n", notes(t, cfg), "%+v %v %q", res, err, notes(t, cfg))
	require.Equal(t, "second draft", res.Prompt, "%+v %v %q", res, err, notes(t, cfg))
	w2.Close()
	w3, err := Open(context.Background(), cfg, Options{Model: runtime.NewMockLLM("gemini-3.8-flash"), NewModel: mockModels})
	require.NoError(t, err)
	defer w3.Close()
	s, _, err := w3.OpenSession(id, false)
	require.NoError(t, err, "reopened: %d messages,", s.MessageCount)
	require.Equal(t, 2, s.MessageCount, "reopened: %d messages, %v", s.MessageCount, err)
}
