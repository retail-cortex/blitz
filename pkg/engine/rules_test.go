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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/gates"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// withJudge gives w a judge answering verdicts in turn ("pass", "fail:
// why"), and returns how many it gave.
func withJudge(t *testing.T, w *Workspace, verdicts ...string) *runtime.MockLLM {
	t.Helper()
	var replies []*genai.Content
	for _, v := range verdicts {
		verdict, reason, _ := strings.Cut(v, ":")
		replies = append(replies, text(`{"verdict": "`+verdict+`", "reason": "`+strings.TrimSpace(reason)+`"}`))
	}
	judge := runtime.NewMockLLM("judge", replies...)
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return judge, nil }
	return judge
}

func writeRule(t *testing.T, w *Workspace, name, content string) {
	t.Helper()
	dir := w.ProjectRulesDir()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".md"), []byte(content), 0o644))
}

// collect is an event sink keeping the notices.
func collect(notices *[]api.Notice) func(api.Event) {
	return func(e api.Event) {
		if e.Notice != nil && !e.Notice.Transient {
			*notices = append(*notices, *e.Notice)
		}
	}
}

// A failing before-agent block rule refuses the prompt, nothing recorded;
// a warn rule says so after the prompt is recorded; a revise rule's
// reason goes to the agent with the prompt.
func TestBeforeAgentRules(t *testing.T) {
	t.Run("block", func(t *testing.T) {
		w, llm := openTestWith(t, nil, text("never"))
		writeRule(t, w, "no-prod", "---\nwhen: before_agent\naction: block\n---\nNever touch production.")
		withJudge(t, w, "fail: it asks to deploy to production")
		s := newSession(t, w)
		_, err := w.Run(context.Background(), s.ID, api.Turn{Text: "deploy to prod"}, ignore)
		var blocked *api.BlockedError
		require.ErrorAs(t, err, &blocked)
		assert.Contains(t, blocked.Reason, "no-prod")
		assert.Contains(t, blocked.Reason, "deploy to production")
		assert.Zero(t, llm.Calls(), "the agent didn't run")
	})
	t.Run("warn and revise", func(t *testing.T) {
		w, llm := openTestWith(t, nil, text("done"))
		writeRule(t, w, "a-polite", "---\nwhen: before_agent\norder: 1\n---\nBe polite.")
		writeRule(t, w, "b-scope", "---\nwhen: before_agent\norder: 2\naction: revise\n---\nKeep changes small.")
		withJudge(t, w, "fail: it's curt", "fail: it asks for a rewrite")
		var notices []api.Notice
		s := newSession(t, w)
		_, err := w.Run(context.Background(), s.ID, api.Turn{Text: "rewrite everything"}, collect(&notices))
		require.NoError(t, err)
		require.Len(t, notices, 1)
		assert.Contains(t, notices[0].Text, "a-polite")
		assert.False(t, notices[0].Error)
		require.Equal(t, 1, llm.Calls())
		assert.Contains(t, lastPrompt(llm), `Rule "b-scope": it asks for a rewrite`)
	})
	t.Run("a judge that fails lets it through", func(t *testing.T) {
		w, llm := openTestWith(t, nil, text("done"))
		writeRule(t, w, "x", "---\nwhen: before_agent\naction: block\n---\nX.")
		w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return nil, errors.New("no model") }
		var notices []api.Notice
		_, err := w.Run(context.Background(), newSession(t, w).ID, api.Turn{Text: "go"}, collect(&notices))
		require.NoError(t, err)
		assert.Equal(t, 1, llm.Calls())
		require.Len(t, notices, 1)
		assert.True(t, notices[0].Error)
		assert.Contains(t, notices[0].Text, "couldn't be checked")
	})
}

// An after-agent revise rule sends the agent back with the reason until
// it passes (twice at most); a block rule says the answer fails it; a
// rule for another agent isn't judged.
func TestAfterAgentRules(t *testing.T) {
	w, llm := openTestWith(t, nil, text("First try."), text("Tested with go test."))
	writeRule(t, w, "tested", "---\nwhen: after_agent\naction: revise\n---\nSay how it was tested.")
	writeRule(t, w, "theirs", "---\nwhen: after_agent\naction: block\nagents: [someone-else]\n---\nX.")
	judge := withJudge(t, w, "fail: no tests named", "pass: names go test")
	s := newSession(t, w)
	res, err := w.Run(context.Background(), s.ID, api.Turn{Text: "fix it"}, ignore)
	require.NoError(t, err)
	assert.Equal(t, 2, llm.Calls(), "a second pass")
	assert.Equal(t, 2, judge.Calls(), "judged twice; the other agent's rule never")
	assert.Contains(t, lastPrompt(llm), `The rule "tested" isn't met: no tests named`)
	assert.Contains(t, res.Output, "Tested with go test.")
	got := strings.Join(transcript(t, w), "\n")
	assert.Contains(t, got, "user: (rules) The rule")

	w2, llm2 := openTestWith(t, nil, text("a"), text("b"), text("c"))
	writeRule(t, w2, "never", "---\nwhen: after_agent\naction: revise\n---\nNever pass.")
	writeRule(t, w2, "hard", "---\nwhen: after_agent\naction: block\n---\nHard.")
	withJudge(t, w2, "fail: no", "fail: hard", "fail: no", "fail: hard", "fail: no", "fail: hard")
	var notices []api.Notice
	_, err = w2.Run(context.Background(), newSession(t, w2).ID, api.Turn{Text: "go"}, collect(&notices))
	require.NoError(t, err)
	assert.Equal(t, 3, llm2.Calls(), "the first pass and two revisions")
	last := notices[len(notices)-1]
	assert.True(t, last.Error)
	assert.Contains(t, last.Text, "after 2 revisions")
	assert.Contains(t, notices[0].Text, "fails the rule hard")
}

// Studio's rule calls: save a gate and an instruction rule, read them
// back, reorder, judge one as written, delete.
func TestWorkspaceRules(t *testing.T) {
	w := openTest(t)
	_, probs, err := w.SaveRule(RuleSpec{Name: "b", When: gates.BeforeAgent, Action: gates.Warn, Text: "B."}, "")
	require.NoError(t, err)
	require.Empty(t, probs)
	_, probs, err = w.SaveRule(RuleSpec{Name: "a", When: gates.BeforeAgent, Action: gates.Block, Text: "A."}, "")
	require.NoError(t, err)
	require.Empty(t, probs)
	r, _, err := w.SaveRule(RuleSpec{Name: "style", Paths: []string{"*.go"}, Text: "Go style."}, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"*.go"}, r.Paths)
	_, probs, _ = w.SaveRule(RuleSpec{Name: "Bad"}, "")
	assert.NotEmpty(t, probs)

	require.NoError(t, w.ReorderRules([]string{"b", "a"}))
	var names []string
	for _, r := range w.Rules() {
		names = append(names, r.Name)
	}
	assert.Equal(t, []string{"b", "a", "style"}, names)

	withJudge(t, w, "fail: it's rude")
	v, err := w.JudgeRule(context.Background(), gates.Rule{Name: "x", When: gates.BeforeAgent, Text: "Be polite."}, "do it now")
	require.NoError(t, err)
	assert.Equal(t, gates.Verdict{Reason: "it's rude"}, v)

	require.NoError(t, w.DeleteRule("a"))
	assert.Error(t, w.DeleteRule("a"))
	assert.Len(t, w.Rules(), 2)
}

// lastPrompt is the text of the last request's last user message.
func lastPrompt(llm *runtime.MockLLM) string {
	if len(llm.Requests) == 0 {
		return ""
	}
	req := llm.Requests[len(llm.Requests)-1]
	for i := len(req.Contents) - 1; i >= 0; i-- {
		if c := req.Contents[i]; c.Role == "user" {
			var b strings.Builder
			for _, p := range c.Parts {
				b.WriteString(p.Text)
			}
			return b.String()
		}
	}
	return ""
}
