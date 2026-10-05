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
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	"github.com/retail-cortex/blitz/pkg/config"
)

// promptLLM answers every request with reply (or err), keeping the prompts.
type promptLLM struct {
	reply   string
	err     error
	prompts []string
}

func (p *promptLLM) Name() string { return "drafter" }

func (p *promptLLM) GenerateContent(_ context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	var b strings.Builder
	for _, c := range req.Contents {
		for _, part := range c.Parts {
			b.WriteString(part.Text)
		}
	}
	p.prompts = append(p.prompts, b.String())
	return func(yield func(*model.LLMResponse, error) bool) {
		if p.err != nil {
			yield(nil, p.err)
			return
		}
		usage := &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 100, CandidatesTokenCount: 10}
		yield(&model.LLMResponse{Content: genai.NewContentFromText(p.reply, genai.RoleModel), UsageMetadata: usage}, nil)
	}
}

// commitRepo makes w's folder a repository with one commit and an identity.
func commitRepo(t *testing.T, w *Workspace) {
	t.Helper()
	gitOut(t, w.Dir(), "init", "-q")
	gitOut(t, w.Dir(), "config", "user.email", "t@t")
	gitOut(t, w.Dir(), "config", "user.name", "t")
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "README.md"), []byte("shop\n"), 0o644))
	gitOut(t, w.Dir(), "add", "README.md")
	gitOut(t, w.Dir(), "commit", "-q", "-m", "feat: start the shop")
}

func TestCleanCommitMessage(t *testing.T) {
	tests := []struct {
		name, reply, want string
	}{
		{"plain", "Fix coupon rounding\n", "Fix coupon rounding"},
		{"fenced", "```text\nFix coupon rounding\n\nRound after summing.\n```", "Fix coupon rounding\n\nRound after summing."},
		{"quoted", "\"Fix coupon rounding\"", "Fix coupon rounding"},
		{"trailing spaces", "Fix it  \n\n- one  \n", "Fix it\n\n- one"},
		{"empty", "  ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cleanCommitMessage(tt.reply))
		})
	}
}

// The commit dialog's draft: what's staged, and a message the model wrote
// from its diff, secrets redacted; nothing asked with nothing staged; a
// failing model leaves the message to the user.
func TestDraftCommit(t *testing.T) {
	needGit(t)
	ctx := context.Background()
	const secret = "gemini-key-0123456789abcdef"

	t.Run("not a repository", func(t *testing.T) {
		w := openTest(t)
		_, err := w.DraftCommit(ctx, false)
		assert.ErrorIs(t, err, ErrNotARepository)
	})

	t.Run("staged and drafted", func(t *testing.T) {
		w, _ := openTestWith(t, func(c *config.Config) { c.LLM.Gemini.APIKey = secret })
		commitRepo(t, w)
		llm := &promptLLM{reply: "```\nfeat: round coupons after summing\n```"}
		var asked []string
		w.newModel = func(_ context.Context, _ *config.Config, ref string) (model.LLM, error) {
			asked = append(asked, ref)
			return llm, nil
		}
		require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "cart.go"), []byte("package cart // key "+secret+"\n"), 0o644))

		d, err := w.DraftCommit(ctx, false)
		require.NoError(t, err)
		assert.Empty(t, d.Files, "nothing staged yet")
		assert.Empty(t, asked, "no model call without anything staged")

		d, err = w.DraftCommit(ctx, true)
		require.NoError(t, err)
		assert.Equal(t, []CommitFile{{Status: "A", Path: "cart.go"}}, d.Files, "stage all: git add --all")
		assert.Equal(t, "feat: round coupons after summing", d.Message)
		assert.Empty(t, d.Problem)
		require.Len(t, llm.prompts, 1)
		assert.Contains(t, llm.prompts[0], "feat: start the shop", "the recent subjects, for the style")
		assert.Contains(t, llm.prompts[0], "cart.go")
		assert.NotContains(t, llm.prompts[0], secret, "secrets are redacted")
	})

	t.Run("staging fails", func(t *testing.T) {
		w := openTest(t)
		commitRepo(t, w)
		require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "README.md"), []byte("locked\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), ".git", "index.lock"), nil, 0o644))
		_, err := w.DraftCommit(ctx, true)
		assert.ErrorContains(t, err, "index.lock", "git's message comes back")
	})

	t.Run("a long diff is cut", func(t *testing.T) {
		w := openTest(t)
		commitRepo(t, w)
		llm := &promptLLM{reply: "Add the data"}
		w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return llm, nil }
		require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "data.txt"), []byte(strings.Repeat("row\n", maxCommitDiffChars)), 0o644))
		d, err := w.DraftCommit(ctx, true)
		require.NoError(t, err)
		assert.Equal(t, "Add the data", d.Message)
		require.Len(t, llm.prompts, 1)
		assert.Contains(t, llm.prompts[0], "the rest of the diff is cut")
		assert.Less(t, len(llm.prompts[0]), maxCommitDiffChars+2000)
	})

	for _, tt := range []struct {
		name  string
		build func(context.Context, *config.Config, string) (model.LLM, error)
		want  string
	}{
		{"no model", func(context.Context, *config.Config, string) (model.LLM, error) { return nil, errors.New("no key") }, "no key"},
		{"a client's config in the error", func(context.Context, *config.Config, string) (model.LLM, error) {
			return nil, errors.New("bad key. ClientConfig: {APIKey: sk-secret}")
		}, "bad key"},
		{"an empty reply", func(context.Context, *config.Config, string) (model.LLM, error) {
			return &promptLLM{reply: "```\n```"}, nil
		}, "no message"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := openTest(t)
			commitRepo(t, w)
			w.newModel = tt.build
			require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "README.md"), []byte("shop v3\n"), 0o644))
			d, err := w.DraftCommit(ctx, true)
			require.NoError(t, err)
			assert.Len(t, d.Files, 1)
			assert.Empty(t, d.Message)
			assert.Contains(t, d.Problem, tt.want)
			assert.NotContains(t, d.Problem, "ClientConfig", "the summary, never the client's internals")
		})
	}

	t.Run("the model fails", func(t *testing.T) {
		w := openTest(t)
		commitRepo(t, w)
		w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) {
			return &promptLLM{err: errors.New("model unavailable")}, nil
		}
		require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "README.md"), []byte("shop v2\n"), 0o644))
		d, err := w.DraftCommit(ctx, true)
		require.NoError(t, err)
		assert.Equal(t, []CommitFile{{Status: "M", Path: "README.md"}}, d.Files)
		assert.Empty(t, d.Message)
		assert.Contains(t, d.Problem, "model unavailable")
	})
}

// Committing what's staged: a message is required, an empty index refused,
// and the new commit's hash and subject come back.
func TestCommit(t *testing.T) {
	needGit(t)
	ctx := context.Background()
	w := openTest(t)

	_, _, err := w.Commit(ctx, "anything")
	assert.ErrorIs(t, err, ErrNotARepository)

	commitRepo(t, w)
	_, _, err = w.Commit(ctx, "  ")
	assert.ErrorIs(t, err, ErrEmptyMessage)
	_, _, err = w.Commit(ctx, "nothing here")
	assert.ErrorIs(t, err, ErrNothingStaged)

	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "cart.go"), []byte("package cart\n"), 0o644))
	gitOut(t, w.Dir(), "add", "cart.go")
	hash, subject, err := w.Commit(ctx, "Add the cart\n\nThe cart package, empty for now.\n")
	require.NoError(t, err)
	assert.Equal(t, "Add the cart", subject)
	assert.Equal(t, gitOut(t, w.Dir(), "rev-parse", "--short", "HEAD"), hash)
	assert.Equal(t, "Add the cart\n\nThe cart package, empty for now.", gitOut(t, w.Dir(), "log", "-1", "--format=%B"))
	assert.Empty(t, gitOut(t, w.Dir(), "status", "--porcelain"))
}

// A hook that refuses the commit: its message comes back, nothing is
// committed and the index stays staged.
func TestCommitHookFails(t *testing.T) {
	needGit(t)
	ctx := context.Background()
	w := openTest(t)
	commitRepo(t, w)
	hook := filepath.Join(w.Dir(), ".git", "hooks", "pre-commit")
	require.NoError(t, os.WriteFile(hook, []byte("#!/bin/sh\necho 'lint failed' >&2\nexit 1\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "cart.go"), []byte("package cart\n"), 0o644))
	gitOut(t, w.Dir(), "add", "cart.go")
	_, _, err := w.Commit(ctx, "Add the cart")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lint failed")
	assert.Equal(t, "feat: start the shop", gitOut(t, w.Dir(), "log", "-1", "--format=%s"))
	assert.Equal(t, "A  cart.go", gitOut(t, w.Dir(), "status", "--porcelain"))
}
