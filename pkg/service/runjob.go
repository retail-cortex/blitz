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
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/workers"
	"github.com/retail-cortex/blitz/pkg/enterprise"
	"github.com/retail-cortex/blitz/pkg/jobspec"
	"github.com/retail-cortex/blitz/pkg/runmerge"
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
	// Resume is the session a paused run goes on in (spec_053 EN-43), with
	// the answer to what it asked (approve or deny), its note, and the
	// request answered.
	Resume, Answer, Note string
	Request              *enterprise.Request
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
	envResume    = "BLITZ_RESUME"
	envAnswer    = "BLITZ_ANSWER"
	envNote      = "BLITZ_ANSWER_NOTE"
	envRequest   = "BLITZ_REQUEST"
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
	if j.Resume = getenv(envResume); j.Resume != "" {
		if len(j.Artifacts.IDs) == 0 {
			// Its session is kept with its artifacts: without their name it
			// would start again from nothing.
			return j, fmt.Errorf("%s is set without %s: this service is too old to resume a paused run", envResume, envArtifacts)
		}
		j.Answer, j.Note = getenv(envAnswer), getenv(envNote)
		if j.Answer != "approve" && j.Answer != "deny" {
			return j, fmt.Errorf("%s %q: approve or deny", envAnswer, j.Answer)
		}
		var req enterprise.Request
		if err := json.Unmarshal([]byte(getenv(envRequest)), &req); err != nil || req.Tool == "" {
			return j, fmt.Errorf("%s isn't the request answered", envRequest)
		}
		j.Request = &req
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
	end := run.safeRun(ctx)
	// The run's end is told even when its time is up.
	tell, cancel := context.WithTimeout(context.WithoutCancel(ctx), endTimeout)
	defer cancel()
	if err := c.EndRun(tell, end); err != nil {
		return fmt.Errorf("telling the organization's service how the run ended (%s): %w", end.Status, err)
	}
	if end.Status != string(api.RunSucceeded) && end.Status != enterprise.RunPaused {
		return fmt.Errorf("the run %s: %s", end.Status, end.Note)
	}
	return nil
}

// safeRun is run, a panic in it a failed run the service hears of now,
// not when its deadline passes, with the stack in the runner's log.
func (r *jobRun) safeRun(ctx context.Context) (end enterprise.RunEnd) {
	defer func() {
		if p := recover(); p != nil {
			fmt.Fprintf(os.Stderr, "panic: %v\n%s", p, debug.Stack())
			end = failed("Blitz stopped with an error: %v", p)
		}
	}()
	return r.run(ctx)
}

type jobRun struct {
	j        Job
	o        JobOptions
	c        *enterprise.Client
	dir      string // the clone
	out      string // what it leaves for people
	log      string // its events, one JSON a line
	sessions string // its session's folder, kept with it while it waits
	mergeTo  string // where its WORKER.md says its changes go (jobspec.MergeMain or MergeNever)
}

// errPaused ends a run that asked for more than its job allows: it waits
// for its owner's answer (spec_053 EN-43).
var errPaused = errors.New("it waits for its owner's approval")

// sameRequest is whether req is the one answered.
func sameRequest(req api.ApprovalRequest, answered *enterprise.Request) bool {
	return answered != nil && req.Tool == answered.Tool && req.Detail == answered.Detail
}

// resumePrompt tells a paused run's agent how its request was answered.
func resumePrompt(j Job) string {
	what := fmt.Sprintf("%s: %s", j.Request.Tool, j.Request.Detail)
	note := ""
	if j.Note != "" {
		note = fmt.Sprintf(` They said: "%s".`, j.Note)
	}
	if j.Answer == "approve" {
		return fmt.Sprintf("Your job's owner approved your request (%s).%s Do it now, then go on with the job where you left off.", what, note)
	}
	return fmt.Sprintf("Your job's owner denied your request (%s).%s Go on without it: finish what you can, and say what you couldn't do.", what, note)
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
	cmd := exec.CommandContext(ctx, r.o.Git, args...)
	cmd.Dir = r.dir
	// The header goes in git's environment, not its command line, which
	// anyone on the computer can read while git runs.
	cmd.Env = append(gitConfigEnv(os.Environ(), "http.extraHeader", "Authorization: Basic "+basic), "GIT_TERMINAL_PROMPT=0")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		// Never the header: it carries the token.
		return out.Bytes(), fmt.Errorf("git %s: %w: %s", args[0], err, bytes.TrimSpace(out.Bytes()))
	}
	return out.Bytes(), nil
}

// gitConfigEnv is env with key=value added to git's configuration through
// GIT_CONFIG_COUNT, after any settings env has there already.
func gitConfigEnv(env []string, key, value string) []string {
	n := 0
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "GIT_CONFIG_COUNT="); ok {
			n, _ = strconv.Atoi(v)
		}
	}
	return append(env, fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", n, key), fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", n, value), fmt.Sprintf("GIT_CONFIG_COUNT=%d", n+1))
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
	r.sessions = filepath.Join(folder, "sessions")
	for _, d := range []string{r.dir, r.out, r.sessions} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return failed("a folder to clone into: %v", err)
		}
	}
	// A paused run goes on from its own branch, where its changes so far
	// are, with its session as it was.
	from := "main"
	if r.j.Resume != "" {
		if err := r.restore(ctx); err != nil {
			return failed("its session, to go on: %v", err)
		}
		if _, err := r.git(ctx, "ls-remote", "--exit-code", "--heads", r.j.CloneURL, r.branch()); err == nil {
			from = r.branch()
		}
	}
	if _, err := r.git(ctx, "clone", "--quiet", "--branch", from, "--single-branch", r.j.CloneURL, "."); err != nil {
		return failed("cloning the workspace: %v", err)
	}
	cfg, err := config.LoadWorkspace(r.o.Config, r.dir)
	if err != nil {
		return failed("the workspace's settings: %v", err)
	}
	cfg.Tools.WorkspaceDir = r.dir
	cfg.Enterprise.URL = r.j.Service
	cfg.Sandbox.AllowedPaths = append(cfg.Sandbox.AllowedPaths, r.out)
	cfg.Session.StorageDir = r.sessions
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
	var hash, onRequest string
	for _, info := range list {
		if info.Name == r.j.Name {
			hash, onRequest, r.mergeTo = info.Hash, info.OnRequest, info.Merge
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
	events, err := os.OpenFile(r.log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) // a paused run's goes on
	if err != nil {
		return failed("the run's log: %v", err)
	}
	enc := json.NewEncoder(events)
	// What its permissions don't allow: the one request answered yes is
	// allowed, once; anything else pauses it for its owner (EN-43).
	runCtx, stopRun := context.WithCancelCause(ctx)
	defer stopRun(nil)
	var mu sync.Mutex
	var pending *enterprise.Request
	approved := r.j.Answer == "approve"
	var session string
	opts := engine.RunOptions{Manual: true, Session: r.j.Resume,
		OnStart: func(run api.Run) { session = run.SessionID },
		OnRequest: func(_ context.Context, req api.ApprovalRequest) (api.Decision, error) {
			mu.Lock()
			defer mu.Unlock()
			if approved && sameRequest(req, r.j.Request) {
				approved = false
				return api.DecisionOnce, nil
			}
			if pending == nil {
				pending = &enterprise.Request{Tool: req.Tool, Kind: string(req.Kind), Detail: req.Detail}
				stopRun(errPaused)
			}
			return api.DecisionDeny, nil
		}}
	if r.j.Resume != "" {
		opts.Prompt = resumePrompt(r.j)
	}
	if onRequest == "refuse" { // the job's choice: refused, as on a computer
		opts.OnRequest = nil
	}
	opts.OnEvent = func(e api.Event) {
		// The run's output is its last message.
		if t := e.Text; t != nil && !t.Thought && !t.Partial {
			last.Reset()
			last.WriteString(t.Text)
		}
		// Its log is every event but the streamed pieces of a text.
		if e.Text == nil || !e.Text.Partial {
			_ = enc.Encode(e)
		}
	}
	result, err := w.RunWorker(runCtx, r.j.Name, opts)
	_ = events.Close()
	end := enterprise.RunEnd{Status: string(result.Status), Note: result.Error, Output: strings.TrimSpace(last.String()), CostUSD: result.CostUSD}
	switch {
	case errors.Is(context.Cause(runCtx), errPaused):
		mu.Lock()
		end.Status, end.Note, end.Pending, end.Session = enterprise.RunPaused, errPaused.Error(), pending, session
		mu.Unlock()
	case err != nil && result.Status == "":
		return failed("running the job: %v", err)
	case result.Status == api.RunSucceeded:
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		// Its deadline stopped it: said so, whatever the engine called it.
		end.Status, end.Note = string(api.RunStopped), "it ran past its time limit"
	case result.Status == api.RunStopped:
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
	if branch != "" && end.Status == string(api.RunSucceeded) {
		end.Merged, end.MergeNote = r.merge(ctx, branch)
	}
	if err := r.keep(ctx); err != nil {
		end.Note = strings.TrimPrefix(end.Note+"; some of its files weren't kept: "+err.Error(), "; ")
	}
	return end
}

// keep uploads what the run left for people (out/, as out/<path>), its
// log (events.jsonl) and its session (session/<path>, for a paused run to
// go on in, EN-43) as its artifacts (JB-30): at most maxRunFiles in all,
// out/'s files after the others, so the log and the session are never the
// ones left out.
func (r *jobRun) keep(ctx context.Context) error {
	ctx = context.WithoutCancel(ctx) // a run past its time still keeps its files
	ctx, cancel := context.WithTimeout(ctx, keepTimeout)
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
	// files are the regular files under dir (never a link out of the
	// run's folder), at most limit+1 of them.
	files := func(dir string, limit int) ([]string, error) {
		var out []string
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			switch {
			case err != nil:
				return err
			case !d.Type().IsRegular():
				return nil
			case len(out) > limit:
				return fs.SkipAll
			}
			out = append(out, path)
			return nil
		})
		return out, err
	}
	put(r.log, "events.jsonl")
	session, err := files(r.sessions, maxRunFiles-1)
	if err != nil {
		errs = append(errs, err)
	}
	session = session[:min(len(session), maxRunFiles-1)]
	for _, path := range session {
		rel, _ := filepath.Rel(r.sessions, path)
		put(path, "session/"+filepath.ToSlash(rel))
	}
	room := maxRunFiles - 1 - len(session)
	out, err := files(r.out, room)
	if err != nil {
		errs = append(errs, err)
	}
	if len(out) > room {
		out = out[:room]
		errs = append(errs, fmt.Errorf("more than %d files in out/ (a run keeps %d, its log and session among them): the first %d were kept", room, maxRunFiles, room))
	}
	for _, path := range out {
		rel, _ := filepath.Rel(r.out, path)
		put(path, "out/"+filepath.ToSlash(rel))
	}
	return errors.Join(errs...)
}

// maxRunFiles is how many files the service keeps a run, its log and
// session among them (a variable for tests).
var maxRunFiles = 1000

// How long a job's runner waits, past the run's own time limit for all
// but the first (spec_009_jobs JB-10, JB-30).
const (
	endTimeout   = 30 * time.Second // to tell the service how the run ended
	keepTimeout  = 10 * time.Minute // to keep its files
	pushTimeout  = 2 * time.Minute  // to push its changes to their branch
	mergeTimeout = 2 * time.Minute  // to add them to main
)

// push commits what the run changed and pushes it to the run's own branch,
// blitz/jobs/<job>/<run> (JB-10): its name, or "" when nothing changed.
func (r *jobRun) push(ctx context.Context) (string, error) {
	ctx = context.WithoutCancel(ctx) // a run past its time still keeps its work
	ctx, cancel := context.WithTimeout(ctx, pushTimeout)
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
	branch := r.branch()
	if _, err := r.git(ctx, "push", "--quiet", "origin", "HEAD:refs/heads/"+branch); err != nil {
		return "", err
	}
	return branch, nil
}

// merge adds a succeeded run's changes to main as its job says (JB-10):
// whether they're on main, or why not.
func (r *jobRun) merge(ctx context.Context, branch string) (bool, string) {
	if r.mergeTo == jobspec.MergeNever {
		return false, "its job keeps its changes on their branch (merge: never)"
	}
	ctx = context.WithoutCancel(ctx) // a run at its time limit still adds its work
	ctx, cancel := context.WithTimeout(ctx, mergeTimeout)
	defer cancel()
	if err := runmerge.IntoMain(ctx, r.git, "origin", branch, "Blitz job "+r.j.Name); err != nil {
		return false, err.Error()
	}
	return true, ""
}

// branch is the run's own branch.
func (r *jobRun) branch() string { return "blitz/jobs/" + r.j.Name + "/" + r.j.Run }

// restore puts a paused run's session back where it was (its files under
// session/ in the run's artifacts).
func (r *jobRun) restore(ctx context.Context) error {
	items, err := r.c.Children(ctx, r.j.Artifacts.String())
	if err != nil {
		return err
	}
	for _, it := range items {
		n, err := urn.Parse(it.URN)
		if err != nil {
			continue
		}
		rel, ok := strings.CutPrefix(n.Path, "session/")
		if !ok {
			continue
		}
		path := filepath.Join(r.sessions, filepath.FromSlash(rel))
		if !strings.HasPrefix(path, r.sessions+string(filepath.Separator)) {
			continue // never outside its folder
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		body, err := r.c.OpenResource(ctx, n.String())
		if err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err == nil {
			_, err = io.Copy(f, body)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
		}
		body.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
