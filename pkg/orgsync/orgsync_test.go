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

package orgsync

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gitAs runs git as who, failing as ErrSignedOut when signedOut says so.
func gitAs(who string, signedOut *atomic.Bool) Git {
	return func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		if signedOut != nil && signedOut.Load() && len(args) > 0 && (args[0] == "fetch" || args[0] == "push") {
			return nil, ErrSignedOut
		}
		cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "user.name=" + who, "-c", "user.email=" + strings.ToLower(who) + "@acme.com"}, args...)...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		out, err := cmd.Output()
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				return out, &gitErr{string(ee.Stderr)}
			}
			return out, err
		}
		return out, nil
	}
}

type gitErr struct{ s string }

func (e *gitErr) Error() string { return e.s }

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitAs("Setup", nil)(context.Background(), dir, args...)
	require.NoError(t, err, "git %v", args)
	return string(out)
}

// shared is a workspace's repository with a first commit, and two people's
// clones of it.
func shared(t *testing.T) (bare, ana, bo string) {
	root := t.TempDir()
	bare, seed := filepath.Join(root, "plans.git"), filepath.Join(root, "seed")
	run(t, root, "init", "-q", "--bare", "-b", "main", bare)
	run(t, root, "init", "-q", "-b", "main", seed)
	require.NoError(t, os.WriteFile(filepath.Join(seed, "plan.md"), []byte("# Plan\n"), 0o644))
	run(t, seed, "add", "-A")
	run(t, seed, "commit", "-q", "-m", "First")
	run(t, seed, "push", "-q", bare, "main")
	ana, bo = filepath.Join(root, "ana"), filepath.Join(root, "bo")
	run(t, root, "clone", "-q", bare, ana)
	run(t, root, "clone", "-q", bare, bo)
	return bare, ana, bo
}

// start runs a syncer for who in dir until the test ends.
func start(t *testing.T, who, dir string, busy *atomic.Bool, signedOut *atomic.Bool) *Syncer {
	s := &Syncer{Dir: dir, Git: gitAs(who, signedOut), Who: who, Quiet: 50 * time.Millisecond, Poll: 20 * time.Millisecond, Fetch: time.Hour,
		Busy: func() bool { return busy != nil && busy.Load() }, SignedIn: func() bool { return signedOut == nil || !signedOut.Load() }}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return s
}

func eventually(t *testing.T, f func() bool, msg string) {
	t.Helper()
	require.Eventually(t, f, 10*time.Second, 20*time.Millisecond, msg)
}

func read(dir, name string) string {
	b, _ := os.ReadFile(filepath.Join(dir, name))
	return string(b)
}

// A change is saved a moment after the last one, never mid-turn, and
// reaches the others when they hear of it (EN-62).
func TestSync(t *testing.T) {
	bare, ana, bo := shared(t)
	var anaBusy atomic.Bool
	anaSync := start(t, "Ana", ana, &anaBusy, nil)
	boSync := start(t, "Bo", bo, nil, nil)
	eventually(t, func() bool { return anaSync.State().State == Saved && boSync.State().State == Saved }, "started: in step")

	anaBusy.Store(true)
	require.NoError(t, os.WriteFile(filepath.Join(ana, "plan.md"), []byte("# Plan\n\nShip it.\n"), 0o644))
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, "First", strings.TrimSpace(run(t, bare, "log", "-1", "--format=%s", "main")), "not mid-turn")
	anaBusy.Store(false)
	if !assert.Eventually(t, func() bool {
		return strings.TrimSpace(run(t, bare, "log", "-1", "--format=%s", "main")) == "Ana: edited plan.md"
	}, 10*time.Second, 20*time.Millisecond) {
		t.Fatalf("state %+v; ana log %s; bare log %s", anaSync.State(), run(t, ana, "log", "--format=%s"), run(t, bare, "log", "--format=%s", "main"))
	}
	eventually(t, func() bool { return anaSync.State().State == Saved }, "saved")

	require.NoError(t, os.WriteFile(filepath.Join(ana, "a.md"), []byte("a"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(ana, "b.md"), []byte("b"), 0o644))
	eventually(t, func() bool {
		return strings.TrimSpace(run(t, bare, "log", "-1", "--format=%s", "main")) == "Ana: 2 files"
	}, "two files, one save")

	boSync.Remote()
	eventually(t, func() bool { return strings.Contains(read(bo, "plan.md"), "Ship it.") && read(bo, "a.md") == "a" }, "Bo has Ana's changes")
}

// A file both changed keeps both versions: the shared one under its name,
// this person's beside it; one deleted by one and changed by the other
// stays changed (EN-63).
func TestConflicts(t *testing.T) {
	bare, ana, bo := shared(t)
	require.NoError(t, os.WriteFile(filepath.Join(bo, "notes.md"), []byte("notes"), 0o644))
	run(t, bo, "add", "-A")
	run(t, bo, "commit", "-q", "-m", "notes")
	run(t, bo, "push", "-q", "origin", "main")
	run(t, ana, "pull", "-q")

	require.NoError(t, os.WriteFile(filepath.Join(ana, "plan.md"), []byte("Ana's plan"), 0o644))
	require.NoError(t, os.Remove(filepath.Join(ana, "notes.md")))
	run(t, ana, "add", "-A")
	run(t, ana, "commit", "-q", "-m", "Ana")
	run(t, ana, "push", "-q", "origin", "main")

	require.NoError(t, os.WriteFile(filepath.Join(bo, "plan.md"), []byte("Bo's plan"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(bo, "notes.md"), []byte("more notes"), 0o644))
	boSync := start(t, "Bo", bo, nil, nil)
	eventually(t, func() bool { return boSync.State().State == Conflicts }, "both versions kept")
	st := boSync.State()
	assert.Equal(t, [][2]string{{"plan.md", "plan (conflict, Bo).md"}}, st.Pairs)
	assert.Equal(t, "Ana's plan", read(bo, "plan.md"), "the shared version under its name")
	assert.Equal(t, "Bo's plan", read(bo, "plan (conflict, Bo).md"), "Bo's beside it")
	assert.Equal(t, "more notes", read(bo, "notes.md"), "changed beats deleted")
	eventually(t, func() bool {
		return strings.Contains(run(t, bare, "ls-tree", "--name-only", "main"), "plan (conflict, Bo).md")
	}, "and shared")

	require.NoError(t, os.Remove(filepath.Join(bo, "plan (conflict, Bo).md")))
	eventually(t, func() bool { return boSync.State().State == Saved }, "settled once the copy is gone")
}

// The service out of reach: saves wait on this computer; signed out, it
// says so (EN-64).
func TestOffline(t *testing.T) {
	bare, ana, _ := shared(t)
	var signedOut atomic.Bool
	s := start(t, "Ana", ana, nil, &signedOut)
	require.NoError(t, os.Rename(bare, bare+".gone"))
	require.NoError(t, os.WriteFile(filepath.Join(ana, "plan.md"), []byte("offline"), 0o644))
	eventually(t, func() bool {
		return strings.TrimSpace(run(t, ana, "log", "-1", "--format=%s")) == "Ana: edited plan.md" && s.State().State == Offline
	}, "saved on this computer, offline")

	signedOut.Store(true)
	require.NoError(t, os.Rename(bare+".gone", bare))
	require.NoError(t, os.WriteFile(filepath.Join(ana, "more.md"), []byte("x"), 0o644))
	eventually(t, func() bool { return s.State().State == SignedOut }, "signed out")

	signedOut.Store(false)
	require.NoError(t, os.WriteFile(filepath.Join(ana, "more.md"), []byte("y"), 0o644))
	eventually(t, func() bool { return s.State().State == Saved }, "back")
	assert.Contains(t, run(t, bare, "log", "--format=%s", "main"), "Ana: edited plan.md")
}

func TestMessages(t *testing.T) {
	assert.Equal(t, "Ana: edited plan.md", message("Ana", []string{"plan.md"}))
	assert.Equal(t, "Ana: 3 files", message("Ana", []string{"a", "b", "c"}))
	assert.Equal(t, "docs/plan (conflict, Ana).md", conflictName("docs/plan.md", "Ana"))
	assert.Equal(t, "Makefile (conflict, Ana)", conflictName("Makefile", "Ana"))
}

// failing is git that fails at the commands named.
func failing(who string, at ...string) Git {
	real := gitAs(who, nil)
	return func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		for _, a := range at {
			if len(args) > 0 && args[0] == a {
				return nil, &gitErr{a + " failed"}
			}
		}
		return real(ctx, dir, args...)
	}
}

// Each step that can fail leaves it offline, its saves on this computer.
func TestFailures(t *testing.T) {
	for _, step := range []string{"add", "commit", "fetch", "push"} {
		t.Run(step, func(t *testing.T) {
			_, ana, _ := shared(t)
			s := &Syncer{Dir: ana, Git: failing("Ana", step), Who: "Ana", Quiet: 10 * time.Millisecond, Poll: 10 * time.Millisecond, Fetch: time.Hour}
			var seen []string
			s.OnState = func(st State) { seen = append(seen, st.State) }
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { s.Run(ctx); close(done) }()
			t.Cleanup(func() { cancel(); <-done })
			require.NoError(t, os.WriteFile(filepath.Join(ana, "x.md"), []byte("x"), 0o644))
			eventually(t, func() bool { return s.State().State == Offline }, "offline")
			assert.Contains(t, s.State().Err, step+" failed")
		})
	}
	t.Run("a merge that stops without conflicts", func(t *testing.T) {
		_, ana, bo := shared(t)
		require.NoError(t, os.WriteFile(filepath.Join(bo, "b.md"), []byte("b"), 0o644))
		run(t, bo, "add", "-A")
		run(t, bo, "commit", "-q", "-m", "b")
		run(t, bo, "push", "-q", "origin", "main")
		s := &Syncer{Dir: ana, Git: failing("Ana", "merge"), Who: "Ana"}
		assert.False(t, s.take(context.Background()))
		assert.Equal(t, Offline, s.State().State)
		assert.Contains(t, s.State().Err, "couldn't be taken")
	})
	t.Run("nothing shared yet", func(t *testing.T) {
		dir := t.TempDir()
		run(t, dir, "init", "-q", "-b", "main")
		bare := t.TempDir()
		run(t, bare, "init", "-q", "--bare")
		run(t, dir, "remote", "add", "origin", bare)
		s := &Syncer{Dir: dir, Git: gitAs("Ana", nil), Who: "Ana"}
		assert.True(t, s.take(context.Background()))
		s.Remote() // before Run: nothing to tell
	})
}

// Conflicts' other cases: a file this person deleted that the other
// changed stays changed; a step that fails while keeping both versions
// undoes the merge and leaves it offline.
func TestConflictCases(t *testing.T) {
	t.Run("this person deleted, they changed", func(t *testing.T) {
		_, ana, bo := shared(t)
		require.NoError(t, os.WriteFile(filepath.Join(ana, "plan.md"), []byte("Ana's plan"), 0o644))
		run(t, ana, "add", "-A")
		run(t, ana, "commit", "-q", "-m", "Ana")
		run(t, ana, "push", "-q", "origin", "main")
		require.NoError(t, os.Remove(filepath.Join(bo, "plan.md")))
		s := &Syncer{Dir: bo, Git: gitAs("Bo", nil), Who: "Bo", Now: time.Now}
		assert.True(t, s.take(context.Background()))
		assert.Equal(t, "Ana's plan", read(bo, "plan.md"), "changed beats deleted")
		assert.Empty(t, s.State().Pairs)
	})
	for _, step := range []string{"show", "checkout", "commit"} {
		t.Run("keeping both fails at "+step, func(t *testing.T) {
			_, ana, bo := shared(t)
			require.NoError(t, os.WriteFile(filepath.Join(ana, "plan.md"), []byte("Ana's"), 0o644))
			run(t, ana, "add", "-A")
			run(t, ana, "commit", "-q", "-m", "Ana")
			run(t, ana, "push", "-q", "origin", "main")
			require.NoError(t, os.WriteFile(filepath.Join(bo, "plan.md"), []byte("Bo's"), 0o644))
			run(t, bo, "add", "-A")
			run(t, bo, "commit", "-q", "-m", "Bo")
			real := gitAs("Bo", nil)
			merging := false
			s := &Syncer{Dir: bo, Who: "Bo", Git: func(ctx context.Context, dir string, args ...string) ([]byte, error) {
				if args[0] == "merge" && len(args) > 1 && args[1] == "-q" {
					merging = true
				}
				if merging && args[0] == step && !(step == "commit" && len(args) > 2 && args[2] == "-m" && !strings.Contains(args[len(args)-1], "both versions")) {
					return nil, &gitErr{step + " failed"}
				}
				return real(ctx, dir, args...)
			}}
			assert.False(t, s.take(context.Background()))
			assert.Equal(t, Offline, s.State().State)
			assert.Equal(t, "Bo's", read(bo, "plan.md"), "the merge undone: Bo's as it was")
		})
	}
}

// A push the service reached and turned down is Refused, with Git's
// words, not Offline: the window says the service refused it.
func TestFailed(t *testing.T) {
	for msg, want := range map[string]string{
		"exit status 1: error: remote unpack failed: reference delta not found":                  Refused,
		"exit status 1: ! [remote rejected] HEAD -> main (pre-receive hook declined)":            Refused,
		"exit status 128: fatal: unable to access 'https://b.acme.com/': Could not resolve host": Offline,
	} {
		s := &Syncer{SignedIn: func() bool { return true }}
		s.failed(errors.New(msg))
		assert.Equal(t, want, s.State().State, msg)
		assert.Equal(t, msg, s.State().Err)
	}
	s := &Syncer{SignedIn: func() bool { return false }}
	s.failed(errors.New("remote unpack failed"))
	assert.Equal(t, SignedOut, s.State().State, "signed out first")
}

// Save now sends what's changed at once, not after the quiet spell, and
// what waited on this computer while the service was away; never mid-turn.
func TestSaveNow(t *testing.T) {
	ctx := context.Background()
	bare, ana, _ := shared(t)
	_, err := (&Syncer{Dir: ana}).SaveNow(ctx)
	require.ErrorIs(t, err, ErrNotRunning)

	var busy atomic.Bool
	s := &Syncer{Dir: ana, Git: gitAs("Ana", nil), Who: "Ana", Quiet: time.Hour, Poll: 20 * time.Millisecond, Fetch: time.Hour,
		Busy: busy.Load}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { s.Run(runCtx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	eventually(t, func() bool { return s.State().State == Saved }, "started")

	require.NoError(t, os.WriteFile(filepath.Join(ana, "plan.md"), []byte("now"), 0o644))
	busy.Store(true)
	_, err = s.SaveNow(ctx)
	require.ErrorIs(t, err, ErrBusy)
	busy.Store(false)
	st, err := s.SaveNow(ctx)
	require.NoError(t, err)
	assert.Equal(t, Saved, st.State)
	assert.Equal(t, "Ana: edited plan.md", strings.TrimSpace(run(t, bare, "log", "-1", "--format=%s", "main")), "an hour's quiet not waited for")

	// Saved here while the service was away: sent now it's back.
	require.NoError(t, os.Rename(bare, bare+".gone"))
	require.NoError(t, os.WriteFile(filepath.Join(ana, "plan.md"), []byte("away"), 0o644))
	st, err = s.SaveNow(ctx)
	require.NoError(t, err)
	assert.Equal(t, Offline, st.State)
	assert.NotEmpty(t, st.Err)
	require.NoError(t, os.Rename(bare+".gone", bare))
	st, err = s.SaveNow(ctx)
	require.NoError(t, err)
	assert.Equal(t, Saved, st.State)
	assert.Equal(t, strings.TrimSpace(run(t, ana, "rev-parse", "HEAD")), strings.TrimSpace(run(t, bare, "rev-parse", "main")))

	st, err = s.SaveNow(ctx)
	require.NoError(t, err)
	assert.Equal(t, Saved, st.State, "nothing to send")

	gone, stop := context.WithCancel(ctx)
	stop()
	_, err = s.SaveNow(gone)
	assert.ErrorIs(t, err, context.Canceled)
}
