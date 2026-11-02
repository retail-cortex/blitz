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
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"mime"
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
	"github.com/retail-cortex/blitz/pkg/urn"
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
	// RunAs is the ID of the person the run acts as, its job's owner, and
	// Artifacts the name its files are kept under (spec_055_urns); either
	// may be unknown to an older service.
	RunAs     string
	Artifacts urn.Name
	// Deadline is when the run must have ended.
	Deadline time.Time
}

// The environment a job's runner sets (the service's jobs.Env).
const (
	envService   = "BLITZ_SERVICE"
	envRun       = "BLITZ_RUN"
	envSecret    = "BLITZ_RUN_SECRET"
	envClone     = "BLITZ_CLONE_URL"
	envJob       = "BLITZ_JOB"
	envDeadline  = "BLITZ_DEADLINE"
	envRunAs     = "BLITZ_RUN_AS"
	envArtifacts = "BLITZ_ARTIFACTS"
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
	j.RunAs = getenv(envRunAs)
	if j.RunAs != "" {
		if _, err := urn.New(urn.Users, j.RunAs); err != nil {
			return j, fmt.Errorf("%s %q isn't a person's ID", envRunAs, j.RunAs)
		}
	}
	if a := getenv(envArtifacts); a != "" {
		n, err := urn.Parse(a)
		if err != nil || n.Kind != urn.Artifacts || len(n.IDs) != 3 || n.Path != "" || n.IDs[2] != strings.ToLower(j.Run) {
			return j, fmt.Errorf("%s %q isn't this run's artifacts", envArtifacts, a)
		}
		j.Artifacts = n
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
	// Root is where runs' folders are made (a new temporary one when "",
	// removed after): each run's is jobs/<person>/<job>/<run>/, holding the
	// clone (workspace/) and what it leaves for people (out/).
	Root string
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
// the organization's models, pushes what it changed to its own branch,
// keeps what it left in out/ and its log as its artifacts, and tells the
// service how it ended. The error is why it couldn't, which the
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
	dir string // the clone
	out string // what it leaves for people
	log string // its events, one JSON a line
}

// folder is the run's own: jobs/<person>/<job>/<run> under root, by IDs
// (spec_055_urns), with the job's name when the service gave no IDs.
func (r *jobRun) folder(root string) string {
	person, job := cmp.Or(r.j.RunAs, "owner"), r.j.Name
	if len(r.j.Artifacts.IDs) == 3 {
		job = r.j.Artifacts.IDs[1]
	}
	return filepath.Join(root, "jobs", person, job, filepath.Base(r.j.Run))
}

// outInstructions tell the job's agent where its files for people go.
const outInstructions = `This is an unattended run of a job on your organization's Blitz service. Files you make for people to read or download (reports, data, charts) go in %s: everything there is kept with the run. Changes to the workspace's own files are pushed to a branch for review.`

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
	root := r.o.Root
	if root == "" {
		tmp, err := os.MkdirTemp("", "blitz-job-")
		if err != nil {
			return failed("a folder to clone into: %v", err)
		}
		defer os.RemoveAll(tmp)
		root = tmp
	}
	folder := r.folder(root)
	r.dir, r.out, r.log = filepath.Join(folder, "workspace"), filepath.Join(folder, "out"), filepath.Join(folder, "events.jsonl")
	for _, d := range []string{r.dir, r.out} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return failed("a folder to clone into: %v", err)
		}
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
	cfg.Sandbox.AllowedPaths = append(cfg.Sandbox.AllowedPaths, r.out)
	store, err := workers.OpenStore(filepath.Join(folder, "workers.json"))
	if err != nil {
		return failed("the jobs' record: %v", err)
	}
	w, err := r.o.Open(ctx, cfg, engine.Options{Workers: store, TrustProject: true, AppendSystemPrompt: fmt.Sprintf(outInstructions, r.out),
		Warn: func(msg string) { slog.Warn(msg, "job", r.j.Name) }})
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
	events, err := os.OpenFile(r.log, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return failed("the run's log: %v", err)
	}
	enc := json.NewEncoder(events)
	result, err := w.RunWorker(ctx, r.j.Name, engine.RunOptions{Manual: true, OnEvent: func(e api.Event) {
		// The run's output is its last message.
		if t := e.Text; t != nil && !t.Thought && !t.Partial {
			last.Reset()
			last.WriteString(t.Text)
		}
		// Its log is every event but the streamed pieces of a text.
		if e.Text == nil || !e.Text.Partial {
			_ = enc.Encode(e)
		}
	}})
	_ = events.Close()
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
	branch, err := r.push(ctx)
	if err != nil {
		end.Status = string(api.RunFailed)
		end.Note = strings.TrimSpace(end.Note + "; its changes weren't pushed: " + err.Error())
	}
	end.Branch = branch
	if err := r.keep(ctx); err != nil {
		end.Note = strings.TrimPrefix(end.Note+"; some of its files weren't kept: "+err.Error(), "; ")
	}
	return end
}

// keep uploads what the run left for people (out/, as out/<path>) and its
// log (events.jsonl) as its artifacts (JB-30), at most maxKeep files.
func (r *jobRun) keep(ctx context.Context) error {
	ctx = context.WithoutCancel(ctx) // a run past its time still keeps its files
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	var errs []error
	put := func(path, name string) {
		f, err := os.Open(path)
		if err != nil {
			errs = append(errs, err)
			return
		}
		defer f.Close()
		if err := r.c.KeepArtifact(ctx, name, f, mime.TypeByExtension(filepath.Ext(name))); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	n := 0
	err := filepath.WalkDir(r.out, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case !d.Type().IsRegular():
			return nil // folders, and never a link out of the run's folder
		case n == maxKeep:
			return fmt.Errorf("more than %d files in out/", maxKeep)
		}
		n++
		rel, _ := filepath.Rel(r.out, path)
		put(path, "out/"+filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		errs = append(errs, err)
	}
	put(r.log, "events.jsonl")
	return errors.Join(errs...)
}

// maxKeep is how many of out/'s files a run keeps (the service keeps
// 1,000 a run, its log among them).
const maxKeep = 999

// push commits what the run changed and pushes it to the run's own branch,
// blitz/jobs/<job>/<run> (JB-10): its name, or "" when nothing changed.
func (r *jobRun) push(ctx context.Context) (string, error) {
	ctx = context.WithoutCancel(ctx) // a run past its time still keeps its work
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if _, err := r.git(ctx, "add", "-A"); err != nil {
		return "", err
	}
	if out, err := r.git(ctx, "status", "--porcelain"); err != nil || len(bytes.TrimSpace(out)) == 0 {
		return "", err
	}
	if _, err := r.git(ctx, "-c", "user.name=Blitz job "+r.j.Name, "-c", "user.email=jobs@blitz.invalid",
		"commit", "--quiet", "-m", fmt.Sprintf("%s: run %s", r.j.Name, r.j.Run)); err != nil {
		return "", err
	}
	branch := "blitz/jobs/" + r.j.Name + "/" + r.j.Run
	if _, err := r.git(ctx, "push", "--quiet", "origin", "HEAD:refs/heads/"+branch); err != nil {
		return "", err
	}
	return branch, nil
}
