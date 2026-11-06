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

package session

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

var t0 = time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)

// said is a raw event with text, at second sec.
func said(id string, sec int) *adksession.Event {
	ev := &adksession.Event{ID: id, InvocationID: "inv-" + id, Timestamp: t0.Add(time.Duration(sec) * time.Second), Author: "user"}
	ev.LLMResponse = model.LLMResponse{Content: genai.NewContentFromText(id, genai.RoleUser)}
	return ev
}

// summary is a compaction record over seconds from..to, written at sec.
func summary(id string, sec, from, to int, holes ...*adksession.Event) *adksession.Event {
	c := &adksession.EventCompaction{
		StartTimestamp:   t0.Add(time.Duration(from) * time.Second),
		EndTimestamp:     t0.Add(time.Duration(to) * time.Second),
		CompactedContent: genai.NewContentFromText("summary "+id, genai.RoleModel),
	}
	for _, h := range holes {
		c.ExcludedEvents = append(c.ExcludedEvents, adksession.EventRef{InvocationID: h.InvocationID, Timestamp: h.Timestamp})
	}
	return &adksession.Event{ID: id, InvocationID: "inv-" + id, Timestamp: t0.Add(time.Duration(sec) * time.Second), Author: "user", Actions: adksession.EventActions{Compaction: c}}
}

func ids(events []*adksession.Event) []string {
	out := make([]string, len(events))
	for i, ev := range events {
		out[i] = ev.ID
	}
	return out
}

func TestWithoutSummarised(t *testing.T) {
	a, b, c, d := said("a", 1), said("b", 2), said("c", 3), said("d", 5)
	state := said("state", 2)
	state.Actions.StateDelta = map[string]any{"k": "v"}
	branch := said("branch", 2)
	branch.Branch = "sub"
	call := said("call", 2)
	call.LLMResponse.Content = &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{ID: "lr", Name: "wait"}}}}
	call.LongRunningToolIDs = []string{"lr"}
	answer := said("answer", 2)
	answer.LLMResponse.Content = &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: "lr", Name: "wait"}}}}
	empty := summary("empty", 4, 1, 3)
	empty.Actions.Compaction.CompactedContent = nil
	cases := []struct {
		name   string
		events []*adksession.Event
		want   []string
	}{
		{"no summary: all kept", []*adksession.Event{a, b, c}, []string{"a", "b", "c"}},
		{"covered: the first kept where the summary goes", []*adksession.Event{a, b, c, summary("s", 4, 1, 3), d}, []string{"a", "s", "d"}},
		{"a hole is kept", []*adksession.Event{a, b, c, summary("s", 4, 1, 3, b), d}, []string{"a", "b", "s", "d"}},
		{"what changed state is kept", []*adksession.Event{a, state, c, summary("s", 4, 1, 3)}, []string{"a", "state", "s"}},
		{"another branch is kept", []*adksession.Event{a, branch, c, summary("s", 4, 1, 3)}, []string{"a", "branch", "s"}},
		{"a long-running call and its answer are kept", []*adksession.Event{a, call, answer, c, summary("s", 4, 1, 3)}, []string{"a", "call", "answer", "s"}},
		{"a summary without content covers nothing", []*adksession.Event{a, b, empty}, []string{"a", "b", "empty"}},
		{"an inverted range covers nothing", []*adksession.Event{a, b, summary("s", 4, 3, 1)}, []string{"a", "b", "s"}},
		{"after a summary over its time: kept", []*adksession.Event{a, summary("s", 4, 1, 3), b, summary("t", 6, 1, 3)}, []string{"a", "s", "b", "t"}},
		{"a rolled summary: one first event for both", []*adksession.Event{a, b, summary("s", 3, 1, 2), c, d, summary("t", 6, 1, 5)}, []string{"a", "s", "t"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ids(withoutSummarised(tc.events)))
		})
	}
}

func TestPersistentServiceTrimsSummarised(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	svc, err := NewPersistentService(dir)
	require.NoError(t, err)
	created, err := svc.Create(ctx, &adksession.CreateRequest{AppName: "app", UserID: "u", SessionID: "s"})
	require.NoError(t, err)
	unpin := svc.Pin("s")
	for _, ev := range []*adksession.Event{said("a", 1), said("b", 2), said("c", 3), summary("s1", 4, 1, 3), said("d", 5)} {
		require.NoError(t, svc.AppendEvent(ctx, created.Session, ev))
	}
	assert.Equal(t, 0, svc.Hidden("s"), "not while a turn runs in it")
	assert.Equal(t, []string{"a", "b", "c", "d"}, eventTexts(t, svc, "s"))
	unpin()
	assert.Equal(t, 2, svc.Hidden("s"), "b and c: the summary stands in for them")
	assert.Equal(t, []string{"a", "d"}, eventTexts(t, svc, "s"), "a stays where the summary goes")
	log, err := svc.Log("s")
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b", "c", "s1", "d"}, ids(log), "the file has every event")

	// Loaded again (a new process): trimmed as it loads.
	svc2, err := NewPersistentService(dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "d"}, eventTexts(t, svc2, "s"))
	assert.Equal(t, 2, svc2.Hidden("s"))

	// Rewound before the summary: all of it back.
	require.NoError(t, svc2.Truncate(ctx, "app", "u", "s", 3))
	assert.Equal(t, []string{"a", "b", "c"}, eventTexts(t, svc2, "s"))
	assert.Equal(t, 0, svc2.Hidden("s"))

	require.NoError(t, svc.Delete(ctx, &adksession.DeleteRequest{AppName: "app", UserID: "u", SessionID: "s"}))
	assert.Equal(t, 0, svc.Hidden("s"))
	log, err = svc.Log("s")
	require.NoError(t, err)
	assert.Empty(t, log)
}

func TestPersistentServiceTrimOnlyWhatsLoaded(t *testing.T) {
	ctx := context.Background()
	svc, err := NewPersistentService(t.TempDir())
	require.NoError(t, err)
	created, err := svc.Create(ctx, &adksession.CreateRequest{AppName: "app", UserID: "u", SessionID: "s"})
	require.NoError(t, err)
	for _, ev := range []*adksession.Event{said("a", 1), said("b", 2), summary("s1", 3, 1, 2)} {
		require.NoError(t, svc.AppendEvent(ctx, created.Session, ev))
	}
	// A session that left memory isn't trimmed (it loads trimmed).
	svc.forget(sessionKey{"app", "u", "s"})
	svc.trim(ctx, "s")
	assert.Equal(t, 0, svc.Hidden("s"))
	// Nothing to leave out: trimming changes nothing.
	svc.touch(ctx, sessionKey{"app", "u", "s"})
	svc.summary["s"] = true
	require.NoError(t, svc.inner.Delete(ctx, &adksession.DeleteRequest{AppName: "app", UserID: "u", SessionID: "s"}))
	svc.trim(ctx, "s")
	assert.Equal(t, 0, svc.Hidden("s"), "gone from memory: nothing trimmed")
}
