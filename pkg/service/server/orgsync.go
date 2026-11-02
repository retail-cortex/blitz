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
	"context"
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
}

type orgSync struct {
	s      *orgsync.Syncer
	cancel func()
	done   chan struct{}
}

// gitOut runs git in dir with the organization's credential helper, giving
// its output; a failure to sign in is orgsync.ErrSignedOut.
func (s *Server) gitOut(ctx context.Context, dir string, args ...string) ([]byte, error) {
	all := append([]string{"-c", "credential.helper=", "-c", "credential.helper=" + s.gitHelper()}, args...)
	cmd := exec.CommandContext(ctx, "git", all...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	if err != nil {
		msg := ""
		if ee, ok := err.(*exec.ExitError); ok {
			msg = strings.TrimSpace(string(ee.Stderr))
		}
		for _, s := range []string{"Authentication failed", "could not read Username", "terminal prompts disabled", "401"} {
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
		if c.Status().SignedIn {
			if list, err := c.Workspaces(ctx); err == nil {
				cfg, err := config.Load(s.configDir)
				if err == nil {
					folder := s.orgFolder(c, cfg)
					for _, w := range list {
						dir := filepath.Join(folder, w.Handle)
						if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil && w.Sync && !w.Archived && w.Role != "viewer" {
							want[dir] = w
						}
					}
				}
			}
		}
		for dir := range running {
			if _, ok := want[dir]; !ok {
				stop(dir)
			}
		}
		for dir, w := range want {
			if running[dir] == nil {
				r := s.startSync(ctx, c, dir, w.Handle)
				s.ent.syncs.mu.Lock()
				running[dir] = r
				s.ent.syncs.mu.Unlock()
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

// startSync runs a syncer for the clone at dir, and follows its watch
// stream so others' pushes come in as they happen.
func (s *Server) startSync(parent context.Context, c *enterprise.Client, dir, handle string) *orgSync {
	ctx, cancel := context.WithCancel(parent)
	who := c.Status().Person.Name
	if who == "" {
		who, _, _ = strings.Cut(c.Status().Person.Email, "@")
	}
	sy := &orgsync.Syncer{Dir: dir, Git: s.gitOut, Who: who,
		Busy:     func() bool { return s.busyIn(dir) },
		SignedIn: func() bool { return c.Status().SignedIn },
		Quiet:    s.ent.syncQuiet, Poll: s.ent.syncPoll,
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
			err := c.WatchWorkspace(ctx, handle, func(string, string, string) { sy.Remote() })
			if err != nil && ctx.Err() == nil {
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
	key, err := canonical(r.Msg.Workspace)
	if err != nil {
		return ok(&pb.GetOrgSyncResponse{})
	}
	h.s.ent.syncs.mu.Lock()
	defer h.s.ent.syncs.mu.Unlock()
	for dir, rs := range h.s.ent.syncs.running {
		if k, err := canonical(dir); err == nil && k == key {
			st := rs.s.State()
			out := &pb.GetOrgSyncResponse{State: st.State}
			for _, p := range st.Pairs {
				out.Pairs = append(out.Pairs, &pb.OrgSyncPair{Shared: p[0], Mine: p[1]})
			}
			return ok(out)
		}
	}
	return ok(&pb.GetOrgSyncResponse{})
}
