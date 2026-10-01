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

// Package server serves Blitz's API (proto/blitz/v1) over Connect:
// one process holding every workspace a user opens, each an engine.Workspace.
// Handlers translate between the protos and the engine; they hold no
// logic of their own.
package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/engine/workers"
	"github.com/retail-cortex/blitz/pkg/images"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// Opener opens the workspace in dir, an absolute directory. The server
// calls it once per workspace; each call must build its own configuration,
// since a workspace owns and changes it.
type Opener func(ctx context.Context, dir string) (*engine.Workspace, error)

// Server holds the open workspaces and serves the API.
type Server struct {
	open      Opener
	version   string      // reported by GetServiceInfo
	configDir string      // the settings directory (--config; \"\" for ~/.blitz)
	started   time.Time   // when New ran
	program   os.FileInfo // the executable as it was then (nil if unknown)
	logDir    string      // where the diagnostic log is written ("": off)
	sched     *scheduler  // nil: workers aren't run
	broker    *broker
	runs      runs // background runs

	mu         sync.Mutex
	workspaces map[string]*workspace // by canonical directory
	opening    map[string]*opening   // workspaces being opened, by canonical directory
	closed     bool
}

// opening is a workspace being opened; done is closed when w or err is set.
type opening struct {
	done chan struct{}
	w    *workspace
	err  error
}

const (
	// keptImages is how many images a workspace keeps for later turns
	// (each at most images.MaxEncodedBytes); older ones are forgotten.
	keptImages = 16
	// maxRequestBytes bounds a request message: an added image (up to
	// images.DefaultMaxInput) is the largest.
	maxRequestBytes = 32 << 20
)

// workspace is an open workspace and what the server keeps for it.
type workspace struct {
	*engine.Workspace

	mu     sync.Mutex
	images map[string]*images.Image // for Turn.image_ids, by ID
	order  []string                 // images' IDs, least recently used first
}

// New returns a server that opens workspaces with open.
func New(open Opener, opts ...Option) *Server {
	s := &Server{open: open, version: "dev", started: time.Now(), program: programFile(), broker: newBroker(), workspaces: map[string]*workspace{}, opening: map[string]*opening{}}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Handler serves every service.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	limit := connect.WithReadMaxBytes(maxRequestBytes)
	mux.Handle(pb.NewSessionServiceHandler(sessionService{s}, limit))
	mux.Handle(pb.NewWorkspaceServiceHandler(workspaceService{s}, limit))
	mux.Handle(pb.NewWorkerServiceHandler(workerService{s}, limit))
	mux.Handle(pb.NewConfigServiceHandler(configService{s}, limit))
	mux.Handle(pb.NewFileServiceHandler(fileService{s}, limit))
	return mux
}

// Close closes every workspace; none opens after it.
func (s *Server) Close() error {
	if s.sched != nil {
		s.sched.stop() // runs use their workspace until they end
	}
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.stopRuns()
	s.mu.Lock()
	open := s.workspaces
	s.workspaces = map[string]*workspace{}
	s.mu.Unlock()
	var errs []error
	for _, w := range open {
		errs = append(errs, w.Close())
	}
	return errors.Join(errs...)
}

var errServerClosed = errors.New("the service is shutting down")

// canonical is the key a workspace is kept under: its absolute directory
// with symlinks resolved, so two spellings share one workspace.
func canonical(dir string) (string, error) {
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("workspace %q is not an absolute directory", dir)
	}
	real, err := filepath.EvalSymlinks(filepath.Clean(dir))
	if err != nil {
		return "", err
	}
	return real, nil
}

// workspace returns the open workspace for dir, opening it on first use.
func (s *Server) workspace(ctx context.Context, dir string) (*workspace, error) {
	key, err := canonical(dir)
	if err != nil {
		return nil, apiError(connect.CodeInvalidArgument, "INVALID_WORKSPACE", err, "workspace", dir)
	}
	// Opening can be slow (models, MCP servers): other workspaces don't
	// wait for it, and callers for the same one share it.
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, apiError(connect.CodeUnavailable, "SHUTTING_DOWN", errServerClosed)
	}
	if w, ok := s.workspaces[key]; ok {
		s.mu.Unlock()
		return w, nil
	}
	op, ok := s.opening[key]
	if !ok {
		op = &opening{done: make(chan struct{})}
		s.opening[key] = op
		go s.openWorkspace(key, op)
	}
	s.mu.Unlock()
	select {
	case <-op.done:
		return op.w, op.err
	case <-ctx.Done():
		return nil, connect.NewError(connect.CodeCanceled, ctx.Err())
	}
}

// openWorkspace opens the workspace for key and settles op. It doesn't use
// the first caller's context: others may be waiting on it.
func (s *Server) openWorkspace(key string, op *opening) {
	defer close(op.done)
	aw, err := s.open(context.Background(), key)
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.opening, key)
	switch {
	case errors.Is(err, api.ErrWorkspaceBusy):
		op.err = apiError(connect.CodeFailedPrecondition, "WORKSPACE_BUSY", err, "workspace", key)
		return
	case err != nil:
		op.err = apiError(connect.CodeFailedPrecondition, "OPEN_FAILED", err, "workspace", key)
		return
	case s.closed:
		aw.Close()
		op.err = apiError(connect.CodeUnavailable, "SHUTTING_DOWN", errServerClosed)
		return
	}
	// Approvals and questions go to the client running the turn.
	aw.Tools().Hooks().SetApprover(s.broker.approve)
	aw.Tools().Hooks().SetUserPrompter(s.broker.question)
	op.w = &workspace{Workspace: aw, images: map[string]*images.Image{}}
	s.workspaces[key] = op.w
}

// errTurnRunning refuses to close a workspace while a turn runs in it:
// closing would cut the turn off, maybe another client's.
var errTurnRunning = errors.New("a turn is running in this workspace")

// closeWorkspace closes and forgets the workspace for dir, if open.
func (s *Server) closeWorkspace(dir string) error {
	key, err := canonical(dir)
	if err != nil {
		return apiError(connect.CodeInvalidArgument, "INVALID_WORKSPACE", err, "workspace", dir)
	}
	s.mu.Lock()
	w, ok := s.workspaces[key]
	if ok && w.Busy() {
		s.mu.Unlock()
		return apiError(connect.CodeFailedPrecondition, "TURN_RUNNING", errTurnRunning, "workspace", dir)
	}
	delete(s.workspaces, key)
	s.mu.Unlock()
	if !ok {
		return nil
	}
	return w.Close()
}

// openDirs returns the open workspaces' directories, sorted.
func (s *Server) openDirs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for dir := range s.workspaces {
		out = append(out, dir)
	}
	slices.Sort(out)
	return out
}

// keepImage remembers an image for later turns and returns its message.
func (w *workspace) keepImage(img *images.Image) *pb.Image {
	w.mu.Lock()
	w.images[img.SHA256] = img
	w.touchImage(img.SHA256)
	for len(w.order) > keptImages {
		delete(w.images, w.order[0])
		w.order = w.order[1:]
	}
	w.mu.Unlock()
	return imageMsg(img)
}

// image returns a kept image by ID.
func (w *workspace) image(id string) (*images.Image, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	img, ok := w.images[id]
	if !ok {
		return nil, apiError(connect.CodeNotFound, "UNKNOWN_IMAGE", fmt.Errorf("no image %q: load or add it first", id), "id", id)
	}
	w.touchImage(id)
	return img, nil
}

// touchImage makes id the most recently used image. w.mu is held.
func (w *workspace) touchImage(id string) {
	w.order = slices.DeleteFunc(w.order, func(o string) bool { return o == id })
	w.order = append(w.order, id)
}

// apiError is a Connect error with an ErrorInfo detail. kv are metadata
// key-value pairs.
func apiError(code connect.Code, reason string, err error, kv ...string) *connect.Error {
	info := &pb.ErrorInfo{Reason: reason, Message: err.Error()}
	if len(kv) > 0 {
		info.Metadata = map[string]string{}
		for i := 0; i+1 < len(kv); i += 2 {
			info.Metadata[kv[i]] = kv[i+1]
		}
	}
	ce := connect.NewError(code, err)
	if d, derr := connect.NewErrorDetail(info); derr == nil {
		ce.AddDetail(d)
	}
	return ce
}

// toAPI maps internal/app's typed errors to Connect codes and reasons.
func toAPI(err error) error {
	if err == nil {
		return nil
	}
	var ce *connect.Error
	if errors.As(err, &ce) {
		return ce
	}
	var (
		unknownAgent   *api.UnknownAgentError
		resume         *api.ResumeError
		invalidSetting *api.InvalidSettingError
		unknownSetting *api.UnknownSettingError
		blocked        *api.BlockedError
		invalidWorker  *workers.InvalidError
	)
	switch {
	case errors.As(err, &unknownAgent):
		return apiError(connect.CodeNotFound, "UNKNOWN_AGENT", err, "name", unknownAgent.Name)
	case errors.As(err, &resume):
		return apiError(connect.CodeNotFound, "RESUME_FAILED", err)
	case errors.As(err, &invalidSetting):
		return apiError(connect.CodeInvalidArgument, "INVALID_SETTING", err)
	case errors.As(err, &unknownSetting):
		return apiError(connect.CodeInvalidArgument, "UNKNOWN_SETTING", err, "key", unknownSetting.Key)
	case errors.As(err, &blocked):
		return apiError(connect.CodePermissionDenied, "PROMPT_BLOCKED", err, "reason", blocked.Reason)
	case errors.As(err, &invalidWorker):
		return apiError(connect.CodeFailedPrecondition, "WORKER_INVALID", err)
	}
	for _, m := range []struct {
		target error
		code   connect.Code
		reason string
	}{
		{api.ErrNoActiveSession, connect.CodeFailedPrecondition, "NO_ACTIVE_SESSION"},
		{api.ErrMaxTurns, connect.CodeResourceExhausted, "MAX_TURNS"},
		{api.ErrUnknownMode, connect.CodeInvalidArgument, "UNKNOWN_MODE"},
		{api.ErrBadRule, connect.CodeInvalidArgument, "BAD_RULE"},
		{api.ErrUnknownCommand, connect.CodeNotFound, "UNKNOWN_COMMAND"},
		{api.ErrBypassNeedsSandbox, connect.CodeFailedPrecondition, "BYPASS_NEEDS_SANDBOX"},
		{api.ErrCostLimit, connect.CodeResourceExhausted, "COST_LIMIT"},
		{api.ErrTimeLimit, connect.CodeDeadlineExceeded, "TIME_LIMIT"},
		{api.ErrSnapshotNameTaken, connect.CodeAlreadyExists, "SNAPSHOT_NAME_TAKEN"},
		{api.ErrNoNote, connect.CodeNotFound, "NO_NOTE"},
		{api.ErrNoGoal, connect.CodeNotFound, "NO_GOAL"},
		{api.ErrBadModelRef, connect.CodeInvalidArgument, "BAD_MODEL_REF"},
		{api.ErrInvalidAgency, connect.CodeInvalidArgument, "INVALID_AGENCY"},
		{api.ErrUndoConflict, connect.CodeFailedPrecondition, "UNDO_CONFLICT"},
		{api.ErrNothingToUndo, connect.CodeFailedPrecondition, "NOTHING_TO_UNDO"},
		// Workspace.Undo returns the checkpoints' own error for it.
		{tools.ErrNothingToUndo, connect.CodeFailedPrecondition, "NOTHING_TO_UNDO"},
		{api.ErrSteerTooLate, connect.CodeFailedPrecondition, "STEER_TOO_LATE"},
		{api.ErrSessionBusy, connect.CodeFailedPrecondition, "SESSION_BUSY"},
		{api.ErrNotRewindPoint, connect.CodeInvalidArgument, "NOT_REWIND_POINT"},
		{api.ErrCantRewindConversation, connect.CodeFailedPrecondition, "CANT_REWIND_CONVERSATION"},
		{api.ErrUnknownRewindMode, connect.CodeInvalidArgument, "UNKNOWN_REWIND_MODE"},
		{api.ErrScriptsDisabled, connect.CodeFailedPrecondition, "SCRIPTS_DISABLED"},
		{api.ErrUnknownLocale, connect.CodeInvalidArgument, "UNKNOWN_LOCALE"},
		{api.ErrImagesDisabled, connect.CodeFailedPrecondition, "IMAGES_DISABLED"},
		{api.ErrNoFetch, connect.CodeFailedPrecondition, "NO_FETCH"},
		{api.ErrNoSearch, connect.CodeFailedPrecondition, "NO_SEARCH"},
		{api.ErrNothingToCompact, connect.CodeFailedPrecondition, "NOTHING_TO_COMPACT"},
		{api.ErrUnknownWorker, connect.CodeNotFound, "UNKNOWN_WORKER"},
		{api.ErrWorkerNotEnabled, connect.CodeFailedPrecondition, "WORKER_DISABLED"},
		{api.ErrRunInProgress, connect.CodeFailedPrecondition, "RUN_IN_PROGRESS"},
		{api.ErrWorkersDisabled, connect.CodeFailedPrecondition, "WORKERS_DISABLED"},
		{api.ErrHashMismatch, connect.CodeFailedPrecondition, "HASH_MISMATCH"},
		{api.ErrWorkerExists, connect.CodeAlreadyExists, "WORKER_EXISTS"},
		{api.ErrUnknownProcess, connect.CodeNotFound, "UNKNOWN_PROCESS"},
		{api.ErrProjectChanged, connect.CodeFailedPrecondition, "PROJECT_CHANGED"},
		{api.ErrUnknownTask, connect.CodeNotFound, "UNKNOWN_TASK"},
		{api.ErrUnknownRequest, connect.CodeNotFound, "UNKNOWN_REQUEST"},
	} {
		if errors.Is(err, m.target) {
			return apiError(m.code, m.reason, err)
		}
	}
	if errors.Is(err, context.Canceled) {
		return connect.NewError(connect.CodeCanceled, err)
	}
	return apiError(connect.CodeInternal, "INTERNAL", err)
}

// errorInfo is err as an ErrorInfo message, for errors reported inside a
// response rather than as the call's error.
func errorInfo(err error) *pb.ErrorInfo {
	if err == nil {
		return nil
	}
	var ce *connect.Error
	if errors.As(toAPI(err), &ce) {
		for _, d := range ce.Details() {
			if v, derr := d.Value(); derr == nil {
				if info, ok := v.(*pb.ErrorInfo); ok {
					return info
				}
			}
		}
	}
	return &pb.ErrorInfo{Reason: "INTERNAL", Message: err.Error()}
}
