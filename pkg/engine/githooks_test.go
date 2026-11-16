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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/flows"
	"github.com/retail-cortex/blitz/pkg/enterprise"
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

	w.installOrgHooks() // installed, and logged
	done, err = w.InstallOrgHooks(ctx)
	require.NoError(t, err)
	assert.Empty(t, done, "installed already")
	require.NoError(t, os.Remove(filepath.Join(hooks, "pre-push")))
	b0, err := os.ReadFile(filepath.Join(hooks, "pre-commit.local"))
	require.NoError(t, err)
	require.NoError(t, os.Rename(filepath.Join(hooks, "pre-commit.local"), filepath.Join(hooks, "pre-commit")))
	require.Equal(t, own, string(b0))
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

// EN-94: a workflow's and an eval's runs are reported to the organization
// when they end, connected to one; not without one. The report reaches
// the service as the person.
func TestReportRuns(t *testing.T) {
	var mu sync.Mutex
	var got []enterprise.RunReport
	was := reportRuns
	reportRuns = func(_ string, r enterprise.RunReport) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, r)
	}
	t.Cleanup(func() { reportRuns = was })
	w, _ := openTestWith(t, func(c *config.Config) { c.Enterprise.URL = "https://blitz.acme.com" })
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "notes.md"), []byte("hi"), 0o644))
	writeFlow(t, w, "notes", "nodes: [{id: a, type: read_file, path: notes.md}]\n")
	_, err := w.Run(context.Background(), newSession(t, w).ID, api.Turn{Text: "/notes", Command: true}, func(api.Event) {})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(w.Dir(), ".agents", "evals"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), ".agents", "evals", "sums.md"), []byte("| ID | Prompt | Output | Type |\n|-|-|-|-|\n| a | 2+2? | 4 | exact |\n"), 0o644))
	_, _ = w.RunEval(context.Background(), "sums", api.EvalOptions{})
	mu.Lock()
	require.Len(t, got, 2, "%v", got)
	assert.Equal(t, "workflow", got[0].Kind)
	assert.Equal(t, "notes", got[0].Name)
	assert.Equal(t, RunDone, got[0].Status)
	assert.Equal(t, filepath.Base(w.Dir()), got[0].Workspace)
	assert.Equal(t, "eval", got[1].Kind)
	assert.Equal(t, "sums", got[1].Name)
	mu.Unlock()

	// Without an organization: none.
	w2, _ := openTestWith(t, nil)
	writeFlow(t, w2, "empty", "nodes: [{id: a, type: timer, wait: 1ms}]\n")
	_, _ = w2.Run(context.Background(), newSession(t, w2).ID, api.Turn{Text: "/empty", Command: true}, func(api.Event) {})
	mu.Lock()
	assert.Len(t, got, 2)
	mu.Unlock()

	// The real one reaches the service.
	org := signedInOrg(t)
	was(org.URL, enterprise.RunReport{Kind: "workflow", Name: "brief", Status: "succeeded", StartedAt: time.Now()})
	require.Eventually(t, func() bool {
		org.Mu.Lock()
		defer org.Mu.Unlock()
		return len(org.Reported) == 1
	}, 5*time.Second, 10*time.Millisecond)
}

// WF-31: an organization API step sends its JSON or text body and its
// headers; a response over what a step keeps fails it, as an unreachable
// service does.
func TestFlowOrgAPIBodies(t *testing.T) {
	org := signedInOrg(t)
	var mu sync.Mutex
	var seen []string
	org.Mu.Lock()
	org.SharedAPIs = map[string]http.Handler{"echo": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, r.Header.Get("Content-Type")+" "+r.Header.Get("Accept")+" "+string(b))
		mu.Unlock()
		if r.URL.Path == "/big" {
			_, _ = w.Write([]byte(strings.Repeat("a", flowResponseLimit+1)))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok": true}`))
	})}
	org.Mu.Unlock()
	tests := []struct {
		name, flow, err, saw string
	}{
		{"JSON", "nodes: [{id: a, type: api_call, connector: echo, method: POST, url: /j, json: {diff: \"{{ input }}\"}}]\n", "", `application/json  {"diff":"x"}`},
		{"text and a header", "nodes: [{id: a, type: api_call, connector: echo, method: POST, url: /t, body: \"hi {{ input }}\", headers: [{name: Accept, value: text/plain}]}]\n", "", " text/plain hi x"},
		{"too much", "nodes: [{id: a, type: api_call, connector: echo, url: /big}]\n", "more than 256 KiB", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, _ := openTestWith(t, func(c *config.Config) { c.Enterprise.URL = org.URL })
			writeFlow(t, w, "call", tt.flow)
			mu.Lock()
			seen = nil
			mu.Unlock()
			_, err := w.Run(context.Background(), newSession(t, w).ID, api.Turn{Text: "/call x", Command: true}, func(api.Event) {})
			if tt.err != "" {
				assert.ErrorContains(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			mu.Lock()
			assert.Equal(t, []string{tt.saw}, seen)
			mu.Unlock()
		})
	}
	bad, _ := openTestWith(t, func(c *config.Config) { c.Enterprise.URL = "::not a url" })
	writeFlow(t, bad, "call", "nodes: [{id: a, type: api_call, connector: echo}]\n")
	_, err := bad.Run(context.Background(), newSession(t, bad).ID, api.Turn{Text: "/call", Command: true}, func(api.Event) {})
	assert.Error(t, err, "an address that doesn't read")
	w, _ := openTestWith(t, func(c *config.Config) { c.Enterprise.URL = "http://127.0.0.1:1" })
	writeFlow(t, w, "call", "nodes: [{id: a, type: api_call, connector: echo}]\n")
	_, err = w.Run(context.Background(), newSession(t, w).ID, api.Turn{Text: "/call", Command: true}, func(api.Event) {})
	assert.Error(t, err, "nothing answers")
}

// A receipt the organization can't take is said, not a failure of the
// hook; a run's report to a service that isn't signed in is dropped.
func TestReceiptUnreachable(t *testing.T) {
	w, _ := openTestWith(t, func(c *config.Config) { c.Enterprise.URL = "http://127.0.0.1:1" })
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "notes.md"), []byte("hi"), 0o644))
	writeFlow(t, w, "notes", "triggers: [git.pre_push]\nnodes: [{id: a, type: read_file, path: notes.md}]\n")
	got, err := w.RunGitHook(context.Background(), "pre-push", "")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.True(t, got[0].Passed)
	reportRuns("http://127.0.0.1:1", enterprise.RunReport{Kind: "workflow", Name: "x"})
}

// A repository whose hooks folder can't be made: said.
func TestInstallOrgHooksUnwritable(t *testing.T) {
	w, _ := openTestWith(t, nil)
	policy := t.TempDir()
	req := filepath.Join(policy, config.OrgRequired, "workflows")
	require.NoError(t, os.MkdirAll(req, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(req, "review"+flows.Suffix), []byte(reviewFlow), 0o644))
	w.setSettings(func(c *config.Config) { c.Org.PolicyDir = policy })
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "README.md"), []byte("hi\n"), 0o644))
	gitRepo(t, w.Dir())
	hooks := filepath.Join(w.Dir(), ".git", "hooks")
	require.NoError(t, os.RemoveAll(hooks))
	require.NoError(t, os.WriteFile(hooks, []byte("not a folder"), 0o644))
	_, err := w.InstallOrgHooks(context.Background())
	assert.Error(t, err)
}
