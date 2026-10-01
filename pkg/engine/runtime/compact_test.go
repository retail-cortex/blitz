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
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	cpsession "github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// lastRequestText joins every text, call and response in the latest model request.
func lastRequestText(m *MockLLM) string {
	req := m.Requests[len(m.Requests)-1]
	var sb strings.Builder
	for _, c := range req.Contents {
		for _, p := range c.Parts {
			sb.WriteString(p.Text)
			if p.FunctionCall != nil {
				sb.WriteString(" CALL:" + p.FunctionCall.ID)
			}
			if p.FunctionResponse != nil {
				sb.WriteString(" RESP:" + p.FunctionResponse.ID)
			}
			sb.WriteString("|")
		}
	}
	return sb.String()
}

func runTurns(t *testing.T, eng *Engine, sid string, prompts ...string) {
	t.Helper()
	for _, p := range prompts {
		t.Run(p, func(t *testing.T) {
			_, err := collect(t, eng, sid, p)
			require.NoError(t, err)
		})
	}
}

func TestCompactReplacesOldTurns(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{},
		textContent("reply-one"), textContent("reply-two"), textContent("reply-three"),
		textContent("SUMMARY-XYZ: user wants a CLI"), // summarizer call
		textContent("reply-four"))
	runTurns(t, f.eng, "s", "turn-one", "turn-two", "turn-three")

	res, err := f.eng.Compact(context.Background(), "s", "the CLI flags", 1)
	require.NoError(t, err)
	assert.Equal(t, 4, res.EventsCompacted, "result %+v", res)
	assert.NotEqual(t, 0, res.SummaryChars, "result %+v", res)
	summarizerPrompt := lastRequestText(f.llm)
	assert.Contains(t, summarizerPrompt, "the CLI flags", "summarizer prompt missing focus or history: %s", summarizerPrompt)
	assert.Contains(t, summarizerPrompt, "turn-one", "summarizer prompt missing focus or history: %s", summarizerPrompt)

	runTurns(t, f.eng, "s", "turn-four")
	got := lastRequestText(f.llm)
	assert.Contains(t, got, "SUMMARY-XYZ", "summary not in next prompt: %s", got)
	for _, gone := range []string{"turn-one", "reply-one", "turn-two", "reply-two"} {
		t.Run(gone, func(t *testing.T) {
			assert.NotContains(t, got, gone, "compacted %q still sent: %s", gone, got)
		})
	}
	for _, kept := range []string{"turn-three", "reply-three", "turn-four"} {
		t.Run(kept, func(t *testing.T) {
			assert.Contains(t, got, kept, "kept %q missing: %s", kept, got)
		})
	}
}

func TestCompactNothingToDoAndEmptySummary(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{}, textContent("only"), textContent("two"), textContent(""), textContent("three"))
	_, err := f.eng.Compact(context.Background(), "missing", "", 1)
	assert.ErrorIs(t, err, api.ErrNothingToCompact, "unknown session: %v", err)
	runTurns(t, f.eng, "s", "first")
	_, err = f.eng.Compact(context.Background(), "s", "", 1)
	assert.ErrorIs(t, err, api.ErrNothingToCompact, "single turn: %v", err)
	runTurns(t, f.eng, "s", "second")
	// The summarizer returns nothing: history must stay intact.
	_, err = f.eng.Compact(context.Background(), "s", "", 1)
	require.Error(t, err, "an empty summary must be rejected")
	runTurns(t, f.eng, "s", "third")
	got := lastRequestText(f.llm)
	assert.Contains(t, got, "first", "failed compaction dropped history: %s", got)
}

func TestCompactKeepsToolCallWithResult(t *testing.T) {
	call := toolCall("list_files", map[string]any{})
	call.Parts[0].FunctionCall.ID = "call-keep"
	f := newEngineWith(t, fixtureOpts{},
		textContent("r1"), call, textContent("listed"), textContent("SUMMARY"), textContent("after"))
	runTurns(t, f.eng, "s", "hello", "list please")
	_, err := f.eng.Compact(context.Background(), "s", "", 1)
	require.NoError(t, err)
	runTurns(t, f.eng, "s", "next")
	got := lastRequestText(f.llm)
	assert.Contains(t, got, "CALL:call-keep", "tool call and result must stay together in the kept turn: %s", got)
	assert.Contains(t, got, "RESP:call-keep", "tool call and result must stay together in the kept turn: %s", got)
	assert.NotContains(t, got, "hello", "first turn should be summarized: %s", got)
}

func TestCompactPersistsAndWorksWithAutoCompactionOff(t *testing.T) {
	dir := t.TempDir()
	off := func(c *config.Config) { c.Context.Compaction = false }
	svc, _ := cpsession.NewPersistentService(dir)
	f := newEngineWith(t, fixtureOpts{cfg: off, opts: []Option{WithSessionService(svc)}},
		textContent("a1"), textContent("a2"), textContent("PERSISTED-SUMMARY"))
	runTurns(t, f.eng, "s", "q1", "q2")
	_, err := f.eng.Compact(context.Background(), "s", "", 1)
	require.NoError(t, err)

	svc2, _ := cpsession.NewPersistentService(dir)
	g := newEngineWith(t, fixtureOpts{cfg: off, opts: []Option{WithSessionService(svc2)}}, textContent("resumed"))
	runTurns(t, g.eng, "s", "q3")
	got := lastRequestText(g.llm)
	assert.Contains(t, got, "PERSISTED-SUMMARY", "resumed session should use the stored summary: %s", got)
	assert.NotContains(t, got, "q1", "resumed session should use the stored summary: %s", got)
	assert.Contains(t, got, "q2", "resumed session should use the stored summary: %s", got)
}

func TestCompactRollsEarlierSummary(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{},
		textContent("r1"), textContent("r2"), textContent("FIRST-SUMMARY"),
		textContent("r3"), textContent("SECOND-SUMMARY"), textContent("r4"))
	runTurns(t, f.eng, "s", "t1", "t2")
	_, err := f.eng.Compact(context.Background(), "s", "", 1)
	require.NoError(t, err)
	runTurns(t, f.eng, "s", "t3")
	_, err = f.eng.Compact(context.Background(), "s", "", 1)
	require.NoError(t, err)
	second := lastRequestText(f.llm)
	assert.Contains(t, second, "FIRST-SUMMARY", "second summarization should see the first summary: %s", second)
	assert.NotContains(t, second, "t1", "events covered by the first summary should not be re-summarized raw: %s", second)
	runTurns(t, f.eng, "s", "t4")
	got := lastRequestText(f.llm)
	assert.Contains(t, got, "SECOND-SUMMARY", "rolled summary should replace the first: %s", got)
	assert.NotContains(t, got, "FIRST-SUMMARY", "rolled summary should replace the first: %s", got)
	assert.NotContains(t, got, "t2", "rolled summary should replace the first: %s", got)
	assert.Contains(t, got, "t3", "recent turns missing: %s", got)
	assert.Contains(t, got, "t4", "recent turns missing: %s", got)
	_ = genai.RoleUser
}

// ev is a session event at second sec of a fixed day.
func ev(id, author string, sec int, text string) *session.Event {
	e := &session.Event{ID: id, InvocationID: "inv-" + id, Author: author, Timestamp: time.Date(2026, 1, 1, 0, 0, sec, 0, time.UTC)}
	e.LLMResponse.Content = genai.NewContentFromText(text, genai.RoleUser)
	return e
}

// The compaction helpers on hand-built events: an event inside the
// window's time span but not in it is listed as excluded (unless an earlier
// summary covers it), excluded events stay in the transcript, empty
// summaries are dropped, and a clock behind the conversation is refused.
func TestCompactionHelpers(t *testing.T) {
	a, b, c, d := ev("a", "user", 1, "A"), ev("b", "model", 2, "B"), ev("c", "user", 3, "C"), ev("d", "model", 4, "D")
	earlier := &session.EventCompaction{StartTimestamp: a.Timestamp, EndTimestamp: a.Timestamp, CompactedContent: genai.NewContentFromText("S0", genai.RoleModel)}
	rolled := ev("r", "user", 5, "")
	rolled.Actions.Compaction = earlier

	got, err := compactionEvent([]*session.Event{d, rolled, b}, []*session.Event{a, b, c, d, rolled}, textContent("S1"))
	require.NoError(t, err)
	excl := got.Actions.Compaction.ExcludedEvents
	require.Len(t, excl, 1, "c is in the span and not summarized; a is covered by the earlier summary: %+v", excl)
	assert.Equal(t, "inv-c", excl[0].InvocationID)
	assert.Equal(t, b.Timestamp, got.Actions.Compaction.StartTimestamp)

	future := ev("f", "user", 0, "F")
	future.Timestamp = time.Now().Add(time.Hour)
	_, err = compactionEvent([]*session.Event{future}, []*session.Event{future}, textContent("S"))
	assert.ErrorContains(t, err, "clock is behind")

	summary := ev("s", "user", 6, "")
	summary.Actions.Compaction = &session.EventCompaction{StartTimestamp: a.Timestamp, EndTimestamp: c.Timestamp, CompactedContent: genai.NewContentFromText("S2", genai.RoleModel),
		ExcludedEvents: []session.EventRef{{InvocationID: "inv-b", Timestamp: b.Timestamp}}}
	empty := ev("e", "user", 7, "")
	empty.Actions.Compaction = &session.EventCompaction{}
	var texts []string
	for _, e := range transcriptEvents([]*session.Event{a, b, c, d, summary, empty}) {
		for _, p := range e.Content.Parts {
			texts = append(texts, p.Text)
		}
	}
	assert.Equal(t, []string{"B", "D", "[Summary of earlier conversation]\n", "S2"}, texts)

	assert.False(t, hasUncompacted([]*session.Event{summary, empty}))
	assert.Nil(t, proseOnly(nil))
	assert.Nil(t, proseOnly(&genai.Content{Parts: []*genai.Part{{Text: "x", Thought: true}}}))
	tool := ev("t", "user", 8, "")
	tool.Content = &genai.Content{Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{Name: "x"}}}}
	assert.False(t, isUserTurnStart(tool), "a tool response isn't a prompt")
}

// Keeping fewer than one turn keeps one, and a summarizer that only thinks
// leaves the history alone.
func TestCompactKeepsAtLeastOneTurnAndNeedsProse(t *testing.T) {
	thinking := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{Text: "pondering", Thought: true}}}
	f := newEngineWith(t, fixtureOpts{}, textContent("r1"), textContent("r2"), thinking, textContent("SUMMARY"))
	runTurns(t, f.eng, "s", "q1", "q2")
	_, err := f.eng.Compact(context.Background(), "s", "", 0)
	require.Error(t, err, "a summary of thoughts only")
	res, err := f.eng.Compact(context.Background(), "s", "", 0)
	require.NoError(t, err)
	assert.Equal(t, 1, res.TurnsKept)
}
