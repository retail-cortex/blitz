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
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// /fork copies the session up to a turn; the source stays as it was.
func TestForkSession(t *testing.T) {
	w, llm := openTestWith(t, nil, text("one"), text("two"), text("three"), text("after the fork"))
	ctx := context.Background()
	s, _ := w.NewSession()
	for _, p := range []string{"first", "second", "third"} {
		_, err := w.Run(ctx, s.ID, api.Turn{Text: p}, func(api.Event) {})
		require.NoError(t, err)
	}
	src, err := w.storage.Get(s.ID)
	require.NoError(t, err)
	require.Len(t, src.Messages, 6)
	srcEvents := w.engine.EventCount(ctx, s.ID)

	fork, err := w.ForkSession(ctx, 1)
	require.NoError(t, err)
	assert.NotEqual(t, s.ID, fork.ID)
	active, _ := w.ActiveSession()
	assert.Equal(t, fork.ID, active.ID, "the fork is active")
	assert.Equal(t, 2, fork.MessageCount, "the first prompt and its answer")
	assert.Less(t, w.engine.EventCount(ctx, fork.ID), srcEvents)

	// The fork goes on from turn 1: the model sees only it.
	_, err = w.Run(ctx, fork.ID, api.Turn{Text: "new direction"}, func(api.Event) {})
	require.NoError(t, err)
	last := userTextAt(llm.Requests[len(llm.Requests)-1].Contents)
	assert.Contains(t, last, "new direction")
	all := llm.Requests[len(llm.Requests)-1].Contents
	joined := ""
	for _, c := range all {
		for _, p := range c.Parts {
			joined += p.Text + " "
		}
	}
	assert.Contains(t, joined, "first")
	assert.NotContains(t, joined, "second", "cut at turn 1")

	again, err := w.storage.Get(s.ID)
	require.NoError(t, err)
	assert.Len(t, again.Messages, 6, "the source is unchanged")
	assert.Equal(t, srcEvents, w.engine.EventCount(ctx, s.ID))

	whole, err := w.ForkSession(ctx, 0)
	require.NoError(t, err)
	assert.Equal(t, 4, whole.MessageCount, "all of the fork it copied")
	_, err = w.ForkSession(ctx, -1)
	assert.ErrorIs(t, err, api.ErrNotRewindPoint)
}

// /export writes prompts, answers and tool calls, secrets masked.
func TestExportSession(t *testing.T) {
	const secret = "sk-secret-0123456789abcdef"
	w, _ := openTestWith(t, func(c *config.Config) { c.LLM.OpenAI.APIKey = secret },
		toolCall("list_files", map[string]any{"path": "."}), text("Nothing there; the key "+secret+" is safe."))
	ctx := context.Background()
	s, _ := w.NewSession()
	_, err := w.RenameSession("Look around")
	require.NoError(t, err)
	_, err = w.Run(ctx, s.ID, api.Turn{Text: "what's here?"}, func(api.Event) {})
	require.NoError(t, err)

	md, err := w.ExportSession("")
	require.NoError(t, err)
	for _, want := range []string{"# Look around", "## You", "what's here?", "- `list_files` {\"path\":\".\"}", "  - → ", "## blitz", "Nothing there"} {
		assert.Contains(t, md, want)
	}
	assert.NotContains(t, md, secret)

	_, err = ExportSession(w.cfg, "no-such-session")
	assert.Error(t, err)
}

// Forking needs an active session that isn't running a turn, and a cut
// point the conversation's log knows.
func TestForkSessionRefusals(t *testing.T) {
	w := openTest(t)
	ctx := context.Background()
	_, err := w.ForkSession(ctx, 0)
	assert.ErrorIs(t, err, api.ErrNoActiveSession)
	_, err = w.ExportSession("")
	assert.ErrorIs(t, err, api.ErrNoActiveSession)

	s := newSession(t, w)
	for _, m := range []session.Message{{Role: "user", Content: "one"}, {Role: "model", Content: "a"}, {Role: "user", Content: "two"}} {
		require.NoError(t, w.storage.Append(m)) // no event counts: an older version's
	}
	_, err = w.ForkSession(ctx, 1)
	assert.ErrorIs(t, err, api.ErrCantRewindConversation)
	w.turnStarted(s.ID)
	_, err = w.ForkSession(ctx, 0)
	assert.ErrorIs(t, err, api.ErrSessionBusy)
	w.turnEnded(s.ID)
}

// A session with no event log (an older version's) is exported from its
// transcript; an untitled one is named by its ID, and a copy says what it
// was copied from.
func TestExportSessionFromTheTranscript(t *testing.T) {
	w := openTest(t)
	s := newSession(t, w)
	for _, m := range []session.Message{
		{Role: "model", Content: "hi"},
	} {
		require.NoError(t, w.storage.Append(m))
	}
	md, err := w.ExportSession(s.ID)
	require.NoError(t, err)
	assert.Contains(t, md, "# Session "+s.ID)
	assert.Contains(t, md, "## Blitz")
	require.NoError(t, w.storage.Append(session.Message{Role: "user", Content: "go on", Kind: session.KindHook}))
	require.NoError(t, w.storage.Append(session.Message{Role: "user", Content: "hello"}))
	md, err = w.ExportSession(s.ID)
	require.NoError(t, err)
	assert.Contains(t, md, "## You (hook)")
	assert.Contains(t, md, "## You · ")
	copied, err := w.storage.Fork(s.ID)
	require.NoError(t, err)
	md, err = ExportSession(w.cfg, copied.ID)
	require.NoError(t, err)
	assert.Contains(t, md, "copied from `"+s.ID+"`")
}

// The event log is written as the conversation: compactions noted, tool
// results in brief, thoughts and empty events left out, one heading per
// speaker in a row.
func TestWriteEvents(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	ev := func(author string, parts ...*genai.Part) *adksession.Event {
		e := adksession.NewEvent(context.Background(), "inv")
		e.Author, e.Timestamp, e.Content = author, at, &genai.Content{Parts: parts}
		return e
	}
	compacted := adksession.NewEvent(context.Background(), "inv")
	compacted.Actions.Compaction = &adksession.EventCompaction{}
	empty := adksession.NewEvent(context.Background(), "inv")
	var b strings.Builder
	writeEvents(&b, []*adksession.Event{
		compacted, empty,
		ev("user", &genai.Part{Text: "list"}),
		ev("blitz", &genai.Part{Text: "thinking", Thought: true}),
		ev("blitz", &genai.Part{FunctionResponse: &genai.FunctionResponse{Name: "ls", Response: map[string]any{"files": []string{"a"}}}}),
		ev("blitz", &genai.Part{Text: "one file"}),
		ev("blitz", &genai.Part{Text: "that's all"}),
	})
	got := b.String()
	assert.Contains(t, got, "summarised")
	assert.Contains(t, got, `  - → {"files":["a"]}`)
	assert.NotContains(t, got, "thinking")
	assert.Equal(t, 1, strings.Count(got, "## blitz"), "one heading for consecutive replies: %s", got)

	b.Reset()
	writeMessages(&b, []session.Message{{Role: "user", Kind: "x", Content: "note"}, {Role: "user", Content: "q"}})
	assert.Equal(t, "\n## You (x)\n\nnote\n\n## You\n\nq\n", b.String(), "no time when there is none")
	assert.Equal(t, "note", cmpKind(""))
}
