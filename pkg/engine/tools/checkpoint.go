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

package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
)

// fileState is a file's state before a change. Its content is kept in the
// store's blobs, by hash.
type fileState struct {
	Exists bool        `json:"exists"`
	Hash   string      `json:"hash,omitempty"` // sha256 of the content
	Size   int64       `json:"size,omitempty"`
	Mode   fs.FileMode `json:"mode,omitempty"`
	// TooLarge marks a file that existed but exceeded the snapshot limit, so
	// it cannot be restored.
	TooLarge bool `json:"too_large,omitempty"`
}

type fileChange struct {
	Abs         string    `json:"abs"`
	Display     string    `json:"display"`
	Before      fileState `json:"before"`
	AfterExists bool      `json:"after_exists"`
	AfterHash   string    `json:"after_hash"` // sha256 of what the tool left
}

// Turn is the set of file changes made while handling one user prompt.
type Turn struct {
	ID int `json:"id"`
	// Session is the session the prompt belongs to, and Prompt the index of
	// its message in the transcript (-1 when unknown), so a rewind can find
	// the changes made from a prompt on.
	Session string `json:"session,omitempty"`
	// Run is the worker run that made the turn: its changes are the run's
	// own (blitz workers undo), not the interactive sessions' /undo.
	Run     string        `json:"run,omitempty"`
	Prompt  int           `json:"prompt"`
	Label   string        `json:"label"`
	Started time.Time     `json:"started"`
	Changes []*fileChange `json:"changes"`
	index   map[string]*fileChange
}

// TurnSummary describes a checkpoint for display.
type TurnSummary struct {
	ID      int
	Session string
	Prompt  int
	Label   string
	Time    time.Time
	Files   []string
}

// UndoResult reports what an undo restored.
type UndoResult struct {
	Turn     TurnSummary
	Restored []string
}

// ErrNothingToUndo is returned when no checkpoint has changes to restore.
var ErrNothingToUndo = errors.New("nothing to undo")

// CheckpointOptions configure a checkpoint store.
type CheckpointOptions struct {
	// MaxBytes bounds the snapshots kept; the oldest turns go first, and the
	// latest is always kept.
	MaxBytes int64
	// Dir keeps the checkpoints on disk, so they outlive the process: an
	// index and content-addressed blobs, owner-only. "" keeps them in memory.
	Dir string
	// MaxAge drops persisted turns older than this (0: 30 days).
	MaxAge time.Duration
}

// Checkpoints snapshots files before tools modify them so changes can be
// undone turn by turn. Only changes made through the file tools are tracked;
// shell commands are not (their effects are detected as conflicts).
type Checkpoints struct {
	mu       sync.Mutex
	ws       *Workspace
	turns    []*Turn
	nextID   int
	bytes    int64
	maxBytes int64
	maxAge   time.Duration
	blobs    blobStore
	index    string // the index file; "" in memory
	warned   bool
	collect  bool // turns were dropped: remove unused blobs at the next save
}

// NewCheckpoints attaches an in-memory checkpoint store to ws.
func NewCheckpoints(ws *Workspace, maxBytes int64) *Checkpoints {
	c, _ := OpenCheckpoints(ws, CheckpointOptions{MaxBytes: maxBytes})
	return c
}

// OpenCheckpoints attaches a checkpoint store to ws, loading what an
// earlier process persisted in o.Dir. A damaged index is set aside and the
// store starts empty (the error says so).
func OpenCheckpoints(ws *Workspace, o CheckpointOptions) (*Checkpoints, error) {
	if o.MaxBytes <= 0 {
		o.MaxBytes = 64 << 20
	}
	if o.MaxAge <= 0 {
		o.MaxAge = 30 * 24 * time.Hour
	}
	c := &Checkpoints{ws: ws, maxBytes: o.MaxBytes, maxAge: o.MaxAge, blobs: &memBlobs{m: map[string][]byte{}}}
	ws.checkpoints = c
	if o.Dir == "" {
		return c, nil
	}
	if err := os.MkdirAll(filepath.Join(o.Dir, "blobs"), 0o700); err != nil {
		return c, fmt.Errorf("checkpoints: %w", err)
	}
	os.Chmod(o.Dir, 0o700)
	c.blobs = diskBlobs(filepath.Join(o.Dir, "blobs"))
	c.index = filepath.Join(o.Dir, "index.json")
	err := c.load()
	c.mu.Lock()
	c.collect = true // blobs a crash left without an index entry
	c.trimLocked()
	c.saveLocked()
	c.mu.Unlock()
	return c, err
}

// checkpointIndex is the persisted form of a store.
type checkpointIndex struct {
	Version int     `json:"version"`
	NextID  int     `json:"next_id"`
	Turns   []*Turn `json:"turns"`
}

func (c *Checkpoints) load() error {
	data, err := os.ReadFile(c.index)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("checkpoints: %w", err)
	}
	var idx checkpointIndex
	if err := json.Unmarshal(data, &idx); err != nil || idx.Version != 1 {
		os.Rename(c.index, c.index+".damaged")
		return fmt.Errorf("checkpoints: unreadable index set aside (%v)", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID = idx.NextID
	for _, t := range idx.Turns {
		if t == nil {
			continue
		}
		t.index = map[string]*fileChange{}
		for _, ch := range t.Changes {
			if ch.Before.Exists && !ch.Before.TooLarge && !c.blobs.has(ch.Before.Hash) {
				ch.Before.TooLarge = true // its snapshot is gone: not restorable
			}
			t.index[ch.Abs] = ch
			c.bytes += ch.Before.Size
		}
		c.turns = append(c.turns, t)
		c.nextID = max(c.nextID, t.ID)
	}
	return nil
}

// saveLocked writes the index (atomically, owner-only) and removes blobs
// no turn needs any more. Failures are logged once: checkpoints then last
// only as long as the process.
func (c *Checkpoints) saveLocked() {
	var err error
	if c.index != "" {
		var data []byte
		if data, err = json.Marshal(checkpointIndex{Version: 1, NextID: c.nextID, Turns: c.turns}); err == nil {
			err = writePrivate(c.index, data)
		}
	}
	if err == nil {
		if c.collect {
			c.collect = false
			c.blobs.keep(c.referencedLocked())
		}
		return
	}
	if !c.warned {
		c.warned = true
		slog.Warn("checkpoints could not be saved; they last only until Blitz exits", "error", err)
	}
}

func (c *Checkpoints) referencedLocked() map[string]bool {
	refs := map[string]bool{}
	for _, t := range c.turns {
		for _, ch := range t.Changes {
			if ch.Before.Hash != "" {
				refs[ch.Before.Hash] = true
			}
		}
	}
	return refs
}

// Begin starts a new turn; later changes are grouped under it.
func (c *Checkpoints) Begin(label string) { c.BeginTurn("", -1, label) }

// BeginTurn starts a new turn for the prompt at index prompt of session's
// transcript.
func (c *Checkpoints) BeginTurn(session string, prompt int, label string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.beginLocked(session, prompt, label, "")
}

// BeginWorkerTurn starts the turn of worker run run, in its session: its
// changes are kept apart from the interactive sessions' (BL-WK-01).
func (c *Checkpoints) BeginWorkerTurn(session, run, label string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.beginLocked(session, -1, label, run)
}

func (c *Checkpoints) beginLocked(session string, prompt int, label, run string) *Turn {
	// Reuse an empty current turn rather than stacking empty ones: the
	// same session's (or one without), not a turn another session is in.
	if n := len(c.turns); n > 0 && len(c.turns[n-1].Changes) == 0 && (c.turns[n-1].Session == session || c.turns[n-1].Session == "") {
		t := c.turns[n-1]
		t.Session, t.Prompt, t.Label, t.Run, t.Started = session, prompt, label, run, time.Now()
		return t
	}
	c.nextID++
	t := &Turn{ID: c.nextID, Session: session, Run: run, Prompt: prompt, Label: label, Started: time.Now(), index: map[string]*fileChange{}}
	c.turns = append(c.turns, t)
	return t
}

// before is called by the workspace before it modifies abs, with its
// current content. It reports whether a new entry was added for this turn.
func (c *Checkpoints) before(session, abs, display string, st fileState, data []byte) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.currentLocked(session)
	if t == nil {
		t = c.beginLocked(session, -1, "(no prompt)", "")
	}
	if _, seen := t.index[abs]; seen {
		return false // keep the state from before the turn's first change
	}
	if st.Exists && !st.TooLarge {
		hash, err := c.blobs.put(data)
		if err != nil {
			slog.Warn("checkpoint snapshot failed; this change can't be undone", "path", display, "error", err)
			st.TooLarge = true
		} else {
			st.Hash, st.Size = hash, int64(len(data))
		}
	}
	ch := &fileChange{Abs: abs, Display: display, Before: st}
	t.Changes = append(t.Changes, ch)
	t.index[abs] = ch
	c.bytes += st.Size
	c.trimLocked()
	return true
}

// currentLocked is the turn a change by session goes to: session's latest,
// so turns running at once (a worker's, an interactive one) keep their
// own changes; the latest of all when session has none (or is "").
func (c *Checkpoints) currentLocked(session string) *Turn {
	if session != "" {
		for i := len(c.turns) - 1; i >= 0; i-- {
			if c.turns[i].Session == session {
				return c.turns[i]
			}
		}
	}
	if n := len(c.turns); n > 0 {
		return c.turns[n-1]
	}
	return nil
}

// discard drops the current turn's entry for abs after a failed change.
func (c *Checkpoints) discard(session, abs string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.currentLocked(session)
	if t == nil {
		return
	}
	ch := t.index[abs]
	if ch == nil {
		return
	}
	delete(t.index, abs)
	c.collect = true
	for i, x := range t.Changes {
		if x == ch {
			t.Changes = append(t.Changes[:i], t.Changes[i+1:]...)
			break
		}
	}
	c.bytes -= ch.Before.Size
	c.saveLocked()
}

// after records the state the workspace left abs in.
func (c *Checkpoints) after(session, abs string, data []byte, exists bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if t := c.currentLocked(session); t != nil {
		if ch := t.index[abs]; ch != nil {
			ch.AfterExists = exists
			ch.AfterHash = hashOf(data)
			c.saveLocked()
		}
	}
}

// trimLocked drops turns older than the age limit, then the oldest turns
// while over the size budget, always keeping the most recent one.
func (c *Checkpoints) trimLocked() {
	cutoff := time.Now().Add(-c.maxAge)
	for len(c.turns) > 1 && (c.bytes > c.maxBytes || c.turns[0].Started.Before(cutoff)) {
		for _, ch := range c.turns[0].Changes {
			c.bytes -= ch.Before.Size
		}
		c.turns = c.turns[1:]
		c.collect = true
	}
}

// List returns turns with changes, newest first.
func (c *Checkpoints) List() []TurnSummary {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []TurnSummary
	for i := len(c.turns) - 1; i >= 0; i-- {
		if len(c.turns[i].Changes) > 0 && c.turns[i].Run == "" {
			out = append(out, summarize(c.turns[i]))
		}
	}
	return out
}

func summarize(t *Turn) TurnSummary {
	s := TurnSummary{ID: t.ID, Session: t.Session, Prompt: t.Prompt, Label: t.Label, Time: t.Started}
	for _, ch := range t.Changes {
		s.Files = append(s.Files, ch.Display)
	}
	return s
}

// Undo reverts the most recent turn that changed files. Unless force is set,
// it refuses when a file no longer matches what the tools wrote, so edits
// made afterwards (by the shell or the user) aren't silently discarded.
func (c *Checkpoints) Undo(force bool) (UndoResult, error) {
	if c == nil {
		return UndoResult{}, errors.New("checkpoints are disabled")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := len(c.turns) - 1; i >= 0; i-- {
		if len(c.turns[i].Changes) > 0 && c.turns[i].Run == "" { // workers' have their own undo
			t := c.turns[i]
			res, applied, err := c.restoreLocked([]*Turn{t}, force, "/undo --force")
			res.Turn = summarize(t)
			if applied {
				c.removeLocked([]*Turn{t})
			}
			return res, err
		}
	}
	return UndoResult{}, ErrNothingToUndo
}

// Rewind restores the files session's prompts changed from the prompt at
// transcript index prompt on, newest first: the workspace as it was
// before that prompt, as far as the file tools changed it. It refuses on
// conflicts as Undo does. Other sessions' changes are left alone (if they
// changed the same files, that shows as a conflict).
func (c *Checkpoints) Rewind(session string, prompt int, force bool) (UndoResult, error) {
	if c == nil {
		return UndoResult{}, errors.New("checkpoints are disabled")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var turns []*Turn
	for _, t := range c.turns {
		if t.Session == session && t.Prompt >= prompt && len(t.Changes) > 0 {
			turns = append(turns, t)
		}
	}
	if len(turns) == 0 {
		return UndoResult{}, ErrNothingToUndo
	}
	res, applied, err := c.restoreLocked(turns, force, "--force")
	if applied {
		c.removeLocked(turns)
	}
	return res, err
}

// RunFiles are the files worker run run changed, sorted.
func (c *Checkpoints) RunFiles(run string) []string {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, t := range c.turns {
		if t.Run != run {
			continue
		}
		for _, ch := range t.Changes {
			if !seen[ch.Display] {
				seen[ch.Display] = true
				out = append(out, ch.Display)
			}
		}
	}
	sort.Strings(out)
	return out
}

// UndoRun restores the files worker run run changed, as they were before
// it, refusing on conflicts as Undo does (BL-WK-02).
func (c *Checkpoints) UndoRun(run string, force bool) (UndoResult, error) {
	if c == nil {
		return UndoResult{}, errors.New("checkpoints are disabled")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var turns []*Turn
	for _, t := range c.turns {
		if t.Run == run && len(t.Changes) > 0 {
			turns = append(turns, t)
		}
	}
	if len(turns) == 0 {
		return UndoResult{}, ErrNothingToUndo
	}
	res, applied, err := c.restoreLocked(turns, force, "--force")
	res.Turn = summarize(turns[len(turns)-1])
	if applied {
		c.removeLocked(turns)
	}
	return res, err
}

// Detach marks session's turns from transcript index prompt on as made
// before whatever prompt comes next at that index, once the conversation
// was rewound without the files: a later rewind to an earlier prompt still
// restores them, one to the new prompt doesn't.
func (c *Checkpoints) Detach(session string, prompt int) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	changed := false
	for _, t := range c.turns {
		if t.Session == session && t.Prompt >= prompt {
			t.Prompt, changed = prompt-1, true
		}
	}
	if changed {
		c.saveLocked()
	}
}

// restoreLocked puts every file turns changed back as it was before the
// earliest of them, checking first that each is still as the latest left it.
// applied reports whether it got past the checks and restored (the turns
// are then done with, even if some files failed).
func (c *Checkpoints) restoreLocked(turns []*Turn, force bool, forceHint string) (res UndoResult, applied bool, err error) {
	type plan struct {
		display string
		abs     string
		target  fileState // before the earliest change
		latest  *fileChange
	}
	byFile := map[string]*plan{}
	var order []string
	for _, t := range turns { // oldest first
		for _, ch := range t.Changes {
			p := byFile[ch.Abs]
			if p == nil {
				p = &plan{display: ch.Display, abs: ch.Abs, target: ch.Before}
				byFile[ch.Abs] = p
				order = append(order, ch.Abs)
			}
			p.latest = ch
		}
	}
	var conflicts, unrestorable []string
	for _, abs := range order {
		p := byFile[abs]
		if p.target.TooLarge {
			unrestorable = append(unrestorable, p.display)
			continue
		}
		data, err := os.ReadFile(abs)
		exists := err == nil
		if exists != p.latest.AfterExists || (exists && hashOf(data) != p.latest.AfterHash) {
			conflicts = append(conflicts, p.display)
		}
	}
	if len(unrestorable) > 0 && !force {
		return UndoResult{}, false, fmt.Errorf("cannot restore files larger than the snapshot limit: %s (use %s to restore the rest)", strings.Join(unrestorable, ", "), forceHint)
	}
	if len(conflicts) > 0 && !force {
		return UndoResult{}, false, fmt.Errorf("%w: %s (use %s to overwrite)", api.ErrUndoConflict, strings.Join(conflicts, ", "), forceHint)
	}
	var errs []error
	for i := len(order) - 1; i >= 0; i-- {
		p := byFile[order[i]]
		if p.target.TooLarge {
			continue
		}
		var data []byte
		if p.target.Exists {
			var err error
			if data, err = c.blobs.get(p.target.Hash); err != nil {
				errs = append(errs, fmt.Errorf("%s: snapshot: %w", p.display, err))
				continue
			}
		}
		if err := c.ws.restore(p.abs, p.target, data); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p.display, err))
			continue
		}
		res.Restored = append(res.Restored, p.display)
	}
	sort.Strings(res.Restored)
	return res, true, errors.Join(errs...)
}

// removeLocked drops turns (after restoring them) and saves.
func (c *Checkpoints) removeLocked(drop []*Turn) {
	gone := map[*Turn]bool{}
	c.collect = true
	for _, t := range drop {
		gone[t] = true
		for _, ch := range t.Changes {
			c.bytes -= ch.Before.Size
		}
	}
	kept := c.turns[:0]
	for _, t := range c.turns {
		if !gone[t] {
			kept = append(kept, t)
		}
	}
	c.turns = kept
	c.saveLocked()
}

// SessionDiff returns a unified diff of every file session's turns changed
// ("" for every session), from its earliest snapshot to its current content.
func (c *Checkpoints) SessionDiff(session string) string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	first := map[string]*fileChange{}
	for _, t := range c.turns {
		if session != "" && t.Session != session {
			continue
		}
		for _, ch := range t.Changes {
			if first[ch.Abs] == nil {
				first[ch.Abs] = ch
			}
		}
	}
	chs := make([]*fileChange, 0, len(first))
	for _, ch := range first {
		chs = append(chs, ch)
	}
	c.mu.Unlock()
	sort.Slice(chs, func(i, j int) bool { return chs[i].Display < chs[j].Display })

	var sb strings.Builder
	for _, ch := range chs {
		if ch.Before.TooLarge {
			fmt.Fprintf(&sb, "# %s: too large to diff\n", ch.Display)
			continue
		}
		var before []byte
		if ch.Before.Exists {
			var err error
			if before, err = c.blobs.get(ch.Before.Hash); err != nil {
				fmt.Fprintf(&sb, "# %s: snapshot missing\n", ch.Display)
				continue
			}
		}
		cur, _ := os.ReadFile(ch.Abs)
		sb.WriteString(unifiedDiff(ch.Display, string(before), string(cur)))
	}
	return sb.String()
}

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// blobStore keeps snapshot contents by hash.
type blobStore interface {
	put(data []byte) (string, error)
	get(hash string) ([]byte, error)
	has(hash string) bool
	// keep removes blobs not in refs.
	keep(refs map[string]bool)
}

type memBlobs struct{ m map[string][]byte }

func (b *memBlobs) put(data []byte) (string, error) {
	h := hashOf(data)
	if _, ok := b.m[h]; !ok {
		b.m[h] = append([]byte(nil), data...)
	}
	return h, nil
}

func (b *memBlobs) get(hash string) ([]byte, error) {
	if d, ok := b.m[hash]; ok {
		return d, nil
	}
	return nil, fs.ErrNotExist
}

func (b *memBlobs) has(hash string) bool { _, ok := b.m[hash]; return ok }

func (b *memBlobs) keep(refs map[string]bool) {
	for h := range b.m {
		if !refs[h] {
			delete(b.m, h)
		}
	}
}

// diskBlobs is a directory of blobs named by their hash, owner-only.
type diskBlobs string

func (d diskBlobs) path(hash string) (string, error) {
	if len(hash) != 64 || strings.Trim(hash, "0123456789abcdef") != "" {
		return "", fmt.Errorf("bad snapshot hash %q", hash)
	}
	return filepath.Join(string(d), hash), nil
}

func (d diskBlobs) put(data []byte) (string, error) {
	h := hashOf(data)
	p, _ := d.path(h)
	if _, err := os.Stat(p); err == nil {
		return h, nil
	}
	return h, writePrivate(p, data)
}

func (d diskBlobs) get(hash string) ([]byte, error) {
	p, err := d.path(hash)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err == nil && hashOf(data) != hash {
		return nil, fmt.Errorf("snapshot %s is damaged", hash[:12])
	}
	return data, err
}

func (d diskBlobs) has(hash string) bool {
	p, err := d.path(hash)
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

func (d diskBlobs) keep(refs map[string]bool) {
	entries, err := os.ReadDir(string(d))
	if err != nil {
		return
	}
	for _, e := range entries {
		if !refs[e.Name()] {
			os.Remove(filepath.Join(string(d), e.Name()))
		}
	}
}

// writePrivate writes a file atomically, owner-only.
func writePrivate(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
