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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/memory"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
)

// waitSummaries waits for summaries being written in the background.
func waitSummaries(t *testing.T, w *Workspace) {
	t.Helper()
	require.Eventually(t, func() bool {
		w.hookCtxMu.Lock()
		defer w.hookCtxMu.Unlock()
		return len(w.summaryQueued) == 0
	}, 10*time.Second, 10*time.Millisecond)
}

// requestText is everything a model request carried, as text.
func requestText(req *model.LLMRequest) string {
	var b strings.Builder
	for _, c := range req.Contents {
		for _, p := range c.Parts {
			b.WriteString(p.Text)
			if p.FunctionResponse != nil {
				for _, v := range p.FunctionResponse.Response {
					if s, ok := v.(string); ok {
						b.WriteString(s)
					}
				}
			}
		}
	}
	return b.String()
}

// latestPrompt is the text of a model request's latest user message.
func latestPrompt(req *model.LLMRequest) string {
	for i := len(req.Contents) - 1; i >= 0; i-- {
		if c := req.Contents[i]; c.Role == "user" && len(c.Parts) > 0 && c.Parts[0].Text != "" {
			return c.Parts[0].Text
		}
	}
	return ""
}

// Moving on from a session writes where it left off (secrets masked); the
// next session's first prompt lists it, recall_session reads it, and a
// session that went on is summarised again from the summary so far.
func TestSessionSummaries(t *testing.T) {
	const key = "sk-test-0123456789abcdefghij"
	w, llm := openTestWith(t, func(c *config.Config) { c.LLM.OpenAI.APIKey = key },
		text("rounded"), text("tested"), // the first session
		text("hello"), toolCall("recall_session", map[string]any{}), text("picking up"), // the second
		text("pushed"), // the first again
	)
	ctx := context.Background()
	small := runtime.NewMockLLM("small", text("Coupon rounding fixed and tested, key "+key+".\n\n## Next\nPush the branch."), text("Pushed.\n\n## Next\nTag it."))
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return small, nil }
	asked := func(i int) string { return requestText(small.Requests[i]) }
	_, err := w.recallSession(ctx, "", "")
	assert.ErrorIs(t, err, errNoEarlierSession)

	first, err := w.NewSession()
	require.NoError(t, err)
	for _, p := range []string{"fix the coupon rounding", "and the tests"} {
		_, err = w.Run(ctx, first.ID, api.Turn{Text: p}, func(api.Event) {})
		require.NoError(t, err)
	}
	second, err := w.NewSession()
	require.NoError(t, err)
	waitSummaries(t, w)
	require.Equal(t, 1, small.Calls(), "written once the person moved on")
	assert.Contains(t, asked(0), "User: fix the coupon rounding")
	assert.Contains(t, asked(0), "Agent: tested")
	s, err := memory.FindSummary(w.summariesDir(), first.ID)
	require.NoError(t, err)
	assert.NotContains(t, s.Text, key, "secrets are never kept")
	assert.Equal(t, 4, s.Messages)

	_, err = w.Run(ctx, second.ID, api.Turn{Text: "hi"}, func(api.Event) {})
	require.NoError(t, err)
	got := latestPrompt(llm.Requests[len(llm.Requests)-1])
	assert.Contains(t, got, "<earlier-sessions-hook-context>")
	assert.Contains(t, got, "("+first.ID+"): Coupon rounding fixed and tested")

	_, err = w.Run(ctx, second.ID, api.Turn{Text: "pick up where we left off"}, func(api.Event) {})
	require.NoError(t, err)
	assert.NotContains(t, latestPrompt(llm.Requests[len(llm.Requests)-2]), "earlier-sessions", "only the first prompt")
	assert.Contains(t, requestText(llm.Requests[len(llm.Requests)-1]), "Push the branch.", "the tool read it")
	assert.Equal(t, 1, small.Calls(), "an up-to-date summary is read, not written again")

	out, err := w.recallSession(ctx, second.ID, second.ID)
	assert.ErrorContains(t, err, "this session")
	assert.Empty(t, out)
	_, err = w.recallSession(ctx, second.ID, "session-nope")
	assert.ErrorContains(t, err, "no session")

	_, _, err = w.LoadSession(first.ID)
	require.NoError(t, err)
	_, err = w.Run(ctx, first.ID, api.Turn{Text: "push it"}, func(api.Event) {})
	require.NoError(t, err)
	waitSummaries(t, w)
	out, err = w.recallSession(ctx, second.ID, first.ID)
	require.NoError(t, err)
	require.Equal(t, 3, small.Calls(), "the second once left, the first again: it was behind")
	assert.Contains(t, asked(1), "User: pick up where we left off")
	assert.Contains(t, asked(2), "The summary so far:\nCoupon rounding fixed")
	assert.Contains(t, asked(2), "User: push it")
	assert.NotContains(t, asked(2), "User: fix the coupon rounding", "only what came since")
	assert.Equal(t, first.ID, out.Session)
	s, _ = memory.FindSummary(w.summariesDir(), first.ID)
	assert.Equal(t, 6, s.Messages)
}

// memory.summaries = false (or memory.auto = false) keeps none, and
// recall_session says so.
func TestSessionSummariesOff(t *testing.T) {
	off := false
	w, llm := openTestWith(t, func(c *config.Config) { c.Memory.Summaries = &off },
		text("a"), text("b"), toolCall("recall_session", map[string]any{}), text("c"))
	ctx := context.Background()
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) {
		t.Error("no summary is written")
		return runtime.NewMockLLM("small"), nil
	}
	first, err := w.NewSession()
	require.NoError(t, err)
	for _, p := range []string{"one", "two"} {
		_, err = w.Run(ctx, first.ID, api.Turn{Text: p}, func(api.Event) {})
		require.NoError(t, err)
	}
	second, err := w.NewSession()
	require.NoError(t, err)
	waitSummaries(t, w)
	_, err = w.Run(ctx, second.ID, api.Turn{Text: "pick up where we left off"}, func(api.Event) {})
	require.NoError(t, err)
	assert.NotContains(t, latestPrompt(llm.Requests[len(llm.Requests)-2]), "earlier-sessions")
	assert.Contains(t, requestText(llm.Requests[len(llm.Requests)-1]), "session summaries are turned off")
}

// A conversation's first prompt says where the repository stands: the
// branch, the uncommitted files and the latest commits; later prompts don't.
func TestWorkspaceStateInFirstPrompt(t *testing.T) {
	w, llm := openTestWith(t, nil, text("a"), text("b"))
	ctx := context.Background()
	gitIn(t, w.Dir(), "init", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "app.tsx"), []byte("x"), 0o644))
	gitIn(t, w.Dir(), "add", "-A")
	gitIn(t, w.Dir(), "commit", "-qm", "Recipe list: cards with photos")
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "app.tsx"), []byte("y"), 0o644))

	s, err := w.NewSession()
	require.NoError(t, err)
	_, err = w.Run(ctx, s.ID, api.Turn{Text: "where did we leave off?"}, func(api.Event) {})
	require.NoError(t, err)
	got := latestPrompt(llm.Requests[len(llm.Requests)-1])
	assert.Contains(t, got, "<workspace-state-hook-context>")
	assert.Contains(t, got, "- Branch main\n")
	assert.Contains(t, got, "- 1 uncommitted:\n   M app.tsx")
	assert.Contains(t, got, "Recipe list: cards with photos (")
	assert.Contains(t, got, "never ask the user to run them")

	_, err = w.Run(ctx, s.ID, api.Turn{Text: "go on"}, func(api.Event) {})
	require.NoError(t, err)
	assert.NotContains(t, latestPrompt(llm.Requests[len(llm.Requests)-1]), "workspace-state")
}

// A summary that can't be written is tried again at the next first prompt,
// which lists the session without one; one that's behind is brought up to
// date from itself and what came since, the transcript cut to its latest.
func TestSessionSummaryRetriesAndCatchesUp(t *testing.T) {
	w, llm := openTestWith(t, nil, text("a"), text("b"), text("c"), text("d"))
	ctx := context.Background()
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return nil, errors.New("no key") }
	long := strings.Repeat("the recipe cards need photos ", 2000) // over maxSummaryInput
	first, err := w.NewSession()
	require.NoError(t, err)
	for _, p := range []string{"fix the cards", long} {
		_, err = w.Run(ctx, first.ID, api.Turn{Text: p}, func(api.Event) {})
		require.NoError(t, err)
	}
	second, err := w.NewSession()
	require.NoError(t, err)
	waitSummaries(t, w)
	_, err = memory.FindSummary(w.summariesDir(), first.ID)
	assert.ErrorIs(t, err, os.ErrNotExist, "the model failed: none written")

	_, err = w.Run(ctx, second.ID, api.Turn{Text: "hi"}, func(api.Event) {})
	require.NoError(t, err)
	got := latestPrompt(llm.Requests[len(llm.Requests)-1])
	assert.Contains(t, got, "("+first.ID+")\n", "listed without a summary")
	waitSummaries(t, w)

	// Written for its first two messages only: behind.
	_, err = memory.SaveSummary(w.summariesDir(), memory.Summary{Session: first.ID, Title: first.Title, Messages: 2, Updated: time.Now(), Text: "Cards half done."})
	require.NoError(t, err)
	small := runtime.NewMockLLM("small", text("Cards done.\n\n## Next\nPhotos."))
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return small, nil }
	third, err := w.NewSession()
	require.NoError(t, err)
	_, err = w.Run(ctx, third.ID, api.Turn{Text: "where were we?"}, func(api.Event) {})
	require.NoError(t, err)
	waitSummaries(t, w)
	require.Equal(t, 1, small.Calls())
	in := requestText(small.Requests[0])
	assert.Contains(t, in, "The summary so far:\nCards half done.")
	assert.Contains(t, in, "The conversation since:\n…\n", "cut to the latest")
	assert.Less(t, len(in), maxSummaryInput+1000)
	s, err := memory.FindSummary(w.summariesDir(), first.ID)
	require.NoError(t, err)
	assert.Equal(t, 4, s.Messages)
	assert.Equal(t, "Cards done.\n\n## Next\nPhotos.", s.Text)
}

// The state names the upstream, and only the first ten changed files.
func TestWorkspaceStateUpstreamAndManyFiles(t *testing.T) {
	w, _ := openTestWith(t, nil)
	ctx := context.Background()
	remote := t.TempDir()
	gitIn(t, remote, "init", "-q", "--bare")
	gitIn(t, w.Dir(), "init", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "a.txt"), []byte("a"), 0o644))
	gitIn(t, w.Dir(), "add", "-A")
	gitIn(t, w.Dir(), "commit", "-qm", "first")
	gitIn(t, w.Dir(), "remote", "add", "origin", remote)
	gitIn(t, w.Dir(), "push", "-q", "-u", "origin", "main")
	for i := range 12 {
		require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), fmt.Sprintf("new%02d.txt", i)), []byte("x"), 0o644))
	}
	got := w.workspaceState(ctx)
	assert.Contains(t, got, "- Branch main, 0 ahead and 0 behind origin/main\n")
	assert.Contains(t, got, "- 12 uncommitted:\n")
	assert.Contains(t, got, "  … and 2 more\n")
	assert.NotContains(t, got, "new11.txt")
}

// A summary recall_session can't write comes back as the tool's error.
func TestRecallSessionError(t *testing.T) {
	w, llm := openTestWith(t, nil, text("a"), text("b"), toolCall("recall_session", map[string]any{}), text("c"))
	ctx := context.Background()
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return nil, errors.New("no key for the small model") }
	first, err := w.NewSession()
	require.NoError(t, err)
	for _, p := range []string{"one", "two"} {
		_, err = w.Run(ctx, first.ID, api.Turn{Text: p}, func(api.Event) {})
		require.NoError(t, err)
	}
	second, err := w.NewSession()
	require.NoError(t, err)
	waitSummaries(t, w)
	_, err = w.Run(ctx, second.ID, api.Turn{Text: "pick up where we left off"}, func(api.Event) {})
	require.NoError(t, err)
	assert.Contains(t, requestText(llm.Requests[len(llm.Requests)-1]), "no key for the small model")
}
