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
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/flows"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/enterprise"
)

// The git events a workflow can run on (spec_043 WF-30), by the hook's
// name.
var hookTriggers = map[string]string{"pre-commit": flows.TriggerPreCommit, "pre-push": flows.TriggerPrePush}

// ErrUnknownHook is a git hook no workflow can run on.
var ErrUnknownHook = api.ErrUnknownHook

// RunGitHook runs the workspace's workflows that run on the git hook
// event (pre-commit, pre-push), the organization's among them (WF-32):
// each on input (the changes: the staged diff, or what's pushed),
// unattended, in its own session; one that fails (a step that fails, an
// API that answers 400 or more) fails the hook. Each one's result goes to
// the organization's service as a receipt when there is one (EN-92).
func (w *Workspace) RunGitHook(ctx context.Context, event, input string) ([]api.HookResult, error) {
	trigger, ok := hookTriggers[event]
	if !ok {
		return nil, ErrUnknownHook
	}
	st, err := session.NewStorage(w.cfg.Session.StorageDir)
	if err != nil {
		return nil, err
	}
	defer st.Release()
	st.SetWorkspace(w.Dir())
	tree := gitTree(ctx, w.Dir(), event)
	out := []api.HookResult{}
	for _, f := range w.Workflows() {
		if !f.RunsOn(trigger) {
			continue
		}
		r := api.HookResult{Workflow: f.Name, Version: w.orgVersion(f)}
		if f.Problem != "" {
			r.Error = f.Problem
			out = append(out, w.receipt(ctx, event, tree, r))
			continue
		}
		rec, err := st.CreateSession(session.NewSessionID(), fmt.Sprintf("%s %s · %s", event, f.Name, time.Now().Format("2006-01-02 15:04")), w.engine.ActiveAgent())
		if err != nil {
			return out, err
		}
		rec.Origin = session.OriginWorker
		var last string
		runCtx := tools.Unattended(ctx, func(context.Context, api.ApprovalRequest) (api.Decision, error) { return api.DecisionDeny, nil })
		_, runErr := w.runFlow(withFlowOrigin(withFlowStorage(runCtx, st), "hook"), rec.ID, f, input, func(e api.Event) {
			if e.Text != nil && !e.Text.Partial && !e.Text.Thought {
				last = e.Text.Text
			}
		})
		r.Passed, r.Output = runErr == nil, strings.TrimSpace(last)
		if runErr != nil {
			r.Error = runErr.Error()
		}
		out = append(out, w.receipt(ctx, event, tree, r))
	}
	return out, nil
}

// orgVersion is the organization's version of workflow f ("" for the
// workspace's own).
func (w *Workspace) orgVersion(f flows.Flow) string {
	org := w.settings().Org
	if org.PolicyDir == "" || !strings.HasPrefix(f.Path, org.PolicyDir+string(filepath.Separator)) {
		return ""
	}
	for _, a := range org.Assets() {
		if a.Kind == "workflow" && a.Name == f.Name && a.State != "dependency" {
			return a.Version
		}
	}
	return ""
}

// receipt sends r to the organization's service (EN-92), and is r: one
// that can't be sent is said, not a failure of the hook.
func (w *Workspace) receipt(ctx context.Context, event, tree string, r api.HookResult) api.HookResult {
	cfg := w.settings()
	if cfg.Enterprise.URL == "" {
		return r
	}
	client, err := orgClient(cfg.Enterprise.URL)
	if err == nil {
		msg := r.Error
		if msg == "" {
			msg = r.Output
		}
		err = client.Receipt(ctx, enterprise.Receipt{Workflow: r.Workflow, Version: r.Version, Event: event, Tree: tree, Passed: r.Passed,
			Message: truncateText(msg, 2000), Workspace: filepath.Base(w.Dir()), Repo: gitRemote(ctx, w.Dir()), At: time.Now().UTC()})
	}
	if err != nil {
		w.warn("telling your organization what " + r.Workflow + " found: " + err.Error())
	}
	return r
}

func truncateText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// gitTree is what event runs on, by git's hash: the staged tree for a
// commit, HEAD for a push ("" outside a repository).
func gitTree(ctx context.Context, dir, event string) string {
	args := []string{"write-tree"}
	if event == "pre-push" {
		args = []string{"rev-parse", "HEAD"}
	}
	out, err := gitText(ctx, dir, args...)
	if err != nil {
		return ""
	}
	return out
}

// gitRemote is the repository's origin's URL ("" for none).
func gitRemote(ctx context.Context, dir string) string {
	out, _ := gitText(ctx, dir, "remote", "get-url", "origin")
	return out
}

func gitText(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var b bytes.Buffer
	cmd.Stdout = &b
	err := cmd.Run()
	return strings.TrimSpace(b.String()), err
}

// ErrHooksInTree is a repository whose hooks are in its work tree
// (core.hooksPath), which Blitz doesn't rewrite (EN-90).
var ErrHooksInTree = errors.New("this repository's git hooks are in its files (core.hooksPath), which Blitz doesn't change")

// within reports whether path is dir or inside it, both cleaned and
// their symbolic links followed.
func within(path, dir string) bool {
	if p, err := filepath.EvalSymlinks(path); err == nil {
		path = p
	}
	if d, err := filepath.EvalSymlinks(dir); err == nil {
		dir = d
	}
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// orgHookHeader starts every hook the organization's workflows install,
// so InstallOrgHooks knows its own.
const orgHookHeader = "#!/bin/sh\n# Blitz: your organization's workflows on this git hook (spec_053 EN-90).\n"

// orgHookScript is the hook for event: the repository's own hook first,
// moved beside it (<hook>.local), then Blitz's run of the workflows; a
// commit or push one of them fails is stopped. Without blitz on PATH it
// says so and lets it through.
func orgHookScript(event string) string {
	return orgHookHeader + `# Your own hook, if you had one, is ` + event + `.local beside this one: it runs first.
here="$(dirname "$0")"
if [ -x "$here/` + event + `.local" ]; then
	"$here/` + event + `.local" "$@" || exit $?
fi
if command -v blitz >/dev/null 2>&1; then
	exec blitz hook run ` + event + `
fi
echo "Blitz isn't on PATH: your organization's ` + event + ` checks didn't run." >&2
exit 0
`
}

// InstallOrgHooks installs the git hooks the organization's required
// workflows run on (EN-90), in the hooks folder the repository uses (its
// core.hooksPath, else .git/hooks): a hook of the repository's own is
// kept, moved to <hook>.local and run first; one Blitz installed before is
// brought up to date. It says which hooks it installed; outside a git
// repository, or with no such workflow, none.
func (w *Workspace) InstallOrgHooks(ctx context.Context) ([]string, error) {
	org := w.settings().Org
	dir := org.ItemDir(config.OrgRequired, "workflows")
	if dir == "" {
		return nil, nil
	}
	var events []string
	for _, f := range flows.Load(dir) {
		for ev, tr := range hookTriggers {
			if f.RunsOn(tr) && !slices.Contains(events, ev) {
				events = append(events, ev)
			}
		}
	}
	if len(events) == 0 {
		return nil, nil
	}
	hooks, err := gitText(ctx, w.Dir(), "rev-parse", "--git-path", "hooks")
	if err != nil || hooks == "" {
		return nil, nil // not a git repository
	}
	if !filepath.IsAbs(hooks) {
		hooks = filepath.Join(w.Dir(), hooks)
	}
	// A hooks folder in the work tree (core.hooksPath, as Blitz's secrets
	// hook sets it: spec_051) is the repository's, committed: Blitz
	// doesn't rewrite it, and says what to add.
	if gitDir, err := gitText(ctx, w.Dir(), "rev-parse", "--absolute-git-dir"); err == nil && !within(hooks, gitDir) {
		if top, err := gitText(ctx, w.Dir(), "rev-parse", "--show-toplevel"); err == nil && within(hooks, top) {
			slices.Sort(events)
			return nil, fmt.Errorf("%w: %s; add `blitz hook run <hook>` to its %s", ErrHooksInTree, hooks, strings.Join(events, " and "))
		}
	}
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		return nil, err
	}
	slices.Sort(events)
	var done []string
	for _, ev := range events {
		path := filepath.Join(hooks, ev)
		want := orgHookScript(ev)
		data, err := os.ReadFile(path)
		switch {
		case err == nil && string(data) == want:
			continue
		case err == nil && !strings.HasPrefix(string(data), orgHookHeader):
			// The repository's own: kept, and run first.
			if _, statErr := os.Stat(path + ".local"); statErr == nil {
				return done, fmt.Errorf("%s: both it and %s.local are there; move one aside", path, ev)
			}
			if err := os.Rename(path, path+".local"); err != nil {
				return done, err
			}
		}
		if err := os.WriteFile(path, []byte(want), 0o755); err != nil {
			return done, err
		}
		if err := os.Chmod(path, 0o755); err != nil {
			return done, err
		}
		done = append(done, ev)
	}
	return done, nil
}

// installOrgHooks is InstallOrgHooks for an opened workspace, or one whose
// organization's policy changed: what it installed logged, a failure
// said.
func (w *Workspace) installOrgHooks() {
	done, err := w.InstallOrgHooks(context.Background())
	if err != nil {
		w.warn("installing your organization's git hooks: " + err.Error())
		return
	}
	if len(done) > 0 {
		slog.Info("your organization's git hooks installed", "workspace", w.Dir(), "hooks", strings.Join(done, ", "))
	}
}
