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

// Package orgsync saves and shares an organization workspace with sync on
// (spec_053_enterprise EN-62 to EN-64): Blitz's service, not the window,
// runs it, so it goes on with the window closed. A change on disk starts
// a timer; a few seconds after the last one, and never while a turn runs
// in the workspace, everything is committed and pushed. Others' changes
// are fetched when it starts, when the watch stream names a commit, now
// and then, and before each push, and merged between turns. A file both
// changed keeps both versions: the other person's under its name, this
// person's beside it as "name (conflict, Ana).ext". Its state is said in
// words, never in Git's.
package orgsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// States of a workspace's sync (EN-64).
const (
	Saved     = "saved"
	Saving    = "saving"
	Offline   = "offline"    // its saves wait on this computer
	SignedOut = "signed_out" // sign in to sync
	Conflicts = "conflicts"  // both versions kept
	// Refused is the service turning this person's saves down: it was
	// reached, so it isn't Offline; Err says why, in Git's words.
	Refused = "refused"
)

// refusals are what Git says when the remote was reached and turned a
// push down (beside a push behind others', which sync takes first).
var refusals = []string{"remote unpack failed", "remote rejected", "remote failure", "pre-receive hook declined", "protected branch"}

// State is a workspace's sync now.
type State struct {
	State string
	// Pairs are the files with both versions kept: each the shared one and
	// this person's copy beside it.
	Pairs [][2]string
	// Err is the last failure, in Git's words, for the log.
	Err string
}

// Git runs git with args in dir, with the organization's credential
// helper, and gives its output; a failure's error carries its output.
type Git func(ctx context.Context, dir string, args ...string) ([]byte, error)

// ErrSignedOut is what Git's failure means when Blitz isn't signed in to
// the organization: no credential for its host.
var ErrSignedOut = errors.New("signed out of the organization")

// Syncer keeps one workspace in step.
type Syncer struct {
	Dir string
	Git Git
	// Upstream is the Git remote that's the organization's ("origin"): a
	// folder shared with it keeps its own origin.
	Upstream string
	// Who names the person in the saves' messages ("Ana").
	Who string
	// Busy reports whether a turn is running in the workspace now.
	Busy func() bool
	// SignedIn reports whether Blitz is signed in to the organization.
	SignedIn func() bool
	// Quiet is how long after the last change it saves (5 s); Poll how
	// often it looks for changes (2 s); Fetch how often it fetches with no
	// news from the watch stream (1 min).
	Quiet, Poll, Fetch time.Duration
	// Now is the clock (time.Now).
	Now func() time.Time
	// OnState hears each change of state.
	OnState func(State)

	mu      sync.Mutex
	state   State
	remote  chan struct{}
	asked   chan chan State // SaveNow's, answered with the state after
	status  string          // git status as last seen
	changed time.Time       // when it last changed
}

func (s *Syncer) upstream() string {
	if s.Upstream != "" {
		return s.Upstream
	}
	return "origin"
}

func (s *Syncer) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func or(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// State is the sync's state now.
func (s *Syncer) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *Syncer) set(st State) {
	s.mu.Lock()
	same := s.state.State == st.State && fmt.Sprint(s.state.Pairs) == fmt.Sprint(st.Pairs)
	s.state = st
	s.mu.Unlock()
	if !same && s.OnState != nil {
		s.OnState(st)
	}
}

// Remote says the watch stream named a commit: it's fetched and merged
// between turns.
func (s *Syncer) Remote() {
	s.mu.Lock()
	ch := s.remote
	s.mu.Unlock()
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// ErrNotRunning is SaveNow before Run, or after it ended.
var ErrNotRunning = errors.New("this workspace's sync isn't running")

// ErrBusy is SaveNow while a turn runs: what the agent is changing saves
// when it ends.
var ErrBusy = errors.New("a turn is running in the workspace: its changes save when it ends")

// SaveNow saves what's changed and sends what's waiting at once, rather
// than after the quiet spell or the next minute's try, taking others'
// changes first, and is the state after.
func (s *Syncer) SaveNow(ctx context.Context) (State, error) {
	s.mu.Lock()
	asked := s.asked
	s.mu.Unlock()
	if asked == nil {
		return State{}, ErrNotRunning
	}
	if s.busy() {
		return s.State(), ErrBusy
	}
	done := make(chan State, 1)
	select {
	case asked <- done:
	case <-ctx.Done():
		return State{}, ctx.Err()
	}
	select {
	case st := <-done:
		return st, nil
	case <-ctx.Done():
		return State{}, ctx.Err()
	}
}

// saveNow is SaveNow's save, in Run.
func (s *Syncer) saveNow(ctx context.Context) State {
	s.set(State{State: Saving, Pairs: s.State().Pairs})
	if !s.take(ctx) {
		return s.State()
	}
	_, shared := s.Git(ctx, s.Dir, "rev-parse", "--verify", "-q", s.upstream()+"/main")
	if shared != nil || s.ahead(ctx) {
		s.push(ctx)
	} else if s.State().State == Saving {
		s.settle()
	}
	return s.State()
}

// Run keeps the workspace in step until ctx ends.
func (s *Syncer) Run(ctx context.Context) {
	s.mu.Lock()
	s.remote = make(chan struct{}, 1)
	remote := s.remote
	s.asked = make(chan chan State)
	asked := s.asked
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.asked = nil
		s.mu.Unlock()
	}()
	poll := time.NewTicker(or(s.Poll, 2*time.Second))
	defer poll.Stop()
	fetch := time.NewTicker(or(s.Fetch, time.Minute))
	defer fetch.Stop()
	want := true // others' changes to take, once no turn runs
	for {
		if want && !s.busy() {
			// Others' changes in, then this person's saves out.
			if s.take(ctx) && s.ahead(ctx) {
				s.push(ctx)
			}
			want = false
		}
		if s.due() && !s.busy() {
			s.save(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-remote:
			want = true
		case done := <-asked:
			done <- s.saveNow(ctx)
		case <-fetch.C:
			want = true
		case <-poll.C:
		}
	}
}

func (s *Syncer) busy() bool { return s.Busy != nil && s.Busy() }

// due reports whether there are changes to save, quiet long enough.
func (s *Syncer) due() bool {
	out, err := s.Git(context.Background(), s.Dir, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return false
	}
	st := string(out)
	s.mu.Lock()
	defer s.mu.Unlock()
	if st != s.status {
		s.status, s.changed = st, s.now()
	}
	return st != "" && s.now().Sub(s.changed) >= or(s.Quiet, 5*time.Second)
}

// failed sets the state for err: signed out, refused by the service, else
// offline (its saves wait on this computer).
func (s *Syncer) failed(err error) {
	st := Offline
	switch {
	case (s.SignedIn != nil && !s.SignedIn()) || errors.Is(err, ErrSignedOut):
		st = SignedOut
	case slices.ContainsFunc(refusals, func(r string) bool { return strings.Contains(err.Error(), r) }):
		st = Refused
	}
	s.set(State{State: st, Pairs: s.State().Pairs, Err: err.Error()})
}

// save commits everything and pushes it, taking others' changes first.
func (s *Syncer) save(ctx context.Context) {
	s.set(State{State: Saving, Pairs: s.State().Pairs})
	if err := s.commit(ctx); err != nil {
		s.failed(err)
		return
	}
	s.push(ctx)
}

// commit saves what's changed on this computer, saying who changed what.
func (s *Syncer) commit(ctx context.Context) error {
	if _, err := s.Git(ctx, s.Dir, "add", "-A"); err != nil {
		return err
	}
	files, _ := s.Git(ctx, s.Dir, "diff", "--cached", "--name-only")
	if names := lines(files); len(names) > 0 {
		if _, err := s.Git(ctx, s.Dir, "commit", "-q", "-m", message(s.Who, names)); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.status = ""
	s.mu.Unlock()
	return nil
}

// push sends the saves, taking others' first when the push is refused, up
// to three times.
func (s *Syncer) push(ctx context.Context) {
	var err error
	for range 3 {
		if _, err = s.Git(ctx, s.Dir, "push", "-q", s.upstream(), "HEAD:main"); err == nil {
			s.settle()
			return
		}
		if !s.take(ctx) {
			break
		}
	}
	s.failed(err)
}

// ahead reports whether this computer has saves the service hasn't.
func (s *Syncer) ahead(ctx context.Context) bool {
	out, err := s.Git(ctx, s.Dir, "rev-list", "--count", s.upstream()+"/main..HEAD")
	return err == nil && strings.TrimSpace(string(out)) != "0"
}

// settle is Saved, or Conflicts while both versions of a file are kept.
func (s *Syncer) settle() {
	st := State{State: Saved}
	for _, p := range s.State().Pairs {
		if _, err := os.Stat(filepath.Join(s.Dir, p[1])); err == nil {
			st.Pairs = append(st.Pairs, p)
		}
	}
	if len(st.Pairs) > 0 {
		st.State = Conflicts
	}
	s.set(st)
}

// take fetches and merges others' changes, keeping both versions of a
// file both changed (EN-63); false when it couldn't fetch.
func (s *Syncer) take(ctx context.Context) bool {
	// This person's changes first, so nothing of theirs is in the way.
	if err := s.commit(ctx); err != nil {
		s.failed(err)
		return false
	}
	if _, err := s.Git(ctx, s.Dir, "fetch", "-q", s.upstream()); err != nil {
		s.failed(err)
		return false
	}
	upstream := s.upstream() + "/main"
	if _, err := s.Git(ctx, s.Dir, "rev-parse", "--verify", "-q", upstream); err != nil {
		return true // nothing shared yet
	}
	if _, err := s.Git(ctx, s.Dir, "merge", "-q", "--no-edit", upstream); err == nil {
		if !s.ahead(ctx) {
			s.settle()
		}
		return true
	}
	if _, err := s.Git(ctx, s.Dir, "rev-parse", "-q", "--verify", "MERGE_HEAD"); err != nil {
		s.failed(errors.New("others' changes couldn't be taken"))
		return false
	}
	pairs, err := s.keepBoth(ctx)
	if err != nil {
		_, _ = s.Git(ctx, s.Dir, "merge", "--abort")
		s.failed(err)
		return false
	}
	st := s.State()
	st.Pairs = append(st.Pairs, pairs...)
	s.set(State{State: Conflicts, Pairs: st.Pairs})
	return true
}

// keepBoth ends a merge that stopped on conflicts: the shared version of
// each file under its name, this person's beside it, and a file one
// deleted and the other changed kept as changed.
func (s *Syncer) keepBoth(ctx context.Context) ([][2]string, error) {
	out, err := s.Git(ctx, s.Dir, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	var pairs [][2]string
	for _, name := range lines(out) {
		ours, oursErr := s.Git(ctx, s.Dir, "show", ":2:"+name)
		_, theirsErr := s.Git(ctx, s.Dir, "show", ":3:"+name)
		switch {
		case oursErr != nil && theirsErr != nil: // neither can be read: nothing to go on
			return nil, oursErr
		case theirsErr != nil: // they deleted it; this person changed it
			if _, err := s.Git(ctx, s.Dir, "checkout", "--ours", "--", name); err != nil {
				return nil, err
			}
		case oursErr != nil: // this person deleted it; they changed it
			if _, err := s.Git(ctx, s.Dir, "checkout", "--theirs", "--", name); err != nil {
				return nil, err
			}
		default:
			if _, err := s.Git(ctx, s.Dir, "checkout", "--theirs", "--", name); err != nil {
				return nil, err
			}
			copyName := conflictName(name, s.Who)
			if err := os.WriteFile(filepath.Join(s.Dir, copyName), ours, 0o644); err != nil {
				return nil, err
			}
			pairs = append(pairs, [2]string{name, copyName})
		}
	}
	if _, err := s.Git(ctx, s.Dir, "add", "-A"); err != nil {
		return nil, err
	}
	if _, err := s.Git(ctx, s.Dir, "commit", "-q", "--no-edit", "-m", fmt.Sprintf("%s: both versions kept", s.Who)); err != nil {
		return nil, err
	}
	return pairs, nil
}

// conflictName is this person's copy's name beside the shared one:
// "plan (conflict, Ana).md".
func conflictName(name, who string) string {
	ext := filepath.Ext(name)
	return strings.TrimSuffix(name, ext) + " (conflict, " + who + ")" + ext
}

// message says who changed what (EN-62): "Ana: edited plan.md", or "Ana:
// 4 files".
func message(who string, files []string) string {
	if len(files) == 1 {
		return fmt.Sprintf("%s: edited %s", who, files[0])
	}
	return fmt.Sprintf("%s: %d files", who, len(files))
}

func lines(b []byte) []string {
	var out []string
	for _, l := range bytes.Split(bytes.TrimSpace(b), []byte("\n")) {
		if len(l) > 0 {
			out = append(out, string(l))
		}
	}
	return out
}
