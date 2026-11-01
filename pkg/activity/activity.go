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

// Package activity counts the install as active, once a day it's used
// (spec_active_installs_054): the first time a front end is used on a day,
// it's marked in ~/.blitz/activity.json and a ping goes to Retail Cortex
// with the install's random ID, the day, the front end, the version and
// the platform, and nothing else. A ping that fails is tried again for
// up to Keep days. [usage] count = false turns all of it off.
package activity

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

// URL is where the pings go (AI-10); BLITZ_ACTIVITY_URL replaces it
// (AI-13).
const URL = "https://license.retailcortex.com/v1/blitz/active"

// The front ends (AI-01).
const (
	Desktop = "desktop"
	CLI     = "cli"
	VSCode  = "vscode"
)

// Fronts are the front ends a ping may name.
var Fronts = []string{Desktop, CLI, VSCode}

// Wait is how long a ping may take (AI-11); Keep how many days before
// today a ping that failed is still tried (AI-12).
var (
	Wait = 5 * time.Second
	Keep = 3
)

// Recorder marks the day's use and sends the pings. Its zero value isn't
// usable: give Dir and Version at least.
type Recorder struct {
	Dir     string // ~/.blitz: activity.json and install-id
	Version string // Blitz's
	// On says whether use is counted: [usage] count (AI-30), read at each
	// use so a change applies at once. nil is on.
	On func() bool
	// URL is where the pings go: URL, or BLITZ_ACTIVITY_URL, unless set.
	URL  string
	HTTP *http.Client
	Now  func() time.Time

	mu sync.Mutex // one process's marks and flushes, one at a time
}

func (r *Recorder) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Recorder) url() string {
	if r.URL != "" {
		return r.URL
	}
	if u := os.Getenv("BLITZ_ACTIVITY_URL"); u != "" {
		return u
	}
	return URL
}

func (r *Recorder) on() bool { return r.On == nil || r.On() }

// entry is a day a front end was used.
type entry struct {
	Day   string `json:"day"`
	Front string `json:"front"`
}

// state is activity.json: the pings still to send, and those sent in the
// last Keep days, so each is sent once (AI-04).
type state struct {
	Pending []entry `json:"pending"`
	Sent    []entry `json:"sent"`
}

func (r *Recorder) file() string { return filepath.Join(r.Dir, "activity.json") }

func (r *Recorder) load() state {
	var s state
	if data, err := os.ReadFile(r.file()); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	return s
}

// save writes s whole, through a private file beside it (one for each
// process), so a reader never sees half of it.
func (r *Recorder) save(s state) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(r.Dir, 0o700); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", r.file(), os.Getpid())
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, r.file())
}

// recent keeps es from the last Keep days before today, and today.
func recent(es []entry, today time.Time) []entry {
	oldest := today.AddDate(0, 0, -Keep).Format(time.DateOnly)
	return slices.DeleteFunc(slices.Clone(es), func(e entry) bool { return e.Day < oldest })
}

// Mark notes that front was used today, and says whether a ping is due
// for it: false when it was already marked today, or use isn't counted
// (AI-01 to AI-04, AI-30).
func (r *Recorder) Mark(front string) (bool, error) {
	if !r.on() || !slices.Contains(Fronts, front) {
		return false, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	today := day(r.now())
	e := entry{Day: today.Format(time.DateOnly), Front: front}
	s := r.load()
	if slices.Contains(s.Pending, e) || slices.Contains(s.Sent, e) {
		return false, nil
	}
	s.Pending = append(recent(s.Pending, today), e)
	s.Sent = recent(s.Sent, today)
	return true, r.save(s)
}

// Flush sends the pings still to send (AI-10, AI-12): those Retail Cortex
// takes, or refuses as not counted, aren't sent again; those that fail
// are kept for a later flush, within Keep days.
func (r *Recorder) Flush(ctx context.Context) error {
	if !r.on() {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	today := day(r.now())
	s := r.load()
	pending := recent(s.Pending, today)
	if len(pending) == 0 {
		return nil
	}
	id, err := InstallID(r.Dir)
	if err != nil {
		return err
	}
	var errs []error
	var kept []entry
	for _, e := range pending {
		switch err := r.send(ctx, id, e); {
		case err == nil, errors.Is(err, errRefused):
			s.Sent = append(s.Sent, e)
		default:
			kept = append(kept, e)
			errs = append(errs, err)
		}
	}
	s.Pending, s.Sent = kept, recent(s.Sent, today)
	if err := r.save(s); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// errRefused is a ping Retail Cortex won't count: it's never sent again.
var errRefused = errors.New("the ping isn't counted")

// send posts one ping, with exactly the fields AI-10 names.
func (r *Recorder) send(ctx context.Context, id string, e entry) error {
	body, err := json.Marshal(map[string]string{
		"install": id, "day": e.Day, "front": e.Front, "version": r.Version, "os": goruntime.GOOS, "arch": goruntime.GOARCH,
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, Wait)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	hc := r.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	res, err := hc.Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	switch {
	case res.StatusCode == http.StatusNoContent || res.StatusCode == http.StatusOK:
		return nil
	case res.StatusCode == http.StatusBadRequest:
		return fmt.Errorf("%w: HTTP %d", errRefused, res.StatusCode)
	}
	return fmt.Errorf("HTTP %d", res.StatusCode)
}

// Use marks front's use today and, when a ping is due, sends it in the
// background: the caller never waits (AI-11). The channel closes when
// that's done, for a program about to exit to wait on briefly.
func (r *Recorder) Use(front string) <-chan struct{} {
	done := make(chan struct{})
	due, err := r.Mark(front)
	if err != nil || !due {
		close(done)
		return done
	}
	go func() {
		defer close(done)
		_ = r.Flush(context.Background())
	}()
	return done
}

// day is t's day in UTC, at midnight.
func day(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// InstallID is the install's ID, 16 random bytes in hex, made the first
// time in dir/install-id and read after (AI-20).
func InstallID(dir string) (string, error) {
	file := filepath.Join(dir, "install-id")
	if data, err := os.ReadFile(file); err == nil {
		if id := strings.TrimSpace(string(data)); validID(id) {
			return id, nil
		}
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(file, []byte(id+"\n"), 0o600); err != nil {
		return "", err
	}
	return id, nil
}

func validID(id string) bool {
	_, err := hex.DecodeString(id)
	return len(id) == 32 && err == nil
}
