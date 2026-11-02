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
}

// A job's run clones its workspace through the service, runs the job with
// the organization's sign-in for its run, pushes what it changed to
// blitz/<job> and says how it ended (spec_009_jobs JB-10, JB-11).
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
		require.NoError(t, RunJob(ctx, job("r1", "digest"), JobOptions{Config: t.TempDir(), Open: open(true), Dir: filepath.Join(t.TempDir(), "ws")}))
		s.Mu.Lock()
		end := s.Ends["r1"]
		s.Mu.Unlock()
		assert.Equal(t, "succeeded", end["status"])
		assert.Equal(t, "The digest is written.", end["output"])
		assert.Contains(t, git(s.Repo("plans"), "show", "blitz/digest:digest.md"), "# Digest")
		assert.Contains(t, git(s.Repo("plans"), "log", "-1", "--format=%s", "blitz/digest"), "digest: run r1")
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

	t.Run("a secret the service doesn't know", func(t *testing.T) {
		j := job("r4", "digest")
		j.Secret = "wrong"
		err := RunJob(ctx, j, JobOptions{Config: t.TempDir(), Open: open(false)})
		assert.ErrorContains(t, err, "telling the organization's service")
	})
}
