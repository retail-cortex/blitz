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

package server

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/enterprise"
	"github.com/retail-cortex/blitz/pkg/orgsync"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// orgSyncs are the organization workspaces kept in step on this computer
// (spec_053_enterprise EN-62): one syncer for each cloned workspace with
// sync on, while signed in, following its watch stream.
type orgSyncs struct {
	mu      sync.Mutex
	running map[string]*orgSync // by the clone's folder
	kick    chan struct{}
	// told are the pairs of files with both versions each workspace's agent
	// has been told of (EN-63), by folder: told once, on the next turn.
	told map[string]map[[2]string]bool
}

type orgSync struct {
	s      *orgsync.Syncer
	cancel func()
	done   chan struct{}
}

// asPerson is git's options for committing as the person signed in to the
// organization: the saves are theirs, and a computer with no name and
// email set for Git couldn't commit them at all.
func asPerson(p enterprise.Person) []string {
	return []string{"-c", "user.name=" + cmp.Or(p.Name, p.Email, "Blitz"), "-c", "user.email=" + cmp.Or(p.Email, "blitz@localhost")}
}

// gitAs is gitOut committing as the person signed in to c.
func (s *Server) gitAs(c *enterprise.Client) orgsync.Git {
	return func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		return s.gitOut(ctx, dir, append(asPerson(c.Status().Person), args...)...)
	}
}

// signedOutSays are what git says when the service didn't take the
// person's sign-in.
var signedOutSays = []string{"Authentication failed", "could not read Username", "terminal prompts disabled", "returned error: 401", "HTTP 401"}

// gitOut runs git in dir with the organization's credential helper, giving
// its output; a failure to sign in is orgsync.ErrSignedOut.
func (s *Server) gitOut(ctx context.Context, dir string, args ...string) ([]byte, error) {
	all := append([]string{"-c", "credential.helper=", "-c", "credential.helper=" + s.gitHelper()}, args...)
	cmd := exec.CommandContext(ctx, "git", all...)
	cmd.Dir = dir
	// Git's messages in English, as they're matched below (LANGUAGE is
	// gettext's, ahead of LC_ALL).
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C", "LANGUAGE=C")
	out, err := cmd.Output()
	if err != nil {
		msg := ""
		if ee, ok := err.(*exec.ExitError); ok {
			msg = strings.TrimSpace(string(ee.Stderr))
		}
		for _, s := range signedOutSays {
			if strings.Contains(msg, s) {
				return out, fmt.Errorf("%w: %s", orgsync.ErrSignedOut, msg)
			}
		}
		return out, fmt.Errorf("%w: %s", err, msg)
	}
	return out, nil
}

// busyIn reports whether a turn runs in the workspace at dir, if it's open.
func (s *Server) busyIn(dir string) bool {
	key, err := canonical(dir)
	if err != nil {
		return false
	}
	s.mu.Lock()
	w := s.workspaces[key]
	s.mu.Unlock()
	return w != nil && w.Busy()
}

// runSyncs keeps a syncer for each cloned sync workspace of c's while
// signed in, until ctx ends, looking again every syncEvery and when kicked.
func (s *Server) runSyncs(ctx context.Context, c *enterprise.Client, kick <-chan struct{}) {
	every := s.ent.syncEvery
	if every == 0 {
		every = 30 * time.Second
	}
	running := map[string]*orgSync{}
	s.ent.syncs.mu.Lock()
	s.ent.syncs.running = running
	s.ent.syncs.mu.Unlock()
	stop := func(dir string) {
		r := running[dir]
		r.cancel()
		<-r.done
		s.ent.syncs.mu.Lock()
		delete(running, dir)
		s.ent.syncs.mu.Unlock()
	}
	defer func() {
		for dir := range running {
			stop(dir)
		}
	}()
	for {
		want := map[string]enterprise.Workspace{}
		remotes := map[string]string{}
		// Known: signed out (none wanted), or the list and the settings
		// read. A list or settings that can't be read now (the service
		// slow, a network blip) leaves the syncs running as they are:
		// stopping them all on one failed request left every workspace
		// unsynced until the next look.
		known := !c.Status().SignedIn
		if !known {
			if list, err := c.Workspaces(ctx); err == nil {
				if cfg, err := config.Load(s.configDir); err == nil {
					known = true
					folder := s.orgFolder(c, cfg)
					shared := sharedFor(c.URL())
					for _, w := range list {
						dir, remote := orgDir(shared, folder, w.Handle)
						if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil && w.Sync && !w.Archived && w.Role != "viewer" {
							want[dir], remotes[dir] = w, remote
						}
					}
				} else {
					slog.Debug("the organization's workspaces to sync: the settings", "error", err)
				}
			} else if ctx.Err() == nil {
				slog.Debug("the organization's workspaces to sync", "error", err)
			}
		}
		if known {
			for dir := range running {
				if _, ok := want[dir]; !ok {
					stop(dir)
				}
			}
			for dir, w := range want {
				if running[dir] == nil {
					r := s.startSync(ctx, c, dir, w.Handle, remotes[dir])
					s.ent.syncs.mu.Lock()
					running[dir] = r
					s.ent.syncs.mu.Unlock()
				}
			}
		}
		t := time.NewTimer(every)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-kick:
		case <-t.C:
		}
		t.Stop()
	}
}

// trackShared has the branch checked out in a folder shared in place
// follow the organization's main, when it follows nothing: so Changes can
// pull from it and count what's ahead and behind (a folder shared before
// this was done gets it as its sync starts). A branch that follows another
// remote is left as it is.
func trackShared(ctx context.Context, dir, remote string) {
	if remote == "" || remote == "origin" {
		return
	}
	if _, err := gitText(ctx, dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); err == nil {
		return
	}
	if _, err := gitText(ctx, dir, "rev-parse", "--verify", "-q", remote+"/main"); err != nil {
		return
	}
	if _, err := gitText(ctx, dir, "branch", "--set-upstream-to="+remote+"/main"); err != nil {
		slog.Debug("a shared folder's branch following the organization's main", "dir", dir, "error", err)
	}
}

// startSync runs a syncer for the clone at dir, and follows its watch
// stream so others' pushes come in as they happen.
func (s *Server) startSync(parent context.Context, c *enterprise.Client, dir, handle, remote string) *orgSync {
	trackShared(parent, dir, remote)
	// The folder's helper is this Blitz's, as Changes trusts: a folder
	// shared by a Blitz since moved has another's.
	if err := s.setHelper(parent, dir, c.URL()); err != nil {
		slog.Debug("a shared folder's credential helper", "dir", dir, "error", err)
	}
	ctx, cancel := context.WithCancel(parent)
	who := c.Status().Person.Name
	if who == "" {
		who, _, _ = strings.Cut(c.Status().Person.Email, "@")
	}
	sy := &orgsync.Syncer{Dir: dir, Git: s.gitAs(c), Who: who, Upstream: remote,
		Busy:     func() bool { return s.busyIn(dir) },
		SignedIn: func() bool { return c.Status().SignedIn },
		Quiet:    s.ent.syncQuiet, Poll: s.ent.syncPoll,
		// Each change of state is logged, a failure with Git's words: the
		// window says it in short.
		OnState: func(st orgsync.State) {
			if st.Err != "" {
				slog.Warn("an organization workspace's sync", "workspace", handle, "dir", dir, "state", st.State, "error", st.Err)
			} else {
				slog.Debug("an organization workspace's sync", "workspace", handle, "state", st.State)
			}
		},
	}
	r := &orgSync{s: sy, cancel: cancel, done: make(chan struct{})}
	var wg sync.WaitGroup
	wg.Go(func() { sy.Run(ctx) })
	wg.Go(func() {
		retry := s.ent.retry
		if retry == 0 {
			retry = 30 * time.Second
		}
		for ctx.Err() == nil {
			// It always ends with an error: the stream ended, or why.
			err := c.WatchWorkspace(ctx, handle, func(string, string, string) { sy.Remote() })
			if ctx.Err() == nil {
				slog.Debug("an organization workspace's stream", "workspace", handle, "error", err)
			}
			select {
			case <-ctx.Done():
			case <-time.After(retry):
				sy.Remote() // whatever was missed meanwhile
			}
		}
	})
	go func() { wg.Wait(); close(r.done) }()
	return r
}

func (h configService) GetOrgSync(_ context.Context, r req[pb.GetOrgSyncRequest]) (*connect.Response[pb.GetOrgSyncResponse], error) {
	sy := h.s.syncer(r.Msg.Workspace)
	if sy == nil {
		return ok(&pb.GetOrgSyncResponse{})
	}
	state, why, pairs := syncMsg(sy.State())
	return ok(&pb.GetOrgSyncResponse{State: state, Error: why, Pairs: pairs, Failed: orgsync.Failed(state)})
}

func (h configService) SaveOrgSync(ctx context.Context, r req[pb.SaveOrgSyncRequest]) (*connect.Response[pb.SaveOrgSyncResponse], error) {
	sy := h.s.syncer(r.Msg.Workspace)
	if sy == nil {
		return nil, apiError(connect.CodeFailedPrecondition, "NOT_SYNCED", errors.New("no sync keeps this workspace in step with an organization"))
	}
	st, err := sy.SaveNow(ctx)
	switch {
	case errors.Is(err, orgsync.ErrBusy):
		return nil, apiError(connect.CodeFailedPrecondition, "GIT_BUSY", err)
	case errors.Is(err, orgsync.ErrNotRunning):
		return nil, apiError(connect.CodeFailedPrecondition, "NOT_SYNCED", err)
	case err != nil:
		return nil, err
	}
	state, why, pairs := syncMsg(st)
	return ok(&pb.SaveOrgSyncResponse{State: state, Error: why, Pairs: pairs, Failed: orgsync.Failed(state)})
}

// syncMsg is a sync's state for the window: why it isn't saved, in Git's
// words, only when it isn't.
func syncMsg(st orgsync.State) (state, why string, pairs []*pb.OrgSyncPair) {
	if st.State == orgsync.Offline || st.State == orgsync.Refused || st.State == orgsync.SignedOut || st.State == orgsync.Secrets {
		why = st.Err
	}
	for _, p := range st.Pairs {
		pairs = append(pairs, &pb.OrgSyncPair{Shared: p[0], Mine: p[1]})
	}
	return st.State, why, pairs
}

// syncer is the syncer keeping the workspace at dir in step (nil when
// none does).
func (s *Server) syncer(dir string) *orgsync.Syncer {
	key, err := canonical(dir)
	if err != nil {
		return nil
	}
	s.ent.syncs.mu.Lock()
	defer s.ent.syncs.mu.Unlock()
	for d, rs := range s.ent.syncs.running {
		if k, err := canonical(d); err == nil && k == key {
			return rs.s
		}
	}
	return nil
}

// conflictNote is what the next turn's agent in dir is told of the files
// whose two versions sync kept (EN-63) that it hasn't been told of yet:
// "" when there are none.
func (s *Server) conflictNote(dir string) string {
	key, err := canonical(dir)
	if err != nil {
		return ""
	}
	s.ent.syncs.mu.Lock()
	defer s.ent.syncs.mu.Unlock()
	var pairs [][2]string
	for d, rs := range s.ent.syncs.running {
		if k, err := canonical(d); err == nil && k == key {
			pairs = rs.s.State().Pairs
		}
	}
	if s.ent.syncs.told == nil {
		s.ent.syncs.told = map[string]map[[2]string]bool{}
	}
	told := s.ent.syncs.told[key]
	if told == nil {
		told = map[[2]string]bool{}
		s.ent.syncs.told[key] = told
	}
	var b strings.Builder
	for _, p := range pairs {
		if told[p] {
			continue
		}
		told[p] = true
		fmt.Fprintf(&b, "- %s (the shared version) and %s (this person's)\n", p[0], p[1])
	}
	if b.Len() == 0 {
		return ""
	}
	return "<workspace-sync>\nThis workspace is kept in step with others in the organization. Someone else changed these files at the same time, so both versions were kept:\n" +
		b.String() + "When your task touches one, merge this person's changes into the shared file and delete the copy; otherwise leave them, and mention them once.\n</workspace-sync>"
}
