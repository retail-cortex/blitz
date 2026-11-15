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
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/flows"
)

// reviewAPI is an organization's code review API: a diff holding
// "secret" fails (422), any other passes.
func reviewAPI() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/check" {
			http.NotFound(w, r)
			return
		}
		if strings.Contains(string(b), "secret") {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"verdict":"fail","why":"a secret in the diff"}`))
			return
		}
		_, _ = w.Write([]byte(`{"verdict":"pass"}`))
	})
}

// reviewFlow is a workflow that sends its input to the review API.
const reviewFlow = `triggers: [git.pre_commit]
nodes:
  - id: review
    type: api_call
    connector: review
    method: POST
    url: /check
`

// WF-30: a workflow's triggers are checked; RunsOn says which it runs on.
func TestFlowTriggers(t *testing.T) {
	tests := []struct {
		name     string
		triggers string
		problem  string
		runsOn   []string
	}{
		{"none: manual", "", "", nil},
		{"git hooks", "[git.pre_commit, git.pre_push]", "", []string{flows.TriggerPreCommit, flows.TriggerPrePush}},
		{"an unknown one", "[git.post_merge]", `trigger "git.post_merge"`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			src := "nodes: [{id: a, type: api_call, connector: review}]\n"
			if tt.triggers != "" {
				src = "triggers: " + tt.triggers + "\n" + src
			}
			require.NoError(t, os.WriteFile(filepath.Join(dir, "f"+flows.Suffix), []byte(src), 0o644))
			got := flows.Load(dir)
			require.Len(t, got, 1)
			if tt.problem == "" {
				assert.Empty(t, got[0].Problem, "a connector's step needs no URL")
			} else {
				assert.Contains(t, got[0].Problem, tt.problem)
			}
			for _, tr := range flows.Triggers {
				assert.Equal(t, strings.Contains(strings.Join(tt.runsOn, " "), tr), got[0].RunsOn(tr), tr)
			}
		})
	}
}

// WF-31: an API step naming an organization's connector calls it through
// the service as the person: a 400 or more fails the step; without an
// organization, it says so.
func TestFlowOrgAPICall(t *testing.T) {
	org := signedInOrg(t)
	org.Mu.Lock()
	org.SharedAPIs = map[string]http.Handler{"review": reviewAPI()}
	org.Mu.Unlock()
	tests := []struct {
		name  string
		org   bool
		input string
		err   string
	}{
		{"passes", true, "a fine change", ""},
		{"fails", true, "a secret change", "answered 422"},
		{"no organization", false, "x", "isn't connected to an organization"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, _ := openTestWith(t, func(c *config.Config) {
				if tt.org {
					c.Enterprise.URL = org.URL
				}
			})
			writeFlow(t, w, "review", reviewFlow)
			_, err := w.Run(context.Background(), newSession(t, w).ID, api.Turn{Text: "/review " + tt.input, Command: true}, func(api.Event) {})
			if tt.err == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tt.err)
			}
		})
	}
}

// WF-32, EN-92: a git hook runs the workflows that run on it, each on the
// changes, a receipt for each sent; one that fails fails; one that can't
// be read fails too; manual ones don't run; an unknown hook is refused.
func TestRunGitHook(t *testing.T) {
	org := signedInOrg(t)
	org.Mu.Lock()
	org.SharedAPIs = map[string]http.Handler{"review": reviewAPI()}
	org.Mu.Unlock()
	w, _ := openTestWith(t, func(c *config.Config) { c.Enterprise.URL = org.URL })
	writeFlow(t, w, "review", reviewFlow)
	writeFlow(t, w, "broken", "triggers: [git.pre_commit, nope]\nnodes: [{id: a, type: api_call, connector: review}]\n")
	writeFlow(t, w, "manual", "nodes: [{id: a, type: api_call, connector: review, url: /check}]\n")
	gitRepo(t, w.Dir())
	ctx := context.Background()

	_, err := w.RunGitHook(ctx, "post-merge", "")
	assert.ErrorIs(t, err, ErrUnknownHook)

	tests := []struct {
		name   string
		input  string
		passed bool
	}{
		{"a fine change", "+ fine", true},
		{"a secret", "+ secret", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			org.Mu.Lock()
			org.Receipts = nil
			org.Mu.Unlock()
			got, err := w.RunGitHook(ctx, "pre-commit", tt.input)
			require.NoError(t, err)
			byName := map[string]api.HookResult{}
			for _, r := range got {
				byName[r.Workflow] = r
			}
			require.Len(t, byName, 2, "manual isn't run: %v", got)
			assert.False(t, byName["broken"].Passed)
			assert.Contains(t, byName["broken"].Error, `trigger "nope"`)
			assert.Equal(t, tt.passed, byName["review"].Passed, byName["review"].Error)
			if !tt.passed {
				assert.Contains(t, byName["review"].Error, "a secret in the diff")
			}
			org.Mu.Lock()
			defer org.Mu.Unlock()
			require.Len(t, org.Receipts, 2)
			for _, rec := range org.Receipts {
				assert.Equal(t, "pre-commit", rec["event"])
				assert.NotEmpty(t, rec["tree"], "the staged tree")
				if rec["workflow"] == "review" {
					assert.Equal(t, tt.passed, rec["passed"])
				}
			}
		})
	}

	// No organization: no receipts, the hook still runs.
	w2, _ := openTestWith(t, nil)
	writeFlow(t, w2, "manual", "nodes: [{id: a, type: api_call, connector: review, url: /check}]\n")
	got, err := w2.RunGitHook(ctx, "pre-push", "")
	require.NoError(t, err)
	assert.Empty(t, got)
}

// EN-90: the organization's required workflows' git hooks are installed in
// a repository: its own hook kept as <hook>.local and run first, Blitz's
// brought up to date, none outside a repository or without such a
// workflow.
func TestInstallOrgHooks(t *testing.T) {
	ctx := context.Background()
	w, _ := openTestWith(t, nil)
	policy := t.TempDir()
	w.setSettings(func(c *config.Config) { c.Org.PolicyDir = policy })

	done, err := w.InstallOrgHooks(ctx)
	require.NoError(t, err)
	assert.Empty(t, done, "no required workflows")

	req := filepath.Join(policy, config.OrgRequired, "workflows")
	require.NoError(t, os.MkdirAll(req, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(req, "review"+flows.Suffix), []byte(strings.Replace(reviewFlow, "[git.pre_commit]", "[git.pre_commit, git.pre_push]", 1)), 0o644))
	done, err = w.InstallOrgHooks(ctx)
	require.NoError(t, err)
	assert.Empty(t, done, "not a repository")

	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "README.md"), []byte("hi\n"), 0o644))
	gitRepo(t, w.Dir())
	hooks := filepath.Join(w.Dir(), ".git", "hooks")
	require.NoError(t, os.MkdirAll(hooks, 0o755))
	own := "#!/bin/sh\necho mine\n"
	require.NoError(t, os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte(own), 0o755))

	done, err = w.InstallOrgHooks(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"pre-commit", "pre-push"}, done)
	b, err := os.ReadFile(filepath.Join(hooks, "pre-commit.local"))
	require.NoError(t, err)
	assert.Equal(t, own, string(b), "the repository's own, kept")
	b, err = os.ReadFile(filepath.Join(hooks, "pre-commit"))
	require.NoError(t, err)
	assert.Equal(t, orgHookScript("pre-commit"), string(b))
	assert.Contains(t, string(b), "blitz hook run pre-commit")
	info, err := os.Stat(filepath.Join(hooks, "pre-push"))
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&0o100, "runnable")

	done, err = w.InstallOrgHooks(ctx)
	require.NoError(t, err)
	assert.Empty(t, done, "up to date")

	// An older Blitz hook is brought up to date; a new hook of the
	// repository's own beside a kept one is refused.
	require.NoError(t, os.WriteFile(filepath.Join(hooks, "pre-push"), []byte(orgHookHeader+"old\n"), 0o755))
	done, err = w.InstallOrgHooks(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"pre-push"}, done)
	require.NoError(t, os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte(own), 0o755))
	_, err = w.InstallOrgHooks(ctx)
	assert.ErrorContains(t, err, "move one aside")
	w.installOrgHooks() // said, not a failure

	// A hooks folder in the work tree (as the secrets hook sets it) isn't
	// rewritten.
	require.NoError(t, os.Remove(filepath.Join(hooks, "pre-commit")))
	cmd := exec.Command("git", "config", "core.hooksPath", ".githooks")
	cmd.Dir = w.Dir()
	require.NoError(t, cmd.Run())
	_, err = w.InstallOrgHooks(ctx)
	assert.ErrorIs(t, err, ErrHooksInTree)
	assert.ErrorContains(t, err, "pre-commit and pre-push")
	assert.NoDirExists(t, filepath.Join(w.Dir(), ".githooks"))
	assert.True(t, within(filepath.Join(w.Dir(), "a", "b"), w.Dir()))
	assert.False(t, within(filepath.Dir(w.Dir()), w.Dir()))
}

// The organization's version of one of its workflows, "" for the
// workspace's own.
func TestOrgVersion(t *testing.T) {
	w, _ := openTestWith(t, nil)
	assert.Empty(t, w.orgVersion(flows.Flow{Name: "review", Path: "/x/review.flow.yaml"}), "no organization")
	policy := t.TempDir()
	items, _ := json.Marshal([]config.OrgAsset{{Kind: "workflow", Name: "review", Version: "1.2.0", State: config.OrgRequired}})
	require.NoError(t, os.WriteFile(filepath.Join(policy, config.OrgItemsFile), items, 0o644))
	w.setSettings(func(c *config.Config) { c.Org.PolicyDir = policy })
	path := filepath.Join(policy, config.OrgRequired, "workflows", "review"+flows.Suffix)
	assert.Equal(t, "1.2.0", w.orgVersion(flows.Flow{Name: "review", Path: path}))
	assert.Empty(t, w.orgVersion(flows.Flow{Name: "other", Path: path}))
	assert.Empty(t, w.orgVersion(flows.Flow{Name: "review", Path: filepath.Join(w.Dir(), "review"+flows.Suffix)}))
	assert.Equal(t, "ab…", truncateText("abc", 2))
	assert.Equal(t, "abc", truncateText("abc", 3))
}
