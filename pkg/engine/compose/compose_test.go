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

package compose

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scripted answers each ask with the next reply, recording the prompts.
type scripted struct {
	replies []string
	prompts []string
}

func (s *scripted) ask(_ context.Context, instruction, prompt string) (string, float64, error) {
	s.prompts = append(s.prompts, prompt)
	if len(s.replies) == 0 {
		return "", 0, errors.New("no more replies")
	}
	r := s.replies[0]
	s.replies = s.replies[1:]
	return r, 0.001, nil
}

// A recipe whose drafts load when their file says "ok".
var testRecipe = Recipe{Kind: "thing", Brief: "A thing is a file that says ok.", Check: func(name, file string) []string {
	if strings.Contains(file, "ok") {
		return nil
	}
	return []string{"it doesn't say ok"}
}}

func TestQuestionThenDraft(t *testing.T) {
	var st Store
	s := st.New("thing", "/w", "", "make me a thing")
	m := &scripted{replies: []string{
		`{"question": "Which colour?", "options": ["red", "blue"]}`,
		"```json\n{\"draft\": {\"name\": \"blue-thing\", \"summary\": \"A blue thing.\", \"file\": \"ok blue\"}}\n```",
	}}
	require.NoError(t, Step(context.Background(), testRecipe, "nothing yet", s, m.ask))
	assert.True(t, s.Asking())
	assert.Equal(t, Turn{Role: "composer", Text: "Which colour?", Options: []string{"red", "blue"}}, s.Turns[1])
	assert.Contains(t, m.prompts[0], "make me a thing")
	assert.Contains(t, m.prompts[0], "nothing yet")
	assert.Contains(t, m.prompts[0], "5 more question(s)")

	Answer(s, " blue ")
	require.NoError(t, Step(context.Background(), testRecipe, "", s, m.ask))
	require.NotNil(t, s.Draft)
	assert.False(t, s.Asking())
	assert.Equal(t, "blue-thing", s.Draft.Name)
	assert.Equal(t, "ok blue", s.Draft.File)
	assert.Empty(t, s.Draft.Problems)
	assert.Equal(t, "A blue thing.", s.Turns[len(s.Turns)-1].Text)
	assert.InDelta(t, 0.002, s.CostUSD, 1e-9)
	assert.Contains(t, m.prompts[1], "user: blue")

	// A change asked of the draft starts a new one.
	Answer(s, "make it red")
	assert.Nil(t, s.Draft)
}

// A draft that doesn't load goes back with its problems; one still wrong
// after two fixes is shown with them.
func TestDraftFixes(t *testing.T) {
	var st Store
	fixed := st.New("thing", "/w", "", "a thing")
	m := &scripted{replies: []string{`{"draft": {"name": "t", "summary": "s", "file": "bad"}}`, `{"draft": {"name": "t", "summary": "s", "file": "ok now"}}`}}
	require.NoError(t, Step(context.Background(), testRecipe, "", fixed, m.ask))
	assert.Equal(t, "ok now", fixed.Draft.File)
	assert.Empty(t, fixed.Draft.Problems)
	assert.Contains(t, m.prompts[1], "it doesn't say ok", "the problems went back")

	stuck := st.New("thing", "/w", "", "a thing")
	bad := `{"draft": {"name": "t", "summary": "s", "file": "bad"}}`
	m = &scripted{replies: []string{bad, bad, bad, bad}}
	require.NoError(t, Step(context.Background(), testRecipe, "", stuck, m.ask))
	assert.Len(t, m.prompts, 3, "the draft and two fixes")
	assert.Equal(t, []string{"it doesn't say ok"}, stuck.Draft.Problems)

	// A fix answered with a question, or nonsense: the draft as it was.
	asked := st.New("thing", "/w", "", "a thing")
	m = &scripted{replies: []string{bad, `{"question": "really?"}`}}
	require.NoError(t, Step(context.Background(), testRecipe, "", asked, m.ask))
	assert.Equal(t, "bad", asked.Draft.File)
	m = &scripted{replies: []string{bad, "nonsense"}}
	nonsense := st.New("thing", "/w", "", "a thing")
	require.NoError(t, Step(context.Background(), testRecipe, "", nonsense, m.ask))
	assert.Equal(t, "bad", nonsense.Draft.File)
}

func TestBadRepliesAndLimits(t *testing.T) {
	var st Store
	for _, r := range []string{"nonsense", `{"question": ""}`, `{"draft": {"file": "  "}}`} {
		s := st.New("thing", "/w", "", "x")
		// Unreadable twice: asked again once, then given up on.
		m := &scripted{replies: []string{r, r}}
		err := Step(context.Background(), testRecipe, "", s, m.ask)
		assert.ErrorIs(t, err, ErrNoReply, r)
		assert.Len(t, m.prompts, 2, r)
		assert.Contains(t, m.prompts[1], "couldn't be read", r)
	}
	// Unreadable once, then a question: the question.
	s0 := st.New("thing", "/w", "", "x")
	require.NoError(t, Step(context.Background(), testRecipe, "", s0, (&scripted{replies: []string{"nonsense", `{"question": "q"}`}}).ask))
	assert.True(t, s0.Asking())
	s := st.New("thing", "/w", "", "x")
	err := Step(context.Background(), testRecipe, "", s, (&scripted{}).ask)
	assert.EqualError(t, err, "no more replies")

	rep, err := parseReply(`{"question": "q", "options": ["1","2","3","4","5","6","7"]}`)
	require.NoError(t, err)
	assert.Len(t, rep.Options, 6)

	full := &Session{Kind: "thing"}
	for range MaxQuestions {
		full.Turns = append(full.Turns, Turn{Role: "composer", Text: "q"}, Turn{Role: "user", Text: "a"})
	}
	assert.Contains(t, Prompt(testRecipe, "", full, nil), "draft it now")
	edit := &Session{Kind: "thing", Current: "the old file"}
	assert.Contains(t, Prompt(testRecipe, "", edit, nil), "the old file")
}

func TestParseReplyFormats(t *testing.T) {
	yaml := "name: a\nsteps:\n  - id: x # {not json}\n    prompt: \"say \\\"hi\\\".\"\n"
	for _, tc := range []struct {
		name, text, file, question string
	}{
		{name: "header then fence", text: "{\"draft\": {\"name\": \"a\", \"summary\": \"s\"}}\n```file\n" + yaml + "```", file: yaml},
		{name: "prose around", text: "Here it is:\n{\"draft\": {\"name\": \"a\", \"summary\": \"s {x}\"}}\n\n```yaml\n" + yaml + "```\nDone.", file: yaml},
		{name: "file in the JSON", text: `{"draft": {"name": "a", "summary": "s", "file": "inline"}}`, file: "inline"},
		{name: "fenced JSON", text: "```json\n{\"question\": \"q?\"}\n```", question: "q?"},
		{name: "question with braces after", text: `{"question": "q?"} and {more}`, question: "q?"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rep, err := parseReply(tc.text)
			require.NoError(t, err)
			if tc.question != "" {
				assert.Equal(t, tc.question, rep.Question)
				return
			}
			require.NotNil(t, rep.Draft)
			assert.Equal(t, "a", rep.Draft.Name)
			assert.Equal(t, strings.TrimSpace(tc.file), rep.Draft.File)
		})
	}
	for _, bad := range []string{"", "```\nunclosed", "{\"draft\": ", "```file```"} {
		_, err := parseReply(bad)
		assert.ErrorIs(t, err, ErrNoReply, bad)
	}
}

func TestStore(t *testing.T) {
	var st Store
	s := st.New("thing", "/w", "", "")
	assert.Empty(t, s.Turns, "no request, no turn")
	got, err := st.Get("/w", s.ID)
	require.NoError(t, err)
	assert.Same(t, s, got)
	_, err = st.Get("/other", s.ID)
	assert.ErrorIs(t, err, ErrNoSession, "another workspace's")
	st.Drop("/other", s.ID)
	_, err = st.Get("/w", s.ID)
	assert.NoError(t, err, "not dropped by another workspace")
	st.Drop("/w", s.ID)
	_, err = st.Get("/w", s.ID)
	assert.ErrorIs(t, err, ErrNoSession)

	old := st.New("thing", "/w", "", "x")
	old.Updated = time.Now().Add(-Keep - time.Minute)
	_, err = st.Get("/w", old.ID)
	assert.ErrorIs(t, err, ErrNoSession, "ended")

	live := st.New("thing", "/w", "", "x")
	live.Draft = &Draft{Name: "n", Problems: []string{"p"}}
	snap := live.Snapshot()
	snap.Turns[0].Text = "changed"
	snap.Draft.Problems[0] = "changed"
	assert.Equal(t, "x", live.Turns[0].Text, "a copy")
	assert.Equal(t, "p", live.Draft.Problems[0])
	live.Lock()
	live.Turns = append(live.Turns, Turn{Role: "user", Text: "held"})
	live.Unlock()
	assert.Len(t, live.Turns, 2)
}
