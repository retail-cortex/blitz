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
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func TestParseVerdict(t *testing.T) {
	cases := []struct {
		name    string
		answer  string
		allowed bool
		reason  string
		bad     bool
	}{
		{name: "allow", answer: `{"decision": "allow", "reason": "it's the task"}`, allowed: true, reason: "it's the task"},
		{name: "deny", answer: `{"decision":"deny","reason":"deletes the home directory"}`, reason: "deletes the home directory"},
		{name: "in a code fence", answer: "```json\n{\"decision\": \"Allow\", \"reason\": \"ok\"}\n```", allowed: true, reason: "ok"},
		{name: "not JSON", answer: "sure, go ahead", bad: true},
		{name: "another word", answer: `{"decision": "maybe"}`, bad: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			allowed, reason, err := parseVerdict(c.answer)
			if c.bad {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.allowed, allowed)
			assert.Equal(t, c.reason, reason)
		})
	}
}

// The reviewer sees the request and the environment, and its tokens count
// for the session.
func TestReviewAsksTheModel(t *testing.T) {
	reviewer := NewMockLLM("reviewer", genai.NewContentFromText(`{"decision":"deny","reason":"outside the task"}`, genai.RoleModel))
	reviewer.Usage = &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 200, CandidatesTokenCount: 12}
	f := newEngineWith(t, fixtureOpts{
		cfg:  func(c *config.Config) { c.Permissions.Auto.Environment = "Pushing to github.com/acme is fine." },
		opts: []Option{WithReviewModel(reviewer)},
	})
	ctx := context.WithValue(context.Background(), runStateKey{}, &runState{sessionID: "s"})
	allowed, reason, err := f.eng.review(ctx, api.ApprovalRequest{Tool: "run_shell_command", Kind: api.ActionCommand, Detail: "rm -rf ~/src"})
	require.NoError(t, err)
	assert.False(t, allowed)
	assert.Equal(t, "outside the task", reason)
	require.Len(t, reviewer.Requests, 1)
	sent := reviewer.Requests[0].Contents[0].Parts[0].Text
	assert.Contains(t, sent, "rm -rf ~/src")
	assert.Contains(t, sent, "Pushing to github.com/acme is fine.")
	assert.Equal(t, 1, f.eng.Usage("s").Calls, "the review isn't in the session's usage")
}

// The reviewer sees the action's targets, its diff and the session's recent
// conversation, and a failing or silent model is an error, not a verdict.
func TestReviewPromptAndFailures(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{}, textContent("first answer"))
	_, err := collect(t, f.eng, "s", "please tidy the README")
	require.NoError(t, err)
	ctx := context.WithValue(context.Background(), runStateKey{}, &runState{sessionID: "s"})
	req := api.ApprovalRequest{Tool: "write_file", Kind: api.ActionWrite, Detail: "edit", Targets: []string{"a.go", "b.go"}, Diff: "+hello"}

	t.Run("prompt", func(t *testing.T) {
		reviewer := NewMockLLM("reviewer", textContent(`{"decision":"allow","reason":"fine"}`))
		f.eng.reviewModel = reviewer
		allowed, _, err := f.eng.review(ctx, req)
		require.NoError(t, err)
		assert.True(t, allowed)
		sent := reviewer.Requests[0].Contents[0].Parts[0].Text
		assert.Contains(t, sent, "a.go, b.go")
		assert.Contains(t, sent, "+hello")
		assert.Contains(t, sent, "user: please tidy the README", "the recent conversation is missing")
		assert.Contains(t, sent, "agent: first answer", "the recent conversation is missing")
		assert.Contains(t, sent, "(none given)", "no environment was described")
	})
	t.Run("model error", func(t *testing.T) {
		f.eng.reviewModel = NewUnavailableModel("down", "offline")
		_, _, err := f.eng.review(ctx, req)
		assert.ErrorIs(t, err, ErrModelUnavailable)
	})
	t.Run("no model", func(t *testing.T) {
		f.eng.reviewModel = nil
		llm := f.eng.llm
		f.eng.llm = nil
		defer func() { f.eng.llm = llm }()
		_, _, err := f.eng.review(ctx, req)
		assert.Error(t, err)
	})
	t.Run("bad JSON", func(t *testing.T) {
		f.eng.reviewModel = NewMockLLM("reviewer", textContent(`{"decision": allow}`))
		_, _, err := f.eng.review(ctx, req)
		assert.Error(t, err)
	})
}

// askModel skips thoughts, prices the call under the model that served it,
// and without a run's session counts it as "default".
func TestAskModelUsage(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{})
	m := NewMockLLM("asked", &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{Text: "hmm", Thought: true}, {Text: "answer"}}})
	m.Usage = &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 10, CandidatesTokenCount: 2}
	m.ServedBy = "asked-001"
	text, err := f.eng.askModel(context.Background(), m, "instr", "prompt")
	require.NoError(t, err)
	assert.Equal(t, "answer", text)
	assert.Equal(t, 1, f.eng.Usage("default").Calls, "a call outside a run counts for the default session")
}

// Prompt hooks are judged by the hook's own model, else the reviewer, else
// the session's model, and must answer JSON.
func TestJudgeHook(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{}, textContent(`ok {"decision":"allow"} done`))
	ctx := context.Background()

	got, err := f.eng.judgeHook(ctx, "none", "no secrets", []byte(`{"tool":"x"}`))
	require.NoError(t, err)
	assert.Equal(t, `{"decision":"allow"}`, got, "the session's model judges when nothing else is set")

	hook := NewMockLLM("hook", textContent(`{"decision":"deny"}`))
	WithHookModel("fast", hook)(f.eng)
	WithReviewModel(NewMockLLM("reviewer", textContent(`{"decision":"ask"}`)))(f.eng)
	got, err = f.eng.judgeHook(ctx, "fast", "c", nil)
	require.NoError(t, err)
	assert.Equal(t, `{"decision":"deny"}`, got)
	assert.Equal(t, 1, hook.Calls())
	got, err = f.eng.judgeHook(ctx, "other", "c", nil)
	require.NoError(t, err)
	assert.Equal(t, `{"decision":"ask"}`, got, "the reviewer judges hooks without a model of their own")

	WithHookModel("words", NewMockLLM("w", textContent("sure")))(f.eng)
	_, err = f.eng.judgeHook(ctx, "words", "c", nil)
	assert.Error(t, err, "an answer without JSON")
	WithHookModel("down", NewUnavailableModel("down", "offline"))(f.eng)
	_, err = f.eng.judgeHook(ctx, "down", "c", nil)
	assert.ErrorIs(t, err, ErrModelUnavailable)

	f.eng.reviewModel, f.eng.llm = nil, nil
	_, err = f.eng.judgeHook(ctx, "none", "c", nil)
	assert.Error(t, err, "no model at all")
}

// JudgeGoal reads the judge's verdict about the session's conversation.
func TestJudgeGoal(t *testing.T) {
	cases := []struct {
		name   string
		answer string
		want   GoalVerdict
		bad    bool
	}{
		{name: "met", answer: `{"verdict":"met","reason":" tests pass "}`, want: GoalVerdict{Met: true, Reason: "tests pass"}},
		{name: "unmet", answer: "```json\n{\"verdict\":\"Unmet\",\"reason\":\"r\",\"next\":\"run the tests\"}\n```", want: GoalVerdict{Reason: "r", Next: "run the tests"}},
		{name: "impossible", answer: `{"verdict":"impossible"}`, want: GoalVerdict{Impossible: true}},
		{name: "other word", answer: `{"verdict":"maybe"}`, bad: true},
		{name: "not JSON", answer: `no idea`, bad: true},
		{name: "broken JSON", answer: `{"verdict":}`, bad: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newEngineWith(t, fixtureOpts{}, textContent("hello back"))
			_, err := collect(t, f.eng, "s", "make the tests pass")
			require.NoError(t, err)
			judge := NewMockLLM("judge", textContent(c.answer))
			f.eng.reviewModel = judge
			got, err := f.eng.JudgeGoal(context.Background(), "s", "the tests pass")
			if c.bad {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
			assert.Contains(t, judge.Requests[0].Contents[0].Parts[0].Text, "user: make the tests pass")
		})
	}
	t.Run("failures", func(t *testing.T) {
		f := newEngineWith(t, fixtureOpts{})
		f.eng.reviewModel = NewUnavailableModel("down", "offline")
		_, err := f.eng.JudgeGoal(context.Background(), "s", "x")
		assert.ErrorIs(t, err, ErrModelUnavailable)
		f.eng.reviewModel, f.eng.llm = nil, nil
		_, err = f.eng.JudgeGoal(context.Background(), "s", "x")
		assert.Error(t, err)
	})
}
