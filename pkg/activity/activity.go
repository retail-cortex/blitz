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
// (spec_054_active_installs): the first time a front end is used on a day,
// it's marked in ~/.blitz/activity.json and a ping goes to Retail Cortex
// with the install's random ID, the day, the front end, the version, the
// platform and whether it went through a proxy (not which), and nothing
// else. A ping that fails is tried again for up to Keep days. [usage]
// count = false turns all of it off.
package activity

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// AgentEnv marks the commands Blitz runs for the model (its shell, its
// language servers): a blitz command run there is the agent's, not the
// person's use of the CLI, and isn't counted (AI-03a).
const AgentEnv = "BLITZ_AGENT"

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
	// CAFile is a PEM file of certificate authorities to trust besides the
	// system's ([network] ca_file: a company's TLS-inspecting proxy), read
	// at each ping; nil or "" for none.
	CAFile func() string
	// Send, when set, sends a ping another way than Go's HTTP client: the
	// desktop app's own network (AI-14), which uses the system's proxy
	// settings, its PAC file and sign-in, as the browser does. It sets
	// the ping's proxy field and says the answer's HTTP status.
	Send func(ctx context.Context, url string, ping map[string]string) (int, error)

	mu       sync.Mutex // activity.json's reads and writes in this process, one at a time; never held over the network
	flushing sync.Mutex // one flush at a time, so a ping isn't sent twice by this process
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
// are kept for a later flush, within Keep days. The file isn't locked
// while the pings go (AI-11: a Mark never waits on the network), and what
// was sent is merged into the file as it is then, so a day marked
// meanwhile, here or by another process, is kept.
func (r *Recorder) Flush(ctx context.Context) error {
	if !r.on() {
		return nil
	}
	r.flushing.Lock()
	defer r.flushing.Unlock()
	r.mu.Lock()
	today := day(r.now())
	pending := recent(r.load().Pending, today)
	r.mu.Unlock()
	if len(pending) == 0 {
		return nil
	}
	id, err := InstallID(r.Dir)
	if err != nil {
		return err
	}
	var errs []error
	var sent []entry
	for _, e := range pending {
		switch err := r.send(ctx, id, e); {
		case err == nil, errors.Is(err, errRefused):
			sent = append(sent, e)
		default:
			errs = append(errs, err)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.load() // as it is now: marks made while the pings went are kept
	s.Pending = recent(slices.DeleteFunc(s.Pending, func(e entry) bool { return slices.Contains(sent, e) }), today)
	for _, e := range sent {
		if !slices.Contains(s.Sent, e) {
			s.Sent = append(s.Sent, e)
		}
	}
	s.Sent = recent(s.Sent, today)
	if err := r.save(s); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// errRefused is a ping Retail Cortex won't count: it's never sent again.
var errRefused = errors.New("the ping isn't counted")

// send sends one ping, with exactly the fields AI-10 names (the sender
// adds proxy).
func (r *Recorder) send(ctx context.Context, id string, e entry) error {
	ping := map[string]string{
		"install": id, "day": e.Day, "front": e.Front, "version": r.Version, "os": goruntime.GOOS, "arch": goruntime.GOARCH,
	}
	ctx, cancel := context.WithTimeout(ctx, Wait)
	defer cancel()
	sendFn := r.post
	if r.Send != nil {
		sendFn = r.Send
	}
	status, err := sendFn(ctx, r.url(), ping)
	if err != nil {
		return err
	}
	switch {
	case status == http.StatusNoContent || status == http.StatusOK:
		return nil
	case status == http.StatusBadRequest:
		return fmt.Errorf("%w: HTTP %d", errRefused, status)
	}
	return fmt.Errorf("HTTP %d", status)
}

// post sends ping with Go's HTTP client: the proxy the environment names
// (HTTPS_PROXY), the system's certificates and CAFile's.
func (r *Recorder) post(ctx context.Context, url string, ping map[string]string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return 0, err
	}
	hc := r.HTTP
	if hc == nil {
		if hc, err = r.client(); err != nil {
			return 0, err
		}
	}
	proxy := "N"
	if t, ok := hc.Transport.(*http.Transport); ok && t.Proxy != nil {
		if u, err := t.Proxy(req); err == nil && u != nil {
			proxy = "Y"
		}
	}
	ping["proxy"] = proxy
	body, err := json.Marshal(ping)
	if err != nil {
		return 0, err
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := hc.Do(req)
	if err != nil {
		return 0, err
	}
	res.Body.Close()
	return res.StatusCode, nil
}

// client is Go's default transport, trusting CAFile's certificate
// authorities too.
func (r *Recorder) client() (*http.Client, error) {
	t := http.DefaultTransport.(*http.Transport).Clone()
	if r.CAFile != nil {
		if file := r.CAFile(); file != "" {
			pem, err := os.ReadFile(file)
			if err != nil {
				return nil, fmt.Errorf("[network] ca_file: %w", err)
			}
			pool, err := x509.SystemCertPool()
			if err != nil || pool == nil {
				pool = x509.NewCertPool()
			}
			if !pool.AppendCertsFromPEM(pem) {
				return nil, fmt.Errorf("[network] ca_file %s: no PEM certificates in it", file)
			}
			tc := &tls.Config{MinVersion: tls.VersionTLS12}
			if t.TLSClientConfig != nil {
				tc = t.TLSClientConfig.Clone()
			}
			tc.RootCAs = pool
			t.TLSClientConfig = tc
		}
	}
	return &http.Client{Transport: t}, nil
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

// UseAndSend marks front's use today and sends, in the background, every
// ping still to send, due today or not: the desktop app's host, which
// sends through the system's proxy (AI-14), sends the pings another
// process marked and couldn't (the service, which has no proxy). The
// channel closes when that's done.
func (r *Recorder) UseAndSend(front string) <-chan struct{} {
	done := make(chan struct{})
	if _, err := r.Mark(front); err != nil || !r.on() {
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
