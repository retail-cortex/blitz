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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
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
	j, err = JobFromEnv(with("", ""))
	require.NoError(t, err)
	assert.Equal(t, ana, j.RunAs)
	assert.Equal(t, []string{ws, job, run}, j.Artifacts.IDs)
	for name, c := range map[string]struct{ k, v, err string }{
		"not a person's ID":    {envRunAs, "ana", "isn't a person's ID"},
		"not a name":           {envArtifacts, "artifacts", "isn't this run's artifacts"},
		"another run's":        {envArtifacts, "urn:blitz:artifacts:" + ws + ":" + job + ":" + job, "isn't this run's artifacts"},
		"a job's, not a run's": {envArtifacts, "urn:blitz:artifacts:" + ws + ":" + job, "isn't this run's artifacts"},
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
				require.NoError(t, os.WriteFile(filepath.Join(cfg.Tools.WorkspaceDir, "digest.md"), []byte("# Digest\n"), 0o644))
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

	t.Run("a workspace that doesn't open", func(t *testing.T) {
		broken := func(context.Context, *config.Config, engine.Options) (*engine.Workspace, error) {
			return nil, os.ErrPermission
		}
		assert.ErrorContains(t, RunJob(ctx, job("r6", "digest"), JobOptions{Config: t.TempDir(), Open: broken}), "opening the workspace")
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
