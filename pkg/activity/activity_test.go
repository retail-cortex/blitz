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

package activity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// server is Retail Cortex's /active: it answers with status and keeps
// what it was sent.
type server struct {
	srv    *httptest.Server
	status atomic.Int32
	mu     sync.Mutex
	got    []map[string]string
	header []http.Header
}

func newServer(t *testing.T) *server {
	t.Helper()
	s := &server{}
	s.status.Store(http.StatusNoContent)
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]string
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&m))
		s.mu.Lock()
		s.got = append(s.got, m)
		s.header = append(s.header, r.Header.Clone())
		s.mu.Unlock()
		w.WriteHeader(int(s.status.Load()))
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *server) pings() []map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]string(nil), s.got...)
}

var start = time.Date(2026, 11, 4, 15, 0, 0, 0, time.UTC)

func newRecorder(t *testing.T, url string, now *time.Time) *Recorder {
	t.Helper()
	return &Recorder{Dir: filepath.Join(t.TempDir(), ".blitz"), Version: "v0.9.0", URL: url, Now: func() time.Time { return *now }}
}

// A front end's use is a ping the first time each day, with exactly
// AI-10's fields; again the same day, nothing (AI-01, AI-04, AI-10).
func TestOncePerDayPerFront(t *testing.T) {
	s := newServer(t)
	now := start
	r := newRecorder(t, s.srv.URL, &now)
	ctx := context.Background()

	due, err := r.Mark(CLI)
	require.NoError(t, err)
	assert.True(t, due)
	require.NoError(t, r.Flush(ctx))
	for range 3 {
		due, err = r.Mark(CLI)
		require.NoError(t, err)
		assert.False(t, due, "again the same day")
		require.NoError(t, r.Flush(ctx))
	}
	due, err = r.Mark(Desktop)
	require.NoError(t, err)
	assert.True(t, due, "another front end the same day")
	require.NoError(t, r.Flush(ctx))
	now = start.AddDate(0, 0, 1)
	due, err = r.Mark(CLI)
	require.NoError(t, err)
	assert.True(t, due, "the next day")
	require.NoError(t, r.Flush(ctx))

	id, err := InstallID(r.Dir)
	require.NoError(t, err)
	got := s.pings()
	require.Len(t, got, 3)
	assert.Equal(t, map[string]string{"install": id, "day": "2026-11-04", "front": "cli", "version": "v0.9.0", "os": goruntime.GOOS, "arch": goruntime.GOARCH}, got[0], "these fields, nothing else")
	assert.Equal(t, "desktop", got[1]["front"])
	assert.Equal(t, "2026-11-05", got[2]["day"])
	s.mu.Lock()
	assert.Empty(t, s.header[0].Get("Cookie"))
	s.mu.Unlock()

	info, err := os.Stat(filepath.Join(r.Dir, "activity.json"))
	require.NoError(t, err)
	if goruntime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
	due, err = r.Mark("tray")
	require.NoError(t, err)
	assert.False(t, due, "not a front end")
}

// Switched off, nothing is marked or sent, and no ID is made (AI-30).
func TestOff(t *testing.T) {
	s := newServer(t)
	now := start
	r := newRecorder(t, s.srv.URL, &now)
	r.On = func() bool { return false }
	due, err := r.Mark(Desktop)
	require.NoError(t, err)
	assert.False(t, due)
	require.NoError(t, r.Flush(context.Background()))
	<-r.Use(CLI)
	assert.Empty(t, s.pings())
	_, err = os.Stat(r.Dir)
	assert.ErrorIs(t, err, os.ErrNotExist, "no activity.json, no install-id")
}

// A ping that fails is kept and sent at a later flush, for up to Keep
// days; one Retail Cortex refuses isn't sent again (AI-12).
func TestRetries(t *testing.T) {
	s := newServer(t)
	now := start
	r := newRecorder(t, s.srv.URL, &now)
	ctx := context.Background()

	s.status.Store(http.StatusServiceUnavailable)
	_, err := r.Mark(CLI)
	require.NoError(t, err)
	assert.Error(t, r.Flush(ctx), "the server is down")
	now = start.Add(time.Hour)
	assert.Error(t, r.Flush(ctx), "still down: kept")
	s.status.Store(http.StatusNoContent)
	now = start.AddDate(0, 0, 2)
	require.NoError(t, r.Flush(ctx), "two days late: sent for its day")
	got := s.pings()
	require.Len(t, got, 3)
	assert.Equal(t, "2026-11-04", got[2]["day"])
	require.NoError(t, r.Flush(ctx))
	assert.Len(t, s.pings(), 3, "sent once")

	s.status.Store(http.StatusServiceUnavailable)
	now = start.AddDate(0, 0, 3)
	_, err = r.Mark(Desktop)
	require.NoError(t, err)
	assert.Error(t, r.Flush(ctx))
	s.status.Store(http.StatusNoContent)
	now = now.AddDate(0, 0, Keep+1)
	require.NoError(t, r.Flush(ctx))
	assert.Len(t, s.pings(), 4, "more than Keep days late: dropped, not sent")

	s.status.Store(http.StatusBadRequest)
	_, err = r.Mark(VSCode)
	require.NoError(t, err)
	require.NoError(t, r.Flush(ctx), "refused: dropped")
	s.status.Store(http.StatusNoContent)
	require.NoError(t, r.Flush(ctx))
	assert.Len(t, s.pings(), 5, "a refused ping isn't sent again")
}

// Use never makes the caller wait: it returns at once, and the ping goes
// in the background (AI-11).
func TestUseDoesntWait(t *testing.T) {
	saved := Wait
	Wait = 300 * time.Millisecond
	t.Cleanup(func() { Wait = saved })
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(slow.Close)
	t.Cleanup(func() { close(release) })
	now := start
	r := newRecorder(t, slow.URL, &now)
	began := time.Now()
	done := r.Use(CLI)
	assert.Less(t, time.Since(began), 100*time.Millisecond, "returned at once")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the ping didn't give up after Wait")
	}
	due, err := r.Mark(CLI)
	require.NoError(t, err)
	assert.False(t, due, "kept to try again, not marked twice")
}

// A flush stuck on the network holds no one up: a mark made meanwhile,
// in this process or another sharing the file, answers at once and is
// still there, to send, when the flush saves what it sent (AI-11, AI-04).
func TestMarkDuringFlush(t *testing.T) {
	// Every wait is bounded, so a regression fails in seconds rather than
	// hanging to Bazel's timeout; release lets the flush go on any exit.
	const bound = 2 * time.Second
	arrived := make(chan struct{}, 4)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		arrived <- struct{}{}
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(slow.Close)
	t.Cleanup(unblock) // runs before slow.Close, which waits for the handler
	now := start
	r := newRecorder(t, slow.URL, &now)
	other := &Recorder{Dir: r.Dir, Version: r.Version, URL: slow.URL, Now: r.Now} // another process
	due, err := r.Mark(Desktop)
	require.NoError(t, err)
	require.True(t, due)
	flushed := make(chan error, 1)
	go func() { flushed <- r.Flush(context.Background()) }()
	select {
	case <-arrived: // the desktop's ping is on the wire
	case <-time.After(bound):
		t.Fatal("the flush sent no ping")
	}

	cases := []struct {
		name string
		rec  *Recorder
		fr   string
	}{
		{"this process", r, CLI},
		{"another process", other, VSCode},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			type marked struct {
				due bool
				err error
			}
			got := make(chan marked, 1)
			go func() {
				due, err := tc.rec.Mark(tc.fr)
				got <- marked{due, err}
			}()
			select {
			case m := <-got:
				require.NoError(t, m.err)
				assert.True(t, m.due)
			case <-time.After(bound):
				t.Fatal("the mark waited for the flush")
			}
		})
	}
	unblock()
	select {
	case err := <-flushed:
		require.NoError(t, err)
	case <-time.After(bound):
		t.Fatal("the flush didn't finish")
	}
	s := r.load()
	day := start.Format(time.DateOnly)
	assert.Equal(t, []entry{{day, Desktop}}, s.Sent)
	assert.ElementsMatch(t, []entry{{day, CLI}, {day, VSCode}}, s.Pending, "the marks made meanwhile are kept")
}

// BLITZ_ACTIVITY_URL replaces the address (AI-13).
func TestActivityURL(t *testing.T) {
	s := newServer(t)
	t.Setenv("BLITZ_ACTIVITY_URL", s.srv.URL)
	now := start
	r := &Recorder{Dir: t.TempDir(), Version: "dev", Now: func() time.Time { return now }}
	<-r.Use(Desktop)
	assert.Len(t, s.pings(), 1)
	t.Setenv("BLITZ_ACTIVITY_URL", "")
	assert.Equal(t, URL, r.url())
}

// The ID is made once, private, and kept; a file that isn't one is
// replaced; with nowhere to keep it, no ping is sent (AI-20).
func TestInstallID(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".blitz")
	id, err := InstallID(dir)
	require.NoError(t, err)
	assert.Regexp(t, `^[0-9a-f]{32}$`, id)
	info, err := os.Stat(filepath.Join(dir, "install-id"))
	require.NoError(t, err)
	if goruntime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
	again, err := InstallID(dir)
	require.NoError(t, err)
	assert.Equal(t, id, again)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "install-id"), []byte("not an id"), 0o600))
	other, err := InstallID(dir)
	require.NoError(t, err)
	assert.NotEqual(t, id, other)

	blocked := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(blocked, "install-id"), 0o700))
	_, err = InstallID(blocked)
	assert.Error(t, err, "install-id is a folder")
	s := newServer(t)
	now := start
	r := &Recorder{Dir: blocked, Version: "dev", URL: s.srv.URL, Now: func() time.Time { return now }}
	_, err = r.Mark(CLI)
	require.NoError(t, err)
	assert.Error(t, r.Flush(context.Background()))
	assert.Empty(t, s.pings())

	_, err = InstallID(filepath.Join(dir, "install-id", "below a file"))
	assert.Error(t, err)
}

// Where activity.json can't be kept, nothing is marked; an address that
// isn't one fails the flush, and the ping is kept (AI-04, AI-12).
func TestFailures(t *testing.T) {
	now := start
	blocked := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocked, nil, 0o600))
	r := &Recorder{Dir: filepath.Join(blocked, ".blitz"), Version: "dev", URL: "http://127.0.0.1:1", Now: func() time.Time { return now }}
	_, err := r.Mark(CLI)
	assert.Error(t, err, "its folder is under a file")
	<-r.Use(CLI)

	bad := newRecorder(t, "http://bad\x7f.example", &now)
	_, err = bad.Mark(CLI)
	require.NoError(t, err)
	assert.Error(t, bad.Flush(context.Background()))
	due, err := bad.Mark(CLI)
	require.NoError(t, err)
	assert.False(t, due, "kept, not marked again")
}
