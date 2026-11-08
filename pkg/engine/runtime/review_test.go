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
	"iter"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
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
	t.Run("too slow", func(t *testing.T) {
		old := reviewTimeout
		reviewTimeout = 20 * time.Millisecond
		defer func() { reviewTimeout = old }()
		f.eng.reviewModel = stalledModel{}
		_, _, err := f.eng.review(ctx, req)
		assert.ErrorContains(t, err, "didn't answer within 20ms", "the user decides")
	})
	t.Run("bad JSON", func(t *testing.T) {
		f.eng.reviewModel = NewMockLLM("reviewer", textContent(`{"decision": allow}`))
		_, _, err := f.eng.review(ctx, req)
		assert.Error(t, err)
	})
}

// The reviewer is told, as a fact, that a write's paths are inside the
// workspace, and a denial saying they're outside it is no verdict: the
// user decides (spec_005 APR-14c: a small local model denied
// .agents/scratch/… as "outside the workspace").
func TestReviewInsideTheWorkspace(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{})
	ctx := context.WithValue(context.Background(), runStateKey{}, &runState{sessionID: "s"})
	dir := f.eng.toolReg.Workspace().Dir()
	write := func(kind api.ActionKind, targets ...string) api.ApprovalRequest {
		return api.ApprovalRequest{Tool: "create_file", Kind: kind, Detail: "Create " + strings.Join(targets, ", "), Targets: targets}
	}
	cases := []struct {
		name    string
		req     api.ApprovalRequest
		answer  string
		fact    bool // the prompt says the paths are inside
		verdict bool // a verdict stands (else an error: the user decides)
		allowed bool
	}{
		{name: "false reason", req: write(api.ActionWrite, ".agents/scratch/notes.md"), answer: `{"decision":"deny","reason":"The action attempts to create a file outside the workspace."}`, fact: true},
		{name: "outside of the project", req: write(api.ActionDelete, "a.md", "b/c.md"), answer: `{"decision":"deny","reason":"It deletes files outside of the project"}`, fact: true},
		{name: "another reason stands", req: write(api.ActionWrite, "notes.md"), answer: `{"decision":"deny","reason":"It overwrites the user's work"}`, fact: true, verdict: true},
		{name: "an allow stands", req: write(api.ActionWrite, "notes.md"), answer: `{"decision":"allow","reason":"asked for"}`, fact: true, verdict: true, allowed: true},
		{name: "a path that climbs out", req: write(api.ActionWrite, "../elsewhere.md"), answer: `{"decision":"deny","reason":"outside the workspace"}`, verdict: true},
		{name: "an absolute path", req: write(api.ActionWrite, "/etc/hosts"), answer: `{"decision":"deny","reason":"outside the workspace"}`, verdict: true},
		{name: "not a file change", req: api.ApprovalRequest{Tool: "run_shell_command", Kind: api.ActionCommand, Detail: "ls", Targets: []string{"ls"}}, answer: `{"decision":"deny","reason":"outside the workspace"}`, verdict: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reviewer := NewMockLLM("reviewer", textContent(c.answer))
			f.eng.reviewModel = reviewer
			allowed, _, err := f.eng.review(ctx, c.req)
			sent := reviewer.Requests[0].Contents[0].Parts[0].Text
			if c.fact {
				assert.Contains(t, sent, "inside the workspace: yes")
				assert.Contains(t, sent, filepath.Join(dir, filepath.FromSlash(c.req.Targets[0])), "the absolute path")
			} else {
				assert.NotContains(t, sent, "inside the workspace: yes")
			}
			if c.verdict {
				require.NoError(t, err)
				assert.Equal(t, c.allowed, allowed)
			} else {
				assert.ErrorContains(t, err, "it's inside")
			}
		})
	}
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

// stalledModel answers nothing until its call is cancelled.
type stalledModel struct{}

func (stalledModel) Name() string { return "stalled" }

func (stalledModel) GenerateContent(ctx context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		<-ctx.Done()
		yield(nil, ctx.Err())
	}
}

// The user's answer to the agent's question is what they asked for, as
// much as their messages: ticking options in a form and approving them
// said "apply these" (2026-11-07: an edit the user chose was denied as
// "they asked to see options, not changes"). A plan's approval too.
func TestReviewSeesAnswers(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{}, textContent("Here are the fixes."))
	_, err := collect(t, f.eng, "s", "Can you display these as options?")
	require.NoError(t, err)
	got, err := f.eng.sessions.Get(context.Background(), &session.GetRequest{AppName: appName, UserID: "user", SessionID: "s"})
	require.NoError(t, err)
	add := func(id, author string, part *genai.Part) {
		e := &session.Event{ID: id, InvocationID: "inv-" + id, Author: author, Timestamp: time.Now()}
		e.LLMResponse.Content = &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{part}}
		require.NoError(t, f.eng.sessions.AppendEvent(context.Background(), got.Session, e))
	}
	add("q", "blitz", &genai.Part{FunctionCall: &genai.FunctionCall{Name: "ask_user_question", Args: map[string]any{"question": "Which fixes should I apply?", "multi_select": true, "options": []any{"Add .agents/scratch/ to .gitignore", "chmod +x the scripts"}}}})
	add("a", "user", &genai.Part{FunctionResponse: &genai.FunctionResponse{Name: "ask_user_question", Response: map[string]any{"answer": "Add .agents/scratch/ to .gitignore, chmod +x the scripts", "selected": []any{"Add .agents/scratch/ to .gitignore", "chmod +x the scripts"}}}})
	add("p", "user", &genai.Part{FunctionResponse: &genai.FunctionResponse{Name: "exit_plan_mode", Response: map[string]any{"approved": true}}})
	add("x", "user", &genai.Part{FunctionResponse: &genai.FunctionResponse{Name: "read_file", Response: map[string]any{"content": "secret stuff"}}})

	reviewer := NewMockLLM("reviewer", textContent(`{"decision":"allow","reason":"the user chose it"}`))
	f.eng.reviewModel = reviewer
	ctx := context.WithValue(context.Background(), runStateKey{}, &runState{sessionID: "s"})
	_, _, err = f.eng.review(ctx, api.ApprovalRequest{Tool: "replace_in_file", Kind: api.ActionWrite, Detail: "Edit .gitignore"})
	require.NoError(t, err)
	sent := reviewer.Requests[0].Contents[0].Parts[0].Text
	assert.Contains(t, sent, "user: Can you display these as options?")
	assert.Contains(t, sent, "agent asked the user: Which fixes should I apply?")
	assert.Contains(t, sent, "user answered: Add .agents/scratch/ to .gitignore, chmod +x the scripts")
	assert.Contains(t, sent, "user approved the agent's plan")
	assert.NotContains(t, sent, "secret stuff", "other tools' results aren't the user's words")
}

func TestAnsweredLine(t *testing.T) {
	call := func(args map[string]any) *genai.Part {
		return &genai.Part{FunctionCall: &genai.FunctionCall{Name: "ask_user_question", Args: args}}
	}
	resp := func(name string, r map[string]any) *genai.Part {
		return &genai.Part{FunctionResponse: &genai.FunctionResponse{Name: name, Response: r}}
	}
	for _, tc := range []struct {
		name string
		part *genai.Part
		want string
	}{
		{"several questions", call(map[string]any{"questions": []any{map[string]any{"question": "DB?"}, map[string]any{"question": "UI?"}}}), "agent asked the user: DB? / UI?"},
		{"no question", call(map[string]any{}), ""},
		{"several answers", resp("ask_user_question", map[string]any{"answers": []any{map[string]any{"question": "DB?", "answer": "PostgreSQL"}, map[string]any{"question": "UI?", "answer": ""}}}), "user answered: DB? → PostgreSQL"},
		{"no answer", resp("ask_user_question", map[string]any{"error": "no one can answer"}), ""},
		{"plan feedback", resp("exit_plan_mode", map[string]any{"approved": false, "feedback": "skip step 2"}), "user, on the agent's plan: skip step 2"},
		{"kept planning", resp("exit_plan_mode", map[string]any{"approved": false}), ""},
		{"text", &genai.Part{Text: "hi"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) { assert.Equal(t, tc.want, answeredLine(tc.part)) })
	}
}
