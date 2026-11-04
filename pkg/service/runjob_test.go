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

package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/enterprise"
	"github.com/retail-cortex/blitz/pkg/enterprise/enterprisetest"
	"github.com/retail-cortex/blitz/pkg/urn"
)

func TestJobFromEnv(t *testing.T) {
	full := map[string]string{envService: "https://blitz.acme.com", envRun: "r1", envSecret: "s", envClone: "https://blitz.acme.com/git/plans.git",
		envJob: "digest", envDeadline: "2026-11-02T10:00:00Z"}
	with := func(k, v string) func(string) string {
		return func(name string) string {
			if name == k {
				return v
			}
			return full[name]
		}
	}
	j, err := JobFromEnv(with("", ""))
	require.NoError(t, err)
	assert.Equal(t, Job{Service: "https://blitz.acme.com", Run: "r1", Secret: "s", CloneURL: "https://blitz.acme.com/git/plans.git", Name: "digest",
		Deadline: time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)}, j)
	for name, c := range map[string]struct{ k, v, err string }{
		"no secret":      {envSecret, "", envSecret + " unset"},
		"a path":         {envJob, "../x", "isn't a job's name"},
		"a bad deadline": {envDeadline, "soon", "isn't a time"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := JobFromEnv(with(c.k, c.v))
			assert.ErrorContains(t, err, c.err)
		})
	}
	_, err = JobFromEnv(with(envDeadline, ""))
	assert.NoError(t, err, "no deadline")

	const (
		ws  = "9b2f0c4e-1d2a-4b7e-9f00-1c2d3e4f5a6b"
		job = "51aa7e10-0b6c-4c2a-8d1e-2f3a4b5c6d7e"
		run = "e8030f9a-7b1c-4d2e-a3f4-5b6c7d8e9f01"
		ana = "7c1e0d2a-3b4c-4d5e-8f6a-7b8c9d0e1f2a"
	)
	full[envRun], full[envRunAs], full[envArtifacts] = run, ana, "urn:blitz:artifacts:"+ws+":"+job+":"+run
	full[envResume], full[envAnswer], full[envNote], full[envRequest] = "s-1", "deny", "Not today.", `{"tool":"run_shell_command","detail":"make deploy"}`
	j, err = JobFromEnv(with("", ""))
	require.NoError(t, err)
	assert.Equal(t, ana, j.RunAs)
	assert.Equal(t, "s-1", j.Resume)
	assert.Equal(t, "make deploy", j.Request.Detail)
	assert.Contains(t, resumePrompt(j), "denied your request (run_shell_command: make deploy). They said: \"Not today.\"")
	assert.Equal(t, []string{ws, job, run}, j.Artifacts.IDs)
	for name, c := range map[string]struct{ k, v, err string }{
		"not a person's ID":    {envRunAs, "ana", "isn't a person's ID"},
		"not a name":           {envArtifacts, "artifacts", "isn't this run's artifacts"},
		"another run's":        {envArtifacts, "urn:blitz:artifacts:" + ws + ":" + job + ":" + job, "isn't this run's artifacts"},
		"a job's, not a run's": {envArtifacts, "urn:blitz:artifacts:" + ws + ":" + job, "isn't this run's artifacts"},
		"an answer of neither": {envAnswer, "maybe", "approve or deny"},
		"no request answered":  {envRequest, "{}", "isn't the request answered"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := JobFromEnv(with(c.k, c.v))
			assert.ErrorContains(t, err, c.err)
		})
	}
}

// A job's run clones its workspace through the service, runs the job with
// the organization's sign-in for its run, pushes what it changed to its
// own branch, keeps what it left in out/ and its log, and says how it ended
// (spec_009_jobs JB-10, JB-11, JB-30).
func TestRunJob(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	ctx := context.Background()
	p := enterprisetest.NewProvider(t)
	s := enterprisetest.NewService(t, p.URL)
	s.AddWorkspace(t, "plans", "Plans", "owner")
	git := func(dir string, args ...string) string {
		cmd := exec.Command("git", append([]string{"-c", "user.name=Ana", "-c", "user.email=ana@acme.com"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
		return string(out)
	}
	work := t.TempDir()
	git(work, "clone", "--quiet", s.Repo("plans"), ".")
	require.NoError(t, os.MkdirAll(filepath.Join(work, ".agents", "workers", "digest"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(work, ".agents", "workers", "digest", "WORKER.md"),
		[]byte("---\nschedule: Daily at 6 AM\nruns_on: service\n---\nWrite the digest.\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(work, ".agents", "workers", "odd"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(work, ".agents", "workers", "odd", "WORKER.md"),
		[]byte("---\nschedule: Daily at 6 AM\nruns_on: moon\n---\nOdd.\n"), 0o644))
	git(work, "add", "-A")
	git(work, "commit", "--quiet", "-m", "The digest")
	git(work, "push", "--quiet", "origin", "HEAD:main")

	open := func(change bool) func(context.Context, *config.Config, engine.Options) (*engine.Workspace, error) {
		return func(ctx context.Context, cfg *config.Config, o engine.Options) (*engine.Workspace, error) {
			assert.Equal(t, s.URL, cfg.Enterprise.URL, "the organization's models")
			assert.True(t, o.TrustProject)
			cfg.Session.StorageDir = t.TempDir()
			if change {
				// Each run's its own, so main having another's isn't this one's.
				require.NoError(t, os.WriteFile(filepath.Join(cfg.Tools.WorkspaceDir, "digest.md"), []byte("# Digest\n\n"+cfg.Tools.WorkspaceDir+"\n"), 0o644))
				// What it leaves for people, in the folder it's told of.
				out := cfg.Sandbox.AllowedPaths[len(cfg.Sandbox.AllowedPaths)-1]
				assert.Contains(t, o.AppendSystemPrompt, out)
				require.NoError(t, os.MkdirAll(filepath.Join(out, "charts"), 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(out, "charts", "sales by week.csv"), []byte("week,sales\n"), 0o644))
			}
			o.Model = runtime.NewMockLLM("m", genai.NewContentFromText("The digest is written.", genai.RoleModel))
			return engine.Open(ctx, cfg, o)
		}
	}
	job := func(run, name string) Job {
		s.Mu.Lock()
		s.Runs[run] = "secret-" + run
		s.Mu.Unlock()
		return Job{Service: s.URL, Run: run, Secret: "secret-" + run, CloneURL: s.URL + "/git/plans.git", Name: name, Deadline: time.Now().Add(time.Minute)}
	}

	t.Run("a run that changes things", func(t *testing.T) {
		const (
			ws  = "9b2f0c4e-1d2a-4b7e-9f00-1c2d3e4f5a6b"
			id  = "51aa7e10-0b6c-4c2a-8d1e-2f3a4b5c6d7e"
			run = "e8030f9a-7b1c-4d2e-a3f4-5b6c7d8e9f01"
			ana = "7c1e0d2a-3b4c-4d5e-8f6a-7b8c9d0e1f2a"
		)
		j := job(run, "digest")
		j.RunAs = ana
		var err error
		j.Artifacts, err = urn.New(urn.Artifacts, ws, id, run)
		require.NoError(t, err)
		root := t.TempDir()
		require.NoError(t, RunJob(ctx, j, JobOptions{Config: t.TempDir(), Open: open(true), Root: root}))
		assert.DirExists(t, filepath.Join(root, "jobs", ana, id, run, "workspace"), "its own folder, by IDs")
		s.Mu.Lock()
		end, kept := s.Ends[run], s.Kept[run]
		s.Mu.Unlock()
		assert.Equal(t, "succeeded", end["status"])
		assert.Equal(t, "The digest is written.", end["output"])
		branch := "blitz/jobs/digest/" + run
		assert.Equal(t, branch, end["branch"], "its own branch")
		assert.Contains(t, git(s.Repo("plans"), "show", branch+":digest.md"), "# Digest")
		assert.Contains(t, git(s.Repo("plans"), "log", "-1", "--format=%s", branch), "digest: run "+run)
		assert.NotContains(t, git(s.Repo("plans"), "ls-tree", "-r", "--name-only", branch), "sales by week.csv", "out/ isn't the workspace's")
		assert.Equal(t, true, end["merged"], "on main too, as the job doesn't say otherwise")
		assert.Equal(t, git(s.Repo("plans"), "show", branch+":digest.md"), git(s.Repo("plans"), "show", "main:digest.md"))

		require.Contains(t, kept, "out/charts/sales by week.csv")
		assert.Equal(t, "week,sales\n", kept["out/charts/sales by week.csv"].Body)
		assert.Equal(t, "text/csv; charset=utf-8", kept["out/charts/sales by week.csv"].MediaType)
		require.Contains(t, kept, "events.jsonl")
		assert.Contains(t, kept["events.jsonl"].Body, "The digest is written.")
	})

	t.Run("a run that changes nothing", func(t *testing.T) {
		require.NoError(t, RunJob(ctx, job("r9", "digest"), JobOptions{Config: t.TempDir(), Open: open(false)}))
		s.Mu.Lock()
		defer s.Mu.Unlock()
		assert.Nil(t, s.Ends["r9"]["branch"], "no branch")
		assert.Contains(t, s.Kept["r9"], "events.jsonl")
	})

	t.Run("more files than a run keeps, and one it can't read", func(t *testing.T) {
		crowd := func(ctx context.Context, cfg *config.Config, o engine.Options) (*engine.Workspace, error) {
			out := cfg.Sandbox.AllowedPaths[len(cfg.Sandbox.AllowedPaths)-1]
			for i := range maxKeep + 1 {
				require.NoError(t, os.WriteFile(filepath.Join(out, fmt.Sprintf("f%04d.txt", i)), nil, 0o600))
			}
			require.NoError(t, os.WriteFile(filepath.Join(out, "a-secret.txt"), []byte("x"), 0o000))
			return open(false)(ctx, cfg, o)
		}
		require.NoError(t, RunJob(ctx, job("r11", "digest"), JobOptions{Config: t.TempDir(), Open: crowd}), "its run still succeeds")
		s.Mu.Lock()
		defer s.Mu.Unlock()
		assert.Contains(t, s.Ends["r11"]["note"], "more than 999 files in out/")
		assert.Contains(t, s.Ends["r11"]["note"], "permission denied")
		assert.Contains(t, s.Kept["r11"], "events.jsonl", "its log still kept")
	})

	t.Run("a service that keeps no files", func(t *testing.T) {
		s.Mu.Lock()
		s.KeepRefused = true
		s.Mu.Unlock()
		defer func() { s.Mu.Lock(); s.KeepRefused = false; s.Mu.Unlock() }()
		require.NoError(t, RunJob(ctx, job("r10", "digest"), JobOptions{Config: t.TempDir(), Open: open(true)}), "its run still succeeds")
		s.Mu.Lock()
		defer s.Mu.Unlock()
		assert.Contains(t, s.Ends["r10"]["note"], "some of its files weren't kept")
	})

	t.Run("a job gone from main", func(t *testing.T) {
		err := RunJob(ctx, job("r2", "gone"), JobOptions{Config: t.TempDir(), Open: open(false)})
		assert.ErrorContains(t, err, `has no job "gone"`)
		s.Mu.Lock()
		assert.Equal(t, "failed", s.Ends["r2"]["status"])
		s.Mu.Unlock()
	})

	t.Run("a workspace it can't clone", func(t *testing.T) {
		j := job("r3", "digest")
		j.CloneURL = s.URL + "/git/nowhere.git"
		err := RunJob(ctx, j, JobOptions{Config: t.TempDir(), Open: open(false)})
		assert.ErrorContains(t, err, "cloning the workspace")
	})

	ended := func(run string) map[string]any {
		s.Mu.Lock()
		defer s.Mu.Unlock()
		return s.Ends[run]
	}

	t.Run("a job that doesn't read", func(t *testing.T) {
		assert.ErrorContains(t, RunJob(ctx, job("r5", "odd"), JobOptions{Config: t.TempDir(), Open: open(false)}), `runs_on "moon"`)
		assert.Equal(t, "failed", ended("r5")["status"])
	})

	t.Run("the organization's models, signed in as the run", func(t *testing.T) {
		built := errors.New("it never got there")
		real := func(ctx context.Context, cfg *config.Config, o engine.Options) (*engine.Workspace, error) {
			_, built = runtime.NewModel(ctx, cfg, "enterprise/default")
			return nil, os.ErrPermission
		}
		_ = RunJob(ctx, job("r12", "digest"), JobOptions{Config: t.TempDir(), Open: real})
		assert.NotContains(t, fmt.Sprint(built), "sign in", "a run has no keychain: its secret signs it in")
		assert.NotContains(t, fmt.Sprint(built), "never got there")
	})

	t.Run("a panic", func(t *testing.T) {
		boom := func(context.Context, *config.Config, engine.Options) (*engine.Workspace, error) {
			panic("nil map")
		}
		err := RunJob(ctx, job("r13", "digest"), JobOptions{Config: t.TempDir(), Open: boom})
		require.ErrorContains(t, err, "nil map")
		s.Mu.Lock()
		end := s.Ends["r13"]
		s.Mu.Unlock()
		assert.Equal(t, "failed", end["status"], "told at once, not at the deadline")
	})

	t.Run("a workspace that doesn't open", func(t *testing.T) {
		broken := func(context.Context, *config.Config, engine.Options) (*engine.Workspace, error) {
			return nil, os.ErrPermission
		}
		assert.ErrorContains(t, RunJob(ctx, job("r6", "digest"), JobOptions{Config: t.TempDir(), Open: broken}), "opening the workspace")
	})

	t.Run("a job that keeps its changes on their branch", func(t *testing.T) {
		keep := t.TempDir()
		git(keep, "clone", "--quiet", s.Repo("plans"), ".")
		require.NoError(t, os.WriteFile(filepath.Join(keep, ".agents", "workers", "digest", "WORKER.md"),
			[]byte("---\nschedule: Daily at 6 AM\nruns_on: service\nmerge: never\n---\nWrite the digest.\n"), 0o644))
		git(keep, "commit", "--quiet", "-am", "Keep it on its branch")
		git(keep, "push", "--quiet", "origin", "HEAD:main")
		before := git(s.Repo("plans"), "rev-parse", "main")
		require.NoError(t, RunJob(ctx, job("r14", "digest"), JobOptions{Config: t.TempDir(), Open: open(true)}))
		end := ended("r14")
		assert.Nil(t, end["merged"])
		assert.Contains(t, end["merge_note"], "merge: never")
		assert.Equal(t, before, git(s.Repo("plans"), "rev-parse", "main"), "main as it was")
		git(keep, "revert", "--no-edit", "HEAD")
		git(keep, "push", "--quiet", "origin", "HEAD:main")
	})

	t.Run("main refused: kept on its branch, saying why", func(t *testing.T) {
		hook := filepath.Join(s.Repo("plans"), "hooks", "pre-receive")
		require.NoError(t, os.WriteFile(hook, []byte("#!/bin/sh\nwhile read old new ref; do [ \"$ref\" = refs/heads/main ] && { echo main is protected >&2; exit 1; }; done; exit 0\n"), 0o755))
		defer os.Remove(hook)
		require.NoError(t, RunJob(ctx, job("r15", "digest"), JobOptions{Config: t.TempDir(), Open: open(true)}))
		end := ended("r15")
		assert.Equal(t, "succeeded", end["status"], "its work is on its branch")
		assert.Nil(t, end["merged"])
		assert.Contains(t, end["merge_note"], "pushing main")
	})

	t.Run("a folder it can't use", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(file, nil, 0o600))
		assert.ErrorContains(t, RunJob(ctx, job("r7", "digest"), JobOptions{Config: t.TempDir(), Open: open(false), Root: filepath.Join(file, "root")}),
			"a folder to clone into")
	})

	t.Run("a push the service refuses", func(t *testing.T) {
		hook := filepath.Join(s.Repo("plans"), "hooks", "pre-receive")
		require.NoError(t, os.WriteFile(hook, []byte("#!/bin/sh\necho no >&2\nexit 1\n"), 0o755))
		defer os.Remove(hook)
		err := RunJob(ctx, job("r8", "digest"), JobOptions{Config: t.TempDir(), Open: open(true)})
		assert.ErrorContains(t, err, "its changes weren't pushed")
		assert.Equal(t, "failed", ended("r8")["status"])
		assert.Equal(t, "The digest is written.", ended("r8")["output"], "its output kept")
	})

	t.Run("a secret the service doesn't know", func(t *testing.T) {
		j := job("r4", "digest")
		j.Secret = "wrong"
		err := RunJob(ctx, j, JobOptions{Config: t.TempDir(), Open: open(false)})
		assert.ErrorContains(t, err, "telling the organization's service")
	})
}

// A run that needs more than its job allows pauses (spec_053 EN-43): it
// says what it asks for, keeps its session, and ends; answered yes, the
// same run goes on in that session, allowed that request once.
func TestRunJobPauses(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	ctx := context.Background()
	p := enterprisetest.NewProvider(t)
	s := enterprisetest.NewService(t, p.URL)
	s.AddWorkspace(t, "plans", "Plans", "owner")
	git := func(dir string, args ...string) string {
		cmd := exec.Command("git", append([]string{"-c", "user.name=Ana", "-c", "user.email=ana@acme.com"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
		return string(out)
	}
	work := t.TempDir()
	git(work, "clone", "--quiet", s.Repo("plans"), ".")
	for name, spec := range map[string]string{
		"release": "---\nschedule: Daily at 6 AM\nruns_on: service\n---\nWrite the release notes.\n",
		"careful": "---\nschedule: Daily at 6 AM\nruns_on: service\non_request: refuse\n---\nWrite the release notes.\n",
	} {
		require.NoError(t, os.MkdirAll(filepath.Join(work, ".agents", "workers", name), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(work, ".agents", "workers", name, "WORKER.md"), []byte(spec), 0o644))
	}
	git(work, "add", "-A")
	git(work, "commit", "--quiet", "-m", "Jobs")
	git(work, "push", "--quiet", "origin", "HEAD:main")

	create := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "create_file", Args: map[string]any{"path": "RELEASE.md", "content": "# 2.0\n"}}}}}
	opener := func(responses ...*genai.Content) func(context.Context, *config.Config, engine.Options) (*engine.Workspace, error) {
		return func(ctx context.Context, cfg *config.Config, o engine.Options) (*engine.Workspace, error) {
			o.Model = runtime.NewMockLLM("m", responses...)
			return engine.Open(ctx, cfg, o)
		}
	}
	const (
		ws  = "9b2f0c4e-1d2a-4b7e-9f00-1c2d3e4f5a6b"
		id  = "51aa7e10-0b6c-4c2a-8d1e-2f3a4b5c6d7e"
		run = "e8030f9a-7b1c-4d2e-a3f4-5b6c7d8e9f01"
	)
	job := func(name string) Job {
		s.Mu.Lock()
		s.Runs[run] = "secret"
		s.Mu.Unlock()
		a, err := urn.New(urn.Artifacts, ws, id, run)
		require.NoError(t, err)
		return Job{Service: s.URL, Run: run, Secret: "secret", CloneURL: s.URL + "/git/plans.git", Name: name, Artifacts: a, Deadline: time.Now().Add(time.Minute)}
	}
	root := t.TempDir()

	// It asks to write a file: paused, its session kept.
	require.NoError(t, RunJob(ctx, job("release"), JobOptions{Config: t.TempDir(), Root: root, Open: opener(create, genai.NewContentFromText("Waiting.", genai.RoleModel))}))
	s.Mu.Lock()
	end, kept := s.Ends[run], s.Kept[run]
	s.Mu.Unlock()
	require.Equal(t, "paused", end["status"])
	pending := end["pending"].(map[string]any)
	assert.Equal(t, "create_file", pending["tool"])
	session, _ := end["session"].(string)
	require.NotEmpty(t, session)
	var sessionFiles int
	for path := range kept {
		if strings.HasPrefix(path, "session/") {
			sessionFiles++
		}
	}
	assert.Positive(t, sessionFiles, "its session kept")

	// Approved: the same run goes on in its session, allowed that once.
	resumed := job("release")
	resumed.Resume, resumed.Answer, resumed.Note = session, "approve", "Ship it."
	resumed.Request = &enterprise.Request{Tool: "create_file", Kind: pending["kind"].(string), Detail: pending["detail"].(string)}
	resumeOpen := opener(create, genai.NewContentFromText("Released.", genai.RoleModel))
	require.NoError(t, RunJob(ctx, resumed, JobOptions{Config: t.TempDir(), Root: t.TempDir(), Open: resumeOpen}))
	s.Mu.Lock()
	end = s.Ends[run]
	s.Mu.Unlock()
	assert.Equal(t, "succeeded", end["status"])
	assert.Equal(t, "Released.", end["output"])
	assert.Contains(t, git(s.Repo("plans"), "show", "blitz/jobs/release/"+run+":RELEASE.md"), "# 2.0")

	// Paused again later and denied: it goes on from its own branch.
	again := job("release")
	again.Resume, again.Answer = session, "deny"
	again.Request = resumed.Request
	var sawRelease bool
	fromBranch := func(ctx context.Context, cfg *config.Config, o engine.Options) (*engine.Workspace, error) {
		_, err := os.Stat(filepath.Join(cfg.Tools.WorkspaceDir, "RELEASE.md"))
		sawRelease = err == nil
		return opener(genai.NewContentFromText("Done without it.", genai.RoleModel))(ctx, cfg, o)
	}
	require.NoError(t, RunJob(ctx, again, JobOptions{Config: t.TempDir(), Open: fromBranch}))
	assert.True(t, sawRelease, "its branch, with its changes so far")

	// A run whose session can't be found doesn't go on.
	lost := job("release")
	other, err := urn.New(urn.Artifacts, ws, id, "00000000-0000-4000-8000-000000000000")
	require.NoError(t, err)
	lost.Artifacts, lost.Resume, lost.Answer, lost.Request = other, session, "approve", resumed.Request
	assert.ErrorContains(t, RunJob(ctx, lost, JobOptions{Config: t.TempDir(), Open: opener()}), "its session, to go on")

	// A job that refuses instead: it goes on without, and ends.
	require.NoError(t, RunJob(ctx, job("careful"), JobOptions{Config: t.TempDir(), Open: opener(create, genai.NewContentFromText("Couldn't write it.", genai.RoleModel))}))
	s.Mu.Lock()
	assert.Equal(t, "succeeded", s.Ends[run]["status"], "refused, not paused")
	s.Mu.Unlock()
}
