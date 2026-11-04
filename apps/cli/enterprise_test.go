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

package main

import (
	"bytes"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/client"
	"github.com/retail-cortex/blitz/pkg/enterprise/enterprisetest"
	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blitz enterprise: connect, sign in with a code, the status, sign out,
// disconnect; an address that isn't a service is a usage error.
func TestEnterpriseCommand(t *testing.T) {
	t.Cleanup(http.DefaultClient.CloseIdleConnections)
	home := isolate(t)
	secrets.SetDefault(&secrets.Memory{})
	idp := enterprisetest.NewProvider(t)
	org := enterprisetest.NewService(t, idp.URL)

	out, err := runCLI(t, "enterprise", "status")
	require.NoError(t, err)
	assert.Contains(t, out, "Not connected to an organization")
	_, err = runCLI(t, "enterprise", "login")
	assert.ErrorContains(t, err, "connect to your organization's service first")

	_, err = runCLI(t, "enterprise", "connect", "http://blitz.acme.com")
	assert.Equal(t, exitUsage, exitCodeFor(err), "%v", err)
	out, err = runCLI(t, "enterprise", "connect", org.URL)
	require.NoError(t, err)
	assert.Contains(t, out, "✓ Connected to Acme Assist ("+org.URL+")")

	out, err = runCLI(t, "enterprise", "status")
	require.NoError(t, err)
	assert.Contains(t, out, "version 1.0.0; offers gateway, jobs, branding")
	assert.Contains(t, out, "Signed in     no: blitz enterprise login")

	out, err = runCLI(t, "enterprise", "login")
	require.NoError(t, err, out)
	assert.Contains(t, out, "Open https://www.google.com/device and enter the code ABCD-EFGH")
	assert.Contains(t, out, "✓ Signed in to Acme Assist as ana@acme.com")

	out, err = runCLI(t, "enterprise", "status")
	require.NoError(t, err)
	assert.Contains(t, out, "Signed in     Ana <ana@acme.com> (member)")
	assert.Contains(t, out, "Renews")

	org.Terms = "2026-11"
	idp.IDToken = "for-someone-else"
	_, err = runCLI(t, "enterprise", "login")
	assert.Equal(t, exitFailure, exitCodeFor(err))
	idp.IDToken = ""

	out, err = runCLI(t, "enterprise", "logout")
	require.NoError(t, err)
	assert.Contains(t, out, "Signed out of Acme Assist.")
	out, err = runCLI(t, "enterprise", "disconnect")
	require.NoError(t, err)
	assert.Contains(t, out, "Blitz uses your own model providers again")
	out, err = runCLI(t, "enterprise", "logout")
	require.NoError(t, err)
	assert.Contains(t, out, "Not connected to an organization.")

	// Settings the service can't use: each command says so.
	require.NoError(t, os.WriteFile(filepath.Join(home, ".blitz", ".env.toml"), []byte("[enterprise]\nurl = \"http://evil.example\"\n"), 0o600))
	for _, args := range [][]string{{"status"}, {"login"}, {"logout"}} {
		_, err := runCLI(t, append([]string{"enterprise"}, args...)...)
		assert.Error(t, err, args)
	}
	_, err = runCLI(t, "enterprise", "disconnect")
	assert.NoError(t, err, "disconnecting is how a bad address is undone")

	// No service: each command says it couldn't start one.
	file := filepath.Join(t.TempDir(), "a-file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	t.Setenv("BLITZ_SOCKET", filepath.Join(file, "s.sock"))
	for _, args := range [][]string{{"status"}, {"connect", "https://b.acme.com"}, {"disconnect"}, {"login"}, {"logout"}, {"terms"}, {"workspaces"}, {"open", "x"}, {"create", "x"}, {"share", "."}} {
		_, err := runCLI(t, append([]string{"enterprise"}, args...)...)
		assert.ErrorContains(t, err, "starting the Blitz service", args)
	}
}

// The status says what it knows: a service not reached, the terms to
// accept, a sign-in without a name or roles.
func TestDescribeOrganization(t *testing.T) {
	cases := map[string]struct {
		o    client.Organization
		want []string
	}{
		"not reached": {client.Organization{URL: "https://b.acme.com", Checked: true, Error: "reaching the organization's service: refused"},
			[]string{"Organization  https://b.acme.com (https://b.acme.com)", "Service       not reached: reaching"}},
		"terms": {client.Organization{URL: "https://b.acme.com", Name: "Acme", SignedIn: true, Email: "ana@acme.com", Terms: "v2", Renews: time.Now()},
			[]string{"Signed in     ana@acme.com\n", "Terms         version v2 to accept"}},
		"managed": {client.Organization{URL: "https://b.acme.com", Name: "Acme", Managed: true},
			[]string{"Organization  Acme (https://b.acme.com)\n              set by your organization's device management"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			var b bytes.Buffer
			describeOrganization(&b, c.o)
			for _, w := range c.want {
				assert.Contains(t, b.String(), w)
			}
		})
	}
}

// runCLIWith is runCLI with input on stdin.
func runCLIWith(t *testing.T, input string, args ...string) (string, error) {
	t.Helper()
	cmd := newRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(input))
	cmd.SetArgs(args)
	err := cmd.Execute()
	stopPrivate()
	return out.String(), err
}

// The organization's terms in the CLI: shown after sign-in when a version
// waits, accepted with y, and declined with anything else, which signs
// out (EN-53).
func TestEnterpriseTerms(t *testing.T) {
	t.Cleanup(http.DefaultClient.CloseIdleConnections)
	isolate(t)
	secrets.SetDefault(&secrets.Memory{})
	idp := enterprisetest.NewProvider(t)
	org := enterprisetest.NewService(t, idp.URL)
	org.Version = "v3"
	_, err := runCLI(t, "enterprise", "connect", org.URL)
	require.NoError(t, err)

	out, err := runCLIWith(t, "n\n", "enterprise", "login")
	assert.Equal(t, exitFailure, exitCodeFor(err), out)
	assert.Contains(t, out, "Acme Assist asks you to accept its terms (version v3):")
	assert.Contains(t, out, "Be kind.")
	assert.Contains(t, out, "Not accepted: signed out of Acme Assist.")

	out, err = runCLIWith(t, "y\n", "enterprise", "login")
	require.NoError(t, err, out)
	assert.Contains(t, out, "✓ Accepted Acme Assist's terms (version v3).")

	out, err = runCLI(t, "enterprise", "terms")
	require.NoError(t, err)
	assert.Contains(t, out, "nothing to accept")

	_, err = runCLI(t, "enterprise", "logout")
	require.NoError(t, err)
	_, err = runCLI(t, "enterprise", "terms")
	assert.Error(t, err, "signed out")
}

// blitz enterprise workspaces and create, and blitz git-credential
// answering Git for the service's host (EN-60, EN-61).
func TestEnterpriseWorkspacesCommand(t *testing.T) {
	t.Cleanup(http.DefaultClient.CloseIdleConnections)
	isolate(t)
	secrets.SetDefault(&secrets.Memory{})
	idp := enterprisetest.NewProvider(t)
	org := enterprisetest.NewService(t, idp.URL)
	_, err := runCLI(t, "enterprise", "connect", org.URL)
	require.NoError(t, err)
	_, err = runCLI(t, "enterprise", "login")
	require.NoError(t, err)

	out, err := runCLI(t, "enterprise", "workspaces")
	require.NoError(t, err)
	assert.Contains(t, out, "You have no workspaces in Acme Assist yet")
	out, err = runCLI(t, "enterprise", "create", "Q4 Launch")
	require.NoError(t, err)
	assert.Contains(t, out, "✓ Made Q4 Launch (q4-launch). Open it: blitz enterprise open q4-launch")
	out, err = runCLI(t, "enterprise", "workspaces")
	require.NoError(t, err)
	assert.Contains(t, out, "q4-launch")
	assert.Contains(t, out, "not on this computer: blitz enterprise open q4-launch")
	_, err = runCLI(t, "enterprise", "open", "nope")
	assert.Error(t, err)
	out, err = runCLI(t, "enterprise", "create", "--personal", "Home")
	require.NoError(t, err)
	assert.Contains(t, out, "(~ana/home)", "everyone's own home")

	// A folder whose history holds a key isn't shared.
	leaky := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"commit", "-q", "-m", "first"}} {
		if args[0] == "add" {
			require.NoError(t, os.WriteFile(filepath.Join(leaky, "config.py"), []byte("KEY = 'AKIAIOSFODNN7EXAMPLE'\n"), 0o644))
		}
		cmd := exec.Command("git", append([]string{"-C", leaky, "-c", "user.name=Ana", "-c", "user.email=ana@acme.com"}, args...)...)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
	_, err = runCLI(t, "enterprise", "share", leaky, "--name", "Leaky")
	assert.ErrorContains(t, err, "config.py:1")
	// One already on this computer: listed where it is, and opened there.
	home, _ := os.UserHomeDir()
	clone := filepath.Join(home, "Blitz", "Acme Assist", "q4-launch")
	require.NoError(t, os.MkdirAll(filepath.Join(clone, ".git"), 0o700))
	out, err = runCLI(t, "enterprise", "workspaces")
	require.NoError(t, err)
	assert.Contains(t, out, clone)
	out, err = runCLI(t, "enterprise", "open", "q4-launch")
	require.NoError(t, err)
	assert.Equal(t, clone+"\n", out)

	host := strings.TrimPrefix(org.URL, "http://")
	out, err = runCLIWith(t, "protocol=http\nhost="+host+"\n\n", "git-credential", "get")
	require.NoError(t, err)
	assert.Contains(t, out, "username=blitz\npassword=")
	out, err = runCLIWith(t, "protocol=https\nhost=github.com\n\n", "git-credential", "get")
	require.NoError(t, err)
	assert.Empty(t, out)

	// Save now, in a folder no sync keeps in step.
	t.Chdir(t.TempDir())
	for _, args := range [][]string{{"sync"}, {"sync", "status"}} {
		_, err = runCLI(t, append([]string{"enterprise"}, args...)...)
		assert.ErrorContains(t, err, "isn't kept in step with your organization", args)
	}

	org.Close()
	for _, args := range [][]string{{"workspaces"}, {"create", "y"}} {
		_, err := runCLI(t, append([]string{"enterprise"}, args...)...)
		assert.Error(t, err, args)
	}
}

// The service's jobs from the terminal (EN-42, EN-43): listed, their
// runs, run now, on and off, their files saved, and paused runs answered.
func TestEnterpriseJobsCommand(t *testing.T) {
	t.Cleanup(http.DefaultClient.CloseIdleConnections)
	isolate(t)
	secrets.SetDefault(&secrets.Memory{})
	idp := enterprisetest.NewProvider(t)
	org := enterprisetest.NewService(t, idp.URL)
	_, err := runCLI(t, "enterprise", "connect", org.URL)
	require.NoError(t, err)
	_, err = runCLI(t, "enterprise", "login")
	require.NoError(t, err)

	out, err := runCLI(t, "enterprise", "jobs")
	require.NoError(t, err)
	assert.Contains(t, out, "runs no jobs in your workspaces")

	const (
		ws  = "9b2f0c4e-1d2a-4b7e-9f00-1c2d3e4f5a6b"
		job = "51aa7e10-0b6c-4c2a-8d1e-2f3a4b5c6d7e"
		run = "e8030f9a-7b1c-4d2e-a3f4-5b6c7d8e9f01"
	)
	set := "urn:blitz:artifacts:" + ws + ":" + job + ":" + run
	org.Mu.Lock()
	next := time.Date(2026, 11, 3, 6, 0, 0, 0, time.Local)
	org.ServiceJobs = []enterprisetest.ServiceJob{{ID: job, Workspace: "plans", Name: "digest", Schedule: "Daily at 6 AM", Enabled: true, NextRun: &next},
		{ID: "other", Workspace: "q4", Name: "digest"}, {ID: "lone", Workspace: "q4", Name: "tidy"}}
	org.ServiceRuns = map[string][]enterprisetest.ServiceRun{job: {{ID: run, Status: "succeeded", Trigger: "schedule", Output: "Done.", Branch: "blitz/jobs/digest/" + run, Artifacts: set, CostUSD: 0.05},
		{ID: "paused-1", Status: "paused", Trigger: "manual", Pending: map[string]string{"tool": "run_shell_command", "detail": "make deploy"}}}}
	org.Files = map[string][]enterprisetest.File{set: {{URN: set + "/out/report.csv", Title: "out/report.csv", Size: 4}}}
	org.Contents = map[string]string{set + "/out/report.csv": "a,b\n"}
	org.Waiting = []enterprisetest.GoingRun{{ServiceRun: enterprisetest.ServiceRun{ID: run, Status: "paused", Output: "Ready to deploy.",
		Pending: map[string]string{"tool": "run_shell_command", "kind": "shell", "detail": "make deploy"}}, Job: job, JobName: "digest", Workspace: "plans"}}
	org.Mu.Unlock()

	out, err = runCLI(t, "enterprise", "jobs")
	require.NoError(t, err)
	assert.Contains(t, out, "plans")
	assert.Contains(t, out, "Daily at 6 AM")
	assert.Contains(t, out, "Nov 3 06:00", "when it runs next")
	_, err = runCLI(t, "enterprise", "jobs", "runs", "digest")
	assert.ErrorContains(t, err, "say which with --workspace")
	_, err = runCLI(t, "enterprise", "jobs", "runs", "nope")
	assert.ErrorContains(t, err, `runs no job "nope"`)
	out, err = runCLI(t, "enterprise", "jobs", "runs", "digest", "--workspace", "plans")
	require.NoError(t, err)
	assert.Contains(t, out, "succeeded (schedule)  $0.0500")
	assert.Contains(t, out, "on its branch only, blitz/jobs/digest/"+run+": blitz enterprise jobs take digest "+run)
	assert.Contains(t, out, "files: blitz enterprise jobs files "+set)
	assert.Contains(t, out, "waits for an answer: run_shell_command make deploy (blitz enterprise jobs approve|deny paused-1)")
	out, err = runCLI(t, "enterprise", "jobs", "runs", "tidy")
	require.NoError(t, err)
	assert.Contains(t, out, "It hasn't run yet.")

	out, err = runCLI(t, "enterprise", "jobs", "run", "tidy")
	require.NoError(t, err)
	assert.Contains(t, out, "✓ tidy: run")

	// A job of a folder that isn't the organization's: push says so.
	here := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(here, ".agents", "workers", "ghost"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(here, ".agents", "workers", "ghost", "WORKER.md"), []byte("---\nruns_on: service\n---\n"), 0o644))
	t.Chdir(here)
	_, err = runCLI(t, "enterprise", "jobs", "runs", "ghost")
	assert.ErrorContains(t, err, `runs no job "ghost"`)
	_, err = runCLI(t, "enterprise", "jobs", "push", "ghost")
	assert.ErrorContains(t, err, "isn't one of the organization's workspaces")

	// A job in a clone of the organization's workspace, not on main yet:
	// the CLI says so, and push puts it there for the service to read.
	org.AddWorkspace(t, "plans", "Plans", "editor")
	org.Mu.Lock()
	pw := org.Workspaces["plans"]
	pw.Sync = false
	org.Workspaces["plans"] = pw
	org.Mu.Unlock()
	home, _ := os.UserHomeDir()
	clone := filepath.Join(home, "Blitz", "Acme Assist", "plans")
	git := func(args ...string) {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-c", "user.name=Ana", "-c", "user.email=ana@acme.com"}, args...)...).CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
	git("clone", "-q", org.Repo("plans"), clone)
	git("-C", clone, "config", "user.name", "Ana")
	git("-C", clone, "config", "user.email", "ana@acme.com")
	worker := func(name, front string) {
		require.NoError(t, os.MkdirAll(filepath.Join(clone, ".agents", "workers", name), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(clone, ".agents", "workers", name, "WORKER.md"), []byte("---\n"+front+"\n---\nDo it.\n"), 0o644))
	}
	worker("fresh", "runs_on: service")
	t.Chdir(clone)
	_, err = runCLI(t, "enterprise", "jobs", "runs", "fresh")
	assert.ErrorContains(t, err, "blitz enterprise jobs push fresh")
	out, err = runCLI(t, "enterprise", "jobs", "push", "fresh")
	require.NoError(t, err)
	assert.Contains(t, out, "✓ fresh is on main and the service has it")

	// A run of it kept its changes on its branch: take adds them to main.
	work := t.TempDir()
	git("clone", "-q", org.Repo("plans"), work)
	git("-C", work, "checkout", "-q", "-b", "run")
	require.NoError(t, os.WriteFile(filepath.Join(work, "recipe.md"), []byte("Soup.\n"), 0o644))
	git("-C", work, "add", "-A")
	git("-C", work, "commit", "-qm", "fresh: run r1")
	git("-C", work, "push", "-q", "origin", "HEAD:refs/heads/blitz/jobs/fresh/r1")
	org.Mu.Lock()
	org.ServiceRuns["job-fresh"] = []enterprisetest.ServiceRun{{ID: "r1", Trigger: "now", Status: "succeeded", Created: time.Now(),
		Branch: "blitz/jobs/fresh/r1", MergeNote: "its job keeps its changes on their branch (merge: never)"}}
	org.Mu.Unlock()
	out, err = runCLI(t, "enterprise", "jobs", "runs", "fresh")
	require.NoError(t, err)
	assert.Contains(t, out, "on its branch only, blitz/jobs/fresh/r1: blitz enterprise jobs take fresh r1\n      its job keeps its changes on their branch (merge: never)")
	out, err = runCLI(t, "enterprise", "jobs", "take", "fresh")
	require.NoError(t, err)
	assert.Contains(t, out, "✓ The changes of fresh's run r1 are on main")
	shown, err := exec.Command("git", "--git-dir", org.Repo("plans"), "show", "main:recipe.md").Output()
	require.NoError(t, err)
	assert.Equal(t, "Soup.\n", string(shown))
	out, err = runCLI(t, "enterprise", "jobs", "take", "fresh")
	require.NoError(t, err)
	assert.Contains(t, out, "is on main already")
	out, err = runCLI(t, "enterprise", "jobs", "runs", "fresh")
	require.NoError(t, err)
	assert.Contains(t, out, "on main (branch: blitz/jobs/fresh/r1)")
	_, err = runCLI(t, "enterprise", "jobs", "take", "fresh", "r9")
	assert.ErrorContains(t, err, "no run r9")
	git("-C", clone, "pull", "-q", "--no-rebase", "origin", "main")
	worker("laptop", "schedule: hourly")
	_, err = runCLI(t, "enterprise", "jobs", "push", "laptop")
	assert.ErrorContains(t, err, "doesn't run it: its WORKER.md doesn't say runs_on: service")
	out, err = runCLI(t, "enterprise", "jobs", "off", "tidy")
	require.NoError(t, err)
	assert.Contains(t, out, "✓ tidy is off.")
	out, err = runCLI(t, "enterprise", "jobs", "on", "tidy")
	require.NoError(t, err)
	assert.Contains(t, out, "✓ tidy is on.")

	out, err = runCLI(t, "enterprise", "jobs", "files", set)
	require.NoError(t, err)
	assert.Contains(t, out, "out/report.csv  4 bytes")
	out, err = runCLI(t, "enterprise", "jobs", "files", set, "--save")
	require.NoError(t, err)
	assert.Contains(t, out, "✓ out/report.csv → ")
	_, err = runCLI(t, "enterprise", "jobs", "files", "urn:blitz:artifacts:"+ws+":"+job+":"+job)
	assert.Error(t, err)
	bare := "urn:blitz:artifacts:" + ws + ":" + job + ":" + ws
	gone := "urn:blitz:artifacts:" + ws + ":" + run + ":" + run
	org.Mu.Lock()
	org.Files[bare] = []enterprisetest.File{}
	org.Files[gone] = []enterprisetest.File{{URN: gone + "/lost.txt", Title: "lost.txt", Size: 1}}
	org.Mu.Unlock()
	out, err = runCLI(t, "enterprise", "jobs", "files", bare)
	require.NoError(t, err)
	assert.Contains(t, out, "It left no files.")
	_, err = runCLI(t, "enterprise", "jobs", "files", gone, "--save")
	assert.Error(t, err, "its content is gone")

	out, err = runCLI(t, "enterprise", "jobs", "pending")
	require.NoError(t, err)
	assert.Contains(t, out, "asks to run_shell_command: make deploy")
	assert.Contains(t, out, "Ready to deploy.")
	out, err = runCLI(t, "enterprise", "jobs", "approve", run, "--note", "Go ahead.")
	require.NoError(t, err)
	assert.Contains(t, out, "✓ Approved: the run goes on")
	_, err = runCLI(t, "enterprise", "jobs", "deny", run)
	assert.ErrorContains(t, err, "isn't waiting")
	out, err = runCLI(t, "enterprise", "jobs", "pending")
	require.NoError(t, err)
	assert.Contains(t, out, "Nothing waits for your answer.")

	// Usage and limits (EN-24).
	out, err = runCLI(t, "enterprise", "usage")
	require.NoError(t, err)
	assert.Contains(t, out, "Today:\n  nothing yet")
	org.Mu.Lock()
	org.Usage = map[string]any{
		"today": map[string]any{"claude": map[string]any{"requests": 3, "input_tokens": 1200, "output_tokens": 300, "cost_usd": 0.05}},
		"month": map[string]any{"claude": map[string]any{"requests": 30, "cost_usd": 1.5}},
		"limits": []any{
			map[string]any{"scope_kind": "person", "period": "day", "unit": "usd", "amount_usd": 5.0, "used": map[string]any{"cost_usd": 0.25}, "resets": "2026-11-03T00:00:00Z"},
			map[string]any{"scope_kind": "group", "period": "month", "unit": "tokens", "model": "claude", "tokens": 1000000, "used": map[string]any{"input_tokens": 1200, "output_tokens": 300}},
		},
		"rate_limits": []any{map[string]any{"model": "claude", "person_rpm": 10, "tpm": 50000}},
		"prompt_logs": true,
	}
	org.Mu.Unlock()
	out, err = runCLI(t, "enterprise", "usage")
	require.NoError(t, err)
	for _, want := range []string{"claude", "3 requests", "$0.0500", "person day budget on every model: $0.25 of $5.00 used ($4.75 left)",
		"group month quota on claude: 1500 of 1000000 tokens used", "10 requests a minute for you, 50000 tokens a minute for everyone",
		"Your organization keeps what's sent to its models"} {
		assert.Contains(t, out, want)
	}

	// Publishing to the catalog (EN-36).
	pubWS := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(pubWS, ".agents", "agents"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pubWS, ".agents", "agents", "writer.md"), []byte("---\nname: writer\ndescription: Writes briefs\n---\nWrite.\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pubWS, ".agents", "agents", "broken.md"), []byte("no front matter"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pubWS, ".agents", "agents", "helper.md"), []byte("---\nname: helper\ndescription: Helps\ngroups: [eng]\n---\nHelp.\n"), 0o644))
	out, err = runCLI(t, "enterprise", "publish", "-d", pubWS)
	require.NoError(t, err)
	assert.Contains(t, out, "agent/writer  (default)")
	assert.Contains(t, out, "agent/broken  (default)\n    ! ")
	assert.Contains(t, out, "agent/helper  (default, for eng)")
	out, err = runCLI(t, "enterprise", "publish", "-d", pubWS, "agent/writer", "--lock", "locked", "--groups", "sales,eng")
	require.NoError(t, err)
	assert.Contains(t, out, "✓ In review: Publish agent writer")
	org.Mu.Lock()
	assert.Contains(t, org.Published["agents/writer.md"], "lock: locked\ngroups: [sales, eng]")
	org.Mu.Unlock()
	_, err = runCLI(t, "enterprise", "publish", "-d", pubWS, "writer")
	assert.ErrorContains(t, err, "<kind>/<name>")
	_, err = runCLI(t, "enterprise", "publish", "-d", pubWS, "agent/broken")
	assert.ErrorContains(t, err, "the catalog would refuse it")
	out, err = runCLI(t, "enterprise", "reviews")
	require.NoError(t, err)
	assert.Contains(t, out, "open  by ana@acme.com: Publish agent writer")
	out, err = runCLI(t, "enterprise", "reviews", "--state", "approved")
	require.NoError(t, err)
	assert.Contains(t, out, "No reviews.")
	org.Mu.Lock()
	org.Reviews = append(org.Reviews, map[string]any{"id": "rev-9", "title": "Publish rule style", "by_email": "bo@acme.com", "paths": []string{"rules/style.md"},
		"problems": []string{"rules/style.md: no front matter"}, "state": "approved", "version": 7, "note": "Looks good.", "created": time.Now()})
	org.Mu.Unlock()
	out, err = runCLI(t, "enterprise", "reviews")
	require.NoError(t, err)
	assert.Contains(t, out, "rev-9  published in version 7  by bo@acme.com: Publish rule style\n    ! rules/style.md: no front matter\n    Looks good.")
	empty := t.TempDir()
	out, err = runCLI(t, "enterprise", "publish", "-d", empty)
	require.NoError(t, err)
	assert.Contains(t, out, "Nothing to publish")

	org.Close()
	for _, args := range [][]string{{"jobs"}, {"jobs", "pending"}, {"jobs", "approve", run}, {"jobs", "files", set}, {"usage"}, {"reviews"}, {"publish", "-d", pubWS, "agent/writer"},
		{"jobs", "runs", "tidy"}, {"jobs", "run", "tidy"}, {"jobs", "on", "tidy"}, {"jobs", "push", "fresh"}, {"jobs", "take", "fresh"}} {
		_, err := runCLI(t, append([]string{"enterprise"}, args...)...)
		assert.Error(t, err, args)
	}
}

// blitz enterprise catalog (EN-34): the organization's items with their
// lock and groups, a default one copied into the workspace and then shown
// as the workspace's own, a locked one refused, and a malformed name.
func TestEnterpriseCatalogCommand(t *testing.T) {
	t.Cleanup(http.DefaultClient.CloseIdleConnections)
	isolate(t)
	secrets.SetDefault(&secrets.Memory{})
	idp := enterprisetest.NewProvider(t)
	org := enterprisetest.NewService(t, idp.URL)
	org.Catalog = map[string]enterprisetest.CatalogFile{
		"agents/reviewer.md": {Kind: "agent", Name: "reviewer", Lock: "locked", Groups: []string{"eng"}, Data: "---\nname: reviewer\n---\nReview."},
		"rules/style.md":     {Kind: "rule", Name: "style", Lock: "default", Data: "Use Go."},
	}
	ws := t.TempDir()
	_, err := runCLI(t, "enterprise", "catalog", "-d", ws)
	assert.Error(t, err, "not connected")
	_, err = runCLI(t, "enterprise", "connect", org.URL)
	require.NoError(t, err)
	out, err := runCLI(t, "enterprise", "catalog", "-d", ws)
	require.NoError(t, err)
	assert.Contains(t, out, "nothing for you yet")
	_, err = runCLI(t, "enterprise", "login")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		out, err = runCLI(t, "enterprise", "catalog", "-d", ws)
		return err == nil && strings.Contains(out, "rule/style")
	}, 5*time.Second, 50*time.Millisecond)
	assert.Contains(t, out, "agent/reviewer  (locked, for eng)")
	assert.Contains(t, out, "rule/style  (default)\n")

	_, err = runCLI(t, "enterprise", "catalog", "copy", "-d", ws, "agent/reviewer")
	assert.ErrorContains(t, err, "locked")
	_, err = runCLI(t, "enterprise", "catalog", "copy", "-d", ws, "style")
	assert.ErrorContains(t, err, "<kind>/<name>")
	out, err = runCLI(t, "enterprise", "catalog", "copy", "-d", ws, "rule/style")
	require.NoError(t, err)
	assert.Contains(t, out, "✓ Copied to .agents/rules/style.md")
	assert.FileExists(t, filepath.Join(ws, ".agents", "rules", "style.md"))
	out, err = runCLI(t, "enterprise", "catalog", "-d", ws, "--kind", "rule")
	require.NoError(t, err)
	assert.Contains(t, out, "rule/style  (default)  this workspace has its own")
	assert.NotContains(t, out, "reviewer")

	// How one hears of one's jobs' runs (JB-31).
	out, err = runCLI(t, "enterprise", "notices")
	require.NoError(t, err)
	assert.Contains(t, out, "Email     on\nWebhook   none\nFor       failures and runs waiting for your answer")
	_, err = runCLI(t, "enterprise", "notices", "--webhook", "http://hooks.example/x")
	assert.ErrorContains(t, err, "https://")
	out, err = runCLI(t, "enterprise", "notices", "--email=false", "--webhook", "https://hooks.example/x", "--every-run")
	require.NoError(t, err)
	assert.Contains(t, out, "Email     off\nWebhook   https://hooks.example/x\nFor       every run")
	org.Mu.Lock()
	assert.Equal(t, map[string]any{"email": false, "webhook": "https://hooks.example/x", "every_run": true}, org.Notices)
	org.Mu.Unlock()

	// The organization's models, with what each can do (EN-21).
	out, err = runCLI(t, "models")
	require.NoError(t, err)
	assert.Contains(t, out, "Smart · the default · tools, images, thinking, 200K context")
	assert.Regexp(t, `enterprise/fast +\$0\.75 in, \$3\.75 out per million tokens +Fast\n`, out)
}

// Why the service hasn't got a job, and what to do, as Studio says it
// (spec_053 EN-42).
func TestMissingText(t *testing.T) {
	for name, c := range map[string]struct {
		m    client.ServiceJobMissing
		want string
	}{
		"outside":     {client.ServiceJobMissing{Reason: "outside", Detail: ".agents/workers/d/WORKER.md"}, "runs jobs from .agents/workers/d/WORKER.md"},
		"not pushed":  {client.ServiceJobMissing{Reason: "not_pushed"}, "blitz enterprise jobs push d"},
		"saving":      {client.ServiceJobMissing{Reason: "saving", SyncState: "saving"}, "sync is putting d"},
		"offline":     {client.ServiceJobMissing{Reason: "saving", SyncState: "offline"}, "can't reach"},
		"signed out":  {client.ServiceJobMissing{Reason: "saving", SyncState: "signed_out"}, "blitz enterprise login"},
		"conflicts":   {client.ServiceJobMissing{Reason: "saving", SyncState: "conflicts"}, "both versions"},
		"refused":     {client.ServiceJobMissing{Reason: "saving", SyncState: "refused", Detail: "remote unpack failed"}, "turned down this workspace's changes, so d isn't on its main: remote unpack failed"},
		"not service": {client.ServiceJobMissing{Reason: "not_service", Detail: "it runs on people's computers"}, "doesn't run it: it runs on people's computers"},
		"not read":    {client.ServiceJobMissing{Reason: "not_read"}, "Try again shortly"},
		"unreadable":  {client.ServiceJobMissing{Reason: "not_read", Detail: "the service is down"}, "yet: the service is down"},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Contains(t, missingText("d", &c.m), c.want)
		})
	}
}

// How a sync stands, in words: never Git's alone.
func TestPrintOrgSync(t *testing.T) {
	for _, tt := range []struct {
		st     client.OrgSync
		want   string
		failed bool
	}{
		{client.OrgSync{State: "saved"}, "✓ Saved: the service has everything\n", false},
		{client.OrgSync{State: "saving"}, "Saving…\n", false},
		{client.OrgSync{State: "offline", Error: "could not resolve host"}, "Not saved: the service can't be reached, so the changes wait on this computer\n  could not resolve host\n", true},
		{client.OrgSync{State: "signed_out"}, "Not saved: signed out (blitz enterprise login)\n", true},
		{client.OrgSync{State: "refused", Error: "remote unpack failed"}, "Not saved: the service refused it\n  remote unpack failed\n", true},
		{client.OrgSync{State: "conflicts", Pairs: [][2]string{{"plan.md", "plan (conflict, Ana).md"}}}, "Saved, with both versions of these files kept:\n  plan.md (yours: plan (conflict, Ana).md)\n", false},
		{client.OrgSync{State: "later"}, "later\n", false},
	} {
		t.Run(tt.st.State, func(t *testing.T) {
			var b strings.Builder
			printOrgSync(&b, tt.st)
			assert.Equal(t, tt.want, b.String())
			assert.Equal(t, tt.failed, failedSync(tt.st.State))
		})
	}
}
