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

package session

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"

	adksession "google.golang.org/adk/v2/session"
)

const eventsSuffix = ".events.jsonl"

// PersistentService is an ADK session service that keeps sessions in memory
// and appends every final event to <dir>/<session>.events.jsonl, so a
// conversation (including tool calls and compaction summaries) can be resumed
// in a later process by using the same session ID.
//
// Only the few sessions used last stay in memory (Resident), and those a
// turn runs in (Pin); the others are dropped and loaded again from their
// file when next asked for, so a service that runs many sessions (a
// worker's every run is one) doesn't keep every conversation it ever ran.
type PersistentService struct {
	inner adksession.Service
	dir   string
	mu    sync.Mutex // serialises loads and file appends

	resMu  sync.Mutex
	recent []sessionKey   // the sessions in memory, least recently used first
	pinned map[string]int // turns running in each session, by ID
}

// Resident is how many sessions stay in memory with no turn running in
// them.
var Resident = 4

// sessionKey names a session in the in-memory service.
type sessionKey struct{ app, user, id string }

// NewPersistentService stores event logs in dir (created owner-only).
func NewPersistentService(dir string) (*PersistentService, error) {
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return nil, err
	}
	return &PersistentService{inner: adksession.InMemoryService(), dir: dir, pinned: map[string]int{}}, nil
}

func (p *PersistentService) eventsPath(id string) (string, error) {
	if err := ValidateID(id); err != nil {
		return "", err
	}
	return filepath.Join(p.dir, id+eventsSuffix), nil
}

// HasEvents reports whether a stored event log exists for id.
func (p *PersistentService) HasEvents(id string) bool {
	path, err := p.eventsPath(id)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// Create creates a session, replaying stored events if the ID was used before.
func (p *PersistentService) Create(ctx context.Context, req *adksession.CreateRequest) (*adksession.CreateResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	resp, err := p.inner.Create(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := p.replayLocked(ctx, resp.Session); err != nil {
		// Not left half-loaded: a later Get would find it without its history.
		_ = p.inner.Delete(ctx, &adksession.DeleteRequest{AppName: req.AppName, UserID: req.UserID, SessionID: resp.Session.ID()})
		return nil, err
	}
	p.touch(ctx, sessionKey{req.AppName, req.UserID, resp.Session.ID()})
	return resp, nil
}

// Get returns a session, loading it from disk on first access after a restart.
func (p *PersistentService) Get(ctx context.Context, req *adksession.GetRequest) (*adksession.GetResponse, error) {
	resp, err := p.inner.Get(ctx, req)
	if err == nil {
		p.touch(ctx, sessionKey{req.AppName, req.UserID, req.SessionID})
	}
	if err == nil || !p.HasEvents(req.SessionID) {
		return resp, err
	}
	p.mu.Lock()
	if _, again := p.inner.Get(ctx, req); again != nil {
		created, cerr := p.inner.Create(ctx, &adksession.CreateRequest{AppName: req.AppName, UserID: req.UserID, SessionID: req.SessionID})
		if cerr != nil {
			p.mu.Unlock()
			return nil, cerr
		}
		if rerr := p.replayLocked(ctx, created.Session); rerr != nil {
			_ = p.inner.Delete(ctx, &adksession.DeleteRequest{AppName: req.AppName, UserID: req.UserID, SessionID: req.SessionID})
			p.mu.Unlock()
			return nil, rerr
		}
	}
	p.mu.Unlock()
	p.touch(ctx, sessionKey{req.AppName, req.UserID, req.SessionID})
	return p.inner.Get(ctx, req)
}

// List delegates to the in-memory service.
func (p *PersistentService) List(ctx context.Context, req *adksession.ListRequest) (*adksession.ListResponse, error) {
	return p.inner.List(ctx, req)
}

// Delete removes the session and its stored events.
func (p *PersistentService) Delete(ctx context.Context, req *adksession.DeleteRequest) error {
	if path, err := p.eventsPath(req.SessionID); err == nil {
		_ = os.Remove(path)
	}
	p.forget(sessionKey{req.AppName, req.UserID, req.SessionID})
	return p.inner.Delete(ctx, req)
}

// Pin keeps session id in memory until the returned func is called: a
// turn's events go to the session the runner loaded, which must still be
// there. Pins nest (two turns in one session).
func (p *PersistentService) Pin(id string) (unpin func()) {
	p.resMu.Lock()
	p.pinned[id]++
	p.resMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			p.resMu.Lock()
			if p.pinned[id]--; p.pinned[id] <= 0 {
				delete(p.pinned, id)
			}
			p.resMu.Unlock()
			p.evict(context.Background())
		})
	}
}

// touch marks a session in memory as just used, then drops the least
// recently used beyond Resident.
func (p *PersistentService) touch(ctx context.Context, k sessionKey) {
	p.resMu.Lock()
	p.recent = slices.DeleteFunc(p.recent, func(r sessionKey) bool { return r == k })
	p.recent = append(p.recent, k)
	p.resMu.Unlock()
	p.evict(ctx)
}

// forget stops tracking a session that has left memory.
func (p *PersistentService) forget(k sessionKey) {
	p.resMu.Lock()
	p.recent = slices.DeleteFunc(p.recent, func(r sessionKey) bool { return r == k })
	p.resMu.Unlock()
}

// evict drops the least recently used sessions no turn runs in, until at
// most Resident of those are left. Their files have every final event, so
// Get loads them again as after a restart.
func (p *PersistentService) evict(ctx context.Context) {
	p.resMu.Lock()
	var drop []sessionKey
	unpinned := 0
	for _, k := range p.recent {
		if p.pinned[k.id] == 0 {
			unpinned++
		}
	}
	keep := p.recent[:0]
	for _, k := range p.recent {
		if unpinned > Resident && p.pinned[k.id] == 0 {
			drop = append(drop, k)
			unpinned--
			continue
		}
		keep = append(keep, k)
	}
	p.recent = keep
	p.resMu.Unlock()
	for _, k := range drop {
		_ = p.inner.Delete(ctx, &adksession.DeleteRequest{AppName: k.app, UserID: k.user, SessionID: k.id})
	}
}

// residentIDs are the sessions in memory now, least recently used first
// (tests).
func (p *PersistentService) residentIDs() []string {
	p.resMu.Lock()
	defer p.resMu.Unlock()
	out := make([]string, len(p.recent))
	for i, k := range p.recent {
		out[i] = k.id
	}
	return out
}

// AppendEvent records the event in memory, then durably appends final events.
func (p *PersistentService) AppendEvent(ctx context.Context, s adksession.Session, ev *adksession.Event) error {
	if err := p.inner.AppendEvent(ctx, s, ev); err != nil {
		return err
	}
	if ev == nil || ev.Partial {
		return nil
	}
	path, err := p.eventsPath(s.ID())
	if err != nil {
		return err
	}
	line, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("encode event: %w", err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, filePerm)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Truncate keeps the first keep events of a session and drops the rest,
// on disk (atomically) and in memory (rewinding the conversation).
func (p *PersistentService) Truncate(ctx context.Context, appName, userID, id string, keep int) error {
	path, err := p.eventsPath(id)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var out bytes.Buffer
	n := 0
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 64<<20)
	for sc.Scan() && n < keep {
		line := bytes.TrimSpace(sc.Bytes())
		var ev adksession.Event
		if len(line) == 0 || json.Unmarshal(line, &ev) != nil {
			continue // replay skips these too
		}
		out.Write(line)
		out.WriteByte('\n')
		n++
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if n < keep {
		return fmt.Errorf("session %s has %d events, fewer than %d", id, n, keep)
	}
	if err := replaceFile(path, out.Bytes()); err != nil {
		return err
	}
	// Rebuild the in-memory session from the file.
	_ = p.inner.Delete(ctx, &adksession.DeleteRequest{AppName: appName, UserID: userID, SessionID: id})
	created, err := p.inner.Create(ctx, &adksession.CreateRequest{AppName: appName, UserID: userID, SessionID: id})
	if err != nil {
		return err
	}
	defer p.touch(ctx, sessionKey{appName, userID, id})
	return p.replayLocked(ctx, created.Session)
}

func (p *PersistentService) replayLocked(ctx context.Context, s adksession.Session) error {
	path, err := p.eventsPath(s.ID())
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 64<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var ev adksession.Event
		if json.Unmarshal(line, &ev) != nil {
			continue // a torn final line from a crash
		}
		if err := p.inner.AppendEvent(ctx, s, &ev); err != nil {
			return fmt.Errorf("replay session %s: %w", s.ID(), err)
		}
	}
	return sc.Err()
}

// ReadEvents reads session id's stored event log from dir, for exports
// (nil when it has none).
func ReadEvents(dir, id string) ([]*adksession.Event, error) {
	if err := ValidateID(id); err != nil {
		return nil, err
	}
	f, err := os.Open(filepath.Join(dir, id+eventsSuffix))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []*adksession.Event
	dec := json.NewDecoder(f)
	for {
		ev := new(adksession.Event)
		if err := dec.Decode(ev); err != nil {
			if errors.Is(err, io.EOF) {
				return out, nil
			}
			return out, fmt.Errorf("event %d: %w", len(out)+1, err)
		}
		out = append(out, ev)
	}
}
