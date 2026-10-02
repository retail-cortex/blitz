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
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
)

// kinds lists the tiles' kinds, in order.
func kinds(s api.Suggestions) []api.SuggestionKind {
	var out []api.SuggestionKind
	for _, t := range s.Tiles {
		out = append(out, t.Kind)
	}
	return out
}

// waitIdeas waits for ideas being written in the background.
func waitIdeas(t *testing.T, w *Workspace) {
	t.Helper()
	require.Eventually(t, func() bool {
		w.ideasMu.Lock()
		defer w.ideasMu.Unlock()
		return !w.ideasRunning
	}, 10*time.Second, 10*time.Millisecond)
}

func TestHarnessMissing(t *testing.T) {
	for _, f := range harnessFiles {
		t.Run(f, func(t *testing.T) {
			w := openTest(t)
			require.True(t, w.HarnessMissing())
			assert.Equal(t, []api.SuggestionKind{api.SuggestSetup}, kinds(w.Suggestions(context.Background())), "setup is offered")
			path := filepath.Join(w.Dir(), filepath.FromSlash(f))
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
			require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))
			assert.False(t, w.HarnessMissing())
		})
	}
}

// State tiles come at once; ideas are written in the background from the
// recent conversations, kept, and written again only after they change.
func TestSuggestions(t *testing.T) {
	w, _ := openTestWith(t, nil, text("first answer"), text("second answer"))
	ctx := context.Background()
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "AGENTS.md"), []byte("x"), 0o644))
	var asked []string
	var reply = `Sure:
` + "```json" + `
[{"title": "Finish the coupon rounding fix", "prompt": "Finish fixing coupon rounding in cart."},
 {"title": "", "prompt": "dropped: no title"},
 {"title": "Add tests for discounts", "prompt": "Write tests for discount.go."}]
` + "```"
	w.newModel = func(_ context.Context, _ *config.Config, ref string) (model.LLM, error) {
		asked = append(asked, ref)
		return runtime.NewMockLLM("ideas", text(reply), text(reply)), nil
	}

	s := w.Suggestions(ctx)
	assert.Empty(t, s.Tiles, "nothing to go on yet")
	assert.False(t, s.Pending)
	assert.Empty(t, asked, "no conversations, no model call")

	sess, err := w.NewSession()
	require.NoError(t, err)
	_, err = w.Run(ctx, sess.ID, api.Turn{Text: "fix the coupon rounding"}, func(api.Event) {})
	require.NoError(t, err)

	s = w.Suggestions(ctx)
	require.Equal(t, []api.SuggestionKind{api.SuggestContinue}, kinds(s))
	assert.Equal(t, sess.ID, s.Tiles[0].SessionID)
	assert.True(t, s.Pending, "ideas are on their way")
	waitIdeas(t, w)
	s = w.Suggestions(ctx)
	assert.False(t, s.Pending)
	require.Equal(t, []api.SuggestionKind{api.SuggestContinue, api.SuggestIdea, api.SuggestIdea}, kinds(s))
	assert.Equal(t, "Finish the coupon rounding fix", s.Tiles[1].Title)
	assert.Equal(t, "Write tests for discount.go.", s.Tiles[2].Prompt)
	assert.Equal(t, []string{""}, asked, "once: the main model, no reviewer set")

	w.Suggestions(ctx)
	assert.Len(t, asked, 1, "kept until the conversations change")

	_, err = w.Run(ctx, sess.ID, api.Turn{Text: "and the tests"}, func(api.Event) {})
	require.NoError(t, err)
	assert.True(t, w.Suggestions(ctx).Pending, "a new message: written again")
	waitIdeas(t, w)
	assert.Len(t, asked, 2)

	w.cfg.Suggestions.Ideas = false
	assert.Equal(t, []api.SuggestionKind{api.SuggestContinue}, kinds(w.Suggestions(ctx)), "ideas off")
}

// A failed attempt isn't repeated for the same conversations until an hour
// has passed; the model is [suggestions] model, else the reviewer's.
func TestSuggestionsFailure(t *testing.T) {
	w, _ := openTestWith(t, func(c *config.Config) { c.Permissions.Auto.Model = "reviewer" }, text("answer"))
	ctx := context.Background()
	var asked []string
	w.newModel = func(_ context.Context, _ *config.Config, ref string) (model.LLM, error) {
		asked = append(asked, ref)
		return nil, errors.New("no key")
	}
	sess, err := w.NewSession()
	require.NoError(t, err)
	_, err = w.Run(ctx, sess.ID, api.Turn{Text: "hello"}, func(api.Event) {})
	require.NoError(t, err)

	assert.True(t, w.Suggestions(ctx).Pending)
	waitIdeas(t, w)
	assert.False(t, w.Suggestions(ctx).Pending, "not retried at once")
	assert.Equal(t, []string{"reviewer"}, asked)

	c := w.readIdeas()
	c.Failed = time.Now().Add(-2 * ideaRetry)
	w.saveIdeas(c)
	w.cfg.Suggestions.Model = "cheap"
	assert.True(t, w.Suggestions(ctx).Pending, "retried after a while")
	waitIdeas(t, w)
	assert.Equal(t, []string{"reviewer", "cheap"}, asked)
}

// Uncommitted changes and a failed worker run are tiles too.
func TestSuggestionsFromState(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	w := openTest(t)
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "BLITZ.md"), []byte("x"), 0o644))
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = w.Dir()
	require.NoError(t, cmd.Run())
	addWorker(t, w, "digest", "---\nschedule: \"@daily\"\n---\nSummarise.\n")
	require.NoError(t, w.runLog.Append(api.Run{ID: "r1", Workspace: w.Dir(), Worker: "digest", Status: api.RunFailed, Started: time.Now(), Error: "model unavailable"}))

	s := w.Suggestions(context.Background())
	require.Equal(t, []api.SuggestionKind{api.SuggestChanges, api.SuggestWorkerFailed}, kinds(s))
	assert.Equal(t, 2, s.Tiles[0].Count, "BLITZ.md and the worker")
	assert.Equal(t, "digest", s.Tiles[1].Worker)
	assert.Equal(t, "model unavailable", s.Tiles[1].Detail)
}

func TestParseIdeas(t *testing.T) {
	long := strings.Repeat("x", 1000)
	cases := []struct {
		name, reply string
		want        int
		err         bool
	}{
		{"bare", `[{"title":"a","prompt":"b"}]`, 1, false},
		{"at most three", `[{"title":"a","prompt":"b"},{"title":"c","prompt":"d"},{"title":"e","prompt":"f"},{"title":"g","prompt":"h"}]`, 3, false},
		{"long ones are cut", `[{"title":"` + long + `","prompt":"` + long + `"}]`, 1, false},
		{"empty", `[]`, 0, false},
		{"no array", `I can't.`, 0, true},
		{"bad JSON", `[{"title": }]`, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseIdeas(c.reply)
			if c.err {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, got, c.want)
			for _, i := range got {
				assert.LessOrEqual(t, len(i.Title), 95)
				assert.LessOrEqual(t, len(i.Prompt), 605)
			}
		})
	}
}

func TestTail(t *testing.T) {
	assert.Equal(t, "short", tail("short", 10))
	assert.Equal(t, "…fgh", tail("abcdefgh", 3))
	assert.Equal(t, "…é", tail("aaé", 2), "a cut rune is skipped") // é is two bytes
	assert.Equal(t, "…", tail("aé", 1))
}

// /setup runs the setup agent, and what it wrote applies from the next
// prompt without reloading by hand.
func TestSetupCommandReloadsInstructions(t *testing.T) {
	agentMD := toolCall("create_file", map[string]any{"path": ".agents/AGENT.md", "content": "Run tests with `make check`."})
	claude := toolCall("create_file", map[string]any{"path": "CLAUDE.md", "content": "@.agents/AGENT.md\n"})
	w, llm := openTestWith(t, func(c *config.Config) { c.Blitz.PermissionMode = "accept-edits" }, agentMD, claude, text("set up"), text("hi"))
	ctx := context.Background()
	assert.Contains(t, w.ListCommands(), api.CommandInfo{Name: "setup", Description: "Set up this workspace's agent harness: a git repository and .gitignore, .agents/AGENT.md, the instruction files that import it, skills and agents", ArgumentHint: "[what to focus on]", Source: "bundled"})
	sess, err := w.NewSession()
	require.NoError(t, err)
	_, err = w.Run(ctx, sess.ID, api.Turn{Text: "/setup", Command: true}, func(api.Event) {})
	require.NoError(t, err)
	var first strings.Builder
	for _, c := range llm.Requests[0].Contents {
		for _, p := range c.Parts {
			first.WriteString(p.Text)
		}
	}
	assert.Contains(t, first.String(), "Set up this workspace's agent harness")
	assert.False(t, w.HarnessMissing())

	_, err = w.Run(ctx, sess.ID, api.Turn{Text: "hello"}, func(api.Event) {})
	require.NoError(t, err)
	last := llm.Requests[len(llm.Requests)-1]
	require.NotNil(t, last.Config.SystemInstruction)
	var system strings.Builder
	for _, p := range last.Config.SystemInstruction.Parts {
		system.WriteString(p.Text)
	}
	assert.Contains(t, system.String(), "Run tests with `make check`.", "the imported AGENT.md is in the next prompt's instructions")
	assert.Equal(t, 1, strings.Count(system.String(), "Run tests with"), "imported once")
	assert.True(t, isSetup("/setup focus on tests"))
	assert.False(t, isSetup("/setups"))
}
