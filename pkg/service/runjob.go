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
	"bytes"
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/workers"
	"github.com/retail-cortex/blitz/pkg/enterprise"
)

// Job is one run of a job the organization's service runs (spec_009_jobs
// JB-10): what `blitzd run-job` reads from its environment, as the
// service's runner sets it.
type Job struct {
	// Service is the organization's service; Run and Secret the run's ID
	// and its own secret, traded for access tokens as the job's owner.
	Service, Run, Secret string
	// CloneURL is the workspace's, through the service; Name the job's.
	CloneURL, Name string
	// Deadline is when the run must have ended.
	Deadline time.Time
}

// The environment a job's runner sets (the service's jobs.Env).
const (
	envService  = "BLITZ_SERVICE"
	envRun      = "BLITZ_RUN"
	envSecret   = "BLITZ_RUN_SECRET"
	envClone    = "BLITZ_CLONE_URL"
	envJob      = "BLITZ_JOB"
	envDeadline = "BLITZ_DEADLINE"
)

// JobFromEnv is the run getenv describes.
func JobFromEnv(getenv func(string) string) (Job, error) {
	j := Job{Service: getenv(envService), Run: getenv(envRun), Secret: getenv(envSecret), CloneURL: getenv(envClone), Name: getenv(envJob)}
	var missing []string
	for name, v := range map[string]string{envService: j.Service, envRun: j.Run, envSecret: j.Secret, envClone: j.CloneURL, envJob: j.Name} {
		if v == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return j, fmt.Errorf("not a job's run: %s unset", strings.Join(missing, ", "))
	}
	if !workers.ValidName(j.Name) {
		return j, fmt.Errorf("%s %q isn't a job's name", envJob, j.Name)
	}
	if d := getenv(envDeadline); d != "" {
		t, err := time.Parse(time.RFC3339, d)
		if err != nil {
			return j, fmt.Errorf("%s %q isn't a time", envDeadline, d)
		}
		j.Deadline = t
	}
	return j, nil
}

// JobOptions are how RunJob runs.
type JobOptions struct {
	// Config is the configuration's directory ("" for the usual).
	Config string
	// Dir is where the workspace is cloned (a new temporary one when "").
	Dir string
	// Git is the git program ("git").
	Git string
	// Open opens the clone's workspace (engine.Open, trusting it: it's the
	// organization's).
	Open func(ctx context.Context, cfg *config.Config, o engine.Options) (*engine.Workspace, error)
	// Client names the runner to the service.
	Client string
}

// RunJob runs one job's run (JB-10, JB-11): it clones the workspace
// through the service as the job's owner, runs the job unattended with
// the organization's models, pushes what it changed to blitz/<job>, and
// tells the service how it ended. The error is why it couldn't, which the
// service has been told too when it could be.
func RunJob(ctx context.Context, j Job, o JobOptions) error {
	if !j.Deadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, j.Deadline)
		defer cancel()
	}
	if o.Git == "" {
		o.Git = "git"
	}
	if o.Open == nil {
		o.Open = engine.Open
	}
	c := enterprise.NewRun(j.Service, j.Run, j.Secret, enterprise.Options{Client: o.Client})
	enterprise.Register(c) // the organization's models find it by the service's address
	run := &jobRun{j: j, o: o, c: c}
	end := run.run(ctx)
	// The run's end is told even when its time is up.
	tell, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := c.EndRun(tell, end); err != nil {
		return fmt.Errorf("telling the organization's service how the run ended (%s): %w", end.Status, err)
	}
	if end.Status != string(api.RunSucceeded) {
		return fmt.Errorf("the run %s: %s", end.Status, end.Note)
	}
	return nil
}

type jobRun struct {
	j   Job
	o   JobOptions
	c   *enterprise.Client
	dir string
}

// git runs git in the clone with the run's access token, for the service.
func (r *jobRun) git(ctx context.Context, args ...string) ([]byte, error) {
	token, err := r.c.AccessToken(ctx)
	if err != nil {
		return nil, err
	}
	basic := base64.StdEncoding.EncodeToString([]byte("x:" + token))
	cmd := exec.CommandContext(ctx, r.o.Git, append([]string{"-c", "http.extraHeader=Authorization: Basic " + basic}, args...)...)
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		// Never the header: it carries the token.
		return out.Bytes(), fmt.Errorf("git %s: %w: %s", args[0], err, bytes.TrimSpace(out.Bytes()))
	}
	return out.Bytes(), nil
}

func failed(format string, a ...any) enterprise.RunEnd {
	return enterprise.RunEnd{Status: string(api.RunFailed), Note: fmt.Sprintf(format, a...)}
}

func (r *jobRun) run(ctx context.Context) enterprise.RunEnd {
	r.dir = r.o.Dir
	if r.dir == "" {
		tmp, err := os.MkdirTemp("", "blitz-job-")
		if err != nil {
			return failed("a folder to clone into: %v", err)
		}
		defer os.RemoveAll(tmp)
		r.dir = filepath.Join(tmp, "workspace")
	}
	if err := os.MkdirAll(r.dir, 0o700); err != nil {
		return failed("a folder to clone into: %v", err)
	}
	if _, err := r.git(ctx, "clone", "--quiet", "--branch", "main", "--single-branch", r.j.CloneURL, "."); err != nil {
		return failed("cloning the workspace: %v", err)
	}
	cfg, err := config.LoadWorkspace(r.o.Config, r.dir)
	if err != nil {
		return failed("the workspace's settings: %v", err)
	}
	cfg.Tools.WorkspaceDir = r.dir
	cfg.Enterprise.URL = r.j.Service
	store, err := workers.OpenStore(filepath.Join(filepath.Dir(r.dir), "workers.json"))
	if err != nil {
		return failed("the jobs' record: %v", err)
	}
	w, err := r.o.Open(ctx, cfg, engine.Options{Workers: store, TrustProject: true, Warn: func(msg string) { slog.Warn(msg, "job", r.j.Name) }})
	if err != nil {
		return failed("opening the workspace: %v", err)
	}
	defer w.Close()
	list, err := w.ListWorkers()
	if err != nil {
		return failed("reading the workspace's jobs: %v", err)
	}
	var hash string
	for _, info := range list {
		if info.Name == r.j.Name {
			hash = info.Hash
			if len(info.Problems) > 0 {
				return failed("the job doesn't read: %s", strings.Join(info.Problems, "; "))
			}
		}
	}
	if hash == "" {
		return failed("the workspace's main has no job %q", r.j.Name)
	}
	if _, err := w.EnableWorker(r.j.Name, hash); err != nil {
		return failed("the job: %v", err)
	}
	var last strings.Builder
	result, err := w.RunWorker(ctx, r.j.Name, engine.RunOptions{Manual: true, OnEvent: func(e api.Event) {
		// The run's output is its last message.
		if t := e.Text; t != nil && !t.Thought && !t.Partial {
			last.Reset()
			last.WriteString(t.Text)
		}
	}})
	end := enterprise.RunEnd{Status: string(result.Status), Note: result.Error, Output: strings.TrimSpace(last.String()), CostUSD: result.CostUSD}
	switch {
	case err != nil && result.Status == "":
		return failed("running the job: %v", err)
	case result.Status == api.RunSucceeded, result.Status == api.RunStopped:
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		end.Status, end.Note = string(api.RunStopped), "it ran past its time limit"
	default:
		end.Status = string(api.RunFailed)
		end.Note = strings.TrimSpace(cmp.Or(result.Error, string(result.Status)))
	}
	if err := r.push(ctx); err != nil {
		end.Status = string(api.RunFailed)
		end.Note = strings.TrimSpace(end.Note + "; its changes weren't pushed: " + err.Error())
	}
	return end
}

// push commits what the run changed and pushes it to blitz/<job> (JB-10),
// replacing the branch's last run's.
func (r *jobRun) push(ctx context.Context) error {
	ctx = context.WithoutCancel(ctx) // a run past its time still keeps its work
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if _, err := r.git(ctx, "add", "-A"); err != nil {
		return err
	}
	if out, err := r.git(ctx, "status", "--porcelain"); err != nil || len(bytes.TrimSpace(out)) == 0 {
		return err
	}
	if _, err := r.git(ctx, "-c", "user.name=Blitz job "+r.j.Name, "-c", "user.email=jobs@blitz.invalid",
		"commit", "--quiet", "-m", fmt.Sprintf("%s: run %s", r.j.Name, r.j.Run)); err != nil {
		return err
	}
	_, err := r.git(ctx, "push", "--quiet", "--force", "origin", "HEAD:refs/heads/blitz/"+r.j.Name)
	return err
}
