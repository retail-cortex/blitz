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
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
)

// reopen opens the workspace and its persisted checkpoints again, as a
// later process would.
func reopen(t *testing.T, dir, store string) (*Workspace, *Checkpoints) {
	t.Helper()
	ws, err := NewWorkspace(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	cp, err := OpenCheckpoints(ws, CheckpointOptions{Dir: store})
	if err != nil {
		t.Fatal(err)
	}
	return ws, cp
}

func readString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

func TestCheckpointsOutliveTheProcess(t *testing.T) {
	dir, store := t.TempDir(), filepath.Join(t.TempDir(), "cp")
	writeFile(t, filepath.Join(dir, "f.txt"), "v1\n")
	ws, cp := reopen(t, dir, store)
	cp.BeginTurn("s1", 0, "first")
	ws.WriteFileAtomic("f.txt", []byte("v2\n"))
	cp.BeginTurn("s1", 2, "second")
	ws.CreateExclusive("new.txt", []byte("n\n"))

	for _, p := range []string{store, filepath.Join(store, "index.json"), filepath.Join(store, "blobs")} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s is %v, not owner-only", p, info.Mode().Perm())
		}
	}
	blobs, _ := os.ReadDir(filepath.Join(store, "blobs"))
	if len(blobs) != 1 {
		t.Fatalf("blobs %v", blobs)
	}
	if info, _ := blobs[0].Info(); info.Mode().Perm() != 0o600 {
		t.Errorf("blob mode %v", info.Mode().Perm())
	}

	// A later process sees both turns, and undoes them from the stored snapshots.
	ws.Close()
	_, cp2 := reopen(t, dir, store)
	l := cp2.List()
	if len(l) != 2 || l[0].Label != "second" || l[0].Session != "s1" || l[0].Prompt != 2 || l[1].Prompt != 0 {
		t.Fatalf("after reopening: %+v", l)
	}
	if d := cp2.SessionDiff("s1"); !strings.Contains(d, "-v1") || !strings.Contains(d, "+v2") || !strings.Contains(d, "+n") {
		t.Errorf("session diff:\n%s", d)
	}
	if d := cp2.SessionDiff("other"); d != "" {
		t.Errorf("another session's diff: %q", d)
	}
	for range 2 {
		if _, err := cp2.Undo(false); err != nil {
			t.Fatal(err)
		}
	}
	if got := readString(t, filepath.Join(dir, "f.txt")); got != "v1\n" {
		t.Errorf("f.txt %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(err) {
		t.Error("new.txt not removed")
	}
	// Undone turns' snapshots go.
	if blobs, _ := os.ReadDir(filepath.Join(store, "blobs")); len(blobs) != 0 {
		t.Errorf("blobs left: %v", blobs)
	}
	_, cp3 := reopen(t, dir, store)
	if l := cp3.List(); len(l) != 0 {
		t.Errorf("undone turns came back: %+v", l)
	}
}

func TestRewindRestoresEveryChangeFromAPrompt(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	cp := NewCheckpoints(ws, 0)
	writeFile(t, filepath.Join(dir, "a.txt"), "a0\n")
	writeFile(t, filepath.Join(dir, "b.txt"), "b0\n")
	cp.BeginTurn("s", 0, "p0")
	ws.WriteFileAtomic("a.txt", []byte("a1\n"))
	cp.BeginTurn("s", 2, "p1")
	ws.WriteFileAtomic("a.txt", []byte("a2\n"))
	ws.CreateExclusive("c.txt", []byte("c\n"))
	cp.BeginTurn("other", 0, "elsewhere")
	ws.WriteFileAtomic("b.txt", []byte("b1\n"))
	cp.BeginTurn("s", 4, "p2")
	ws.WriteFileAtomic("a.txt", []byte("a3\n"))

	res, err := cp.Rewind("s", 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := readString(t, filepath.Join(dir, "a.txt")); got != "a1\n" {
		t.Errorf("a.txt %q, want the state before prompt 2", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "c.txt")); !os.IsNotExist(err) {
		t.Error("c.txt, created from prompt 2 on, is still there")
	}
	if got := readString(t, filepath.Join(dir, "b.txt")); got != "b1\n" {
		t.Errorf("another session's change was undone: %q", got)
	}
	if strings.Join(res.Restored, ",") != "a.txt,c.txt" {
		t.Errorf("restored %v", res.Restored)
	}
	var left []string
	for _, s := range cp.List() {
		left = append(left, s.Label)
	}
	if strings.Join(left, ",") != "elsewhere,p0" {
		t.Errorf("turns left %v", left)
	}
	if _, err := cp.Rewind("s", 2, false); !errors.Is(err, ErrNothingToUndo) {
		t.Errorf("nothing left to rewind: %v", err)
	}
}

func TestRewindChecksEveryFileFirst(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	cp := NewCheckpoints(ws, 0)
	writeFile(t, filepath.Join(dir, "a.txt"), "a0\n")
	writeFile(t, filepath.Join(dir, "b.txt"), "b0\n")
	cp.BeginTurn("s", 0, "p0")
	ws.WriteFileAtomic("a.txt", []byte("a1\n"))
	cp.BeginTurn("s", 2, "p1")
	ws.WriteFileAtomic("b.txt", []byte("b1\n"))
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("mine\n"), 0o644)

	if _, err := cp.Rewind("s", 0, false); !errors.Is(err, api.ErrUndoConflict) || !strings.Contains(err.Error(), "b.txt") {
		t.Fatalf("conflict: %v", err)
	}
	if got := readString(t, filepath.Join(dir, "a.txt")); got != "a1\n" {
		t.Error("a file was restored before the conflict was found")
	}
	if _, err := cp.Rewind("s", 0, true); err != nil {
		t.Fatal(err)
	}
	if readString(t, filepath.Join(dir, "a.txt")) != "a0\n" || readString(t, filepath.Join(dir, "b.txt")) != "b0\n" {
		t.Error("forced rewind didn't restore both")
	}
}

// After the conversation is rewound without the files, the changes made
// from that prompt on belong before the next prompt.
func TestDetachAfterAConversationRewind(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	cp := NewCheckpoints(ws, 0)
	writeFile(t, filepath.Join(dir, "a.txt"), "a0\n")
	cp.BeginTurn("s", 0, "p0")
	ws.WriteFileAtomic("a.txt", []byte("a1\n"))
	cp.BeginTurn("s", 2, "p1")
	ws.WriteFileAtomic("a.txt", []byte("a2\n"))
	cp.Detach("s", 2)
	cp.BeginTurn("s", 2, "new p1")
	ws.WriteFileAtomic("a.txt", []byte("a3\n"))

	if _, err := cp.Rewind("s", 2, false); err != nil {
		t.Fatal(err)
	}
	if got := readString(t, filepath.Join(dir, "a.txt")); got != "a2\n" {
		t.Fatalf("rewinding the new prompt: %q (the detached change must stay)", got)
	}
	if _, err := cp.Rewind("s", 0, false); err != nil {
		t.Fatal(err)
	}
	if got := readString(t, filepath.Join(dir, "a.txt")); got != "a0\n" {
		t.Fatalf("rewinding the first prompt: %q", got)
	}
}

func TestPersistedCheckpointsAgeOutAndSurviveDamage(t *testing.T) {
	dir, store := t.TempDir(), filepath.Join(t.TempDir(), "cp")
	writeFile(t, filepath.Join(dir, "a.txt"), "a0\n")
	writeFile(t, filepath.Join(dir, "b.txt"), "b0\n")
	ws, cp := reopen(t, dir, store)
	cp.BeginTurn("s", 0, "old")
	ws.WriteFileAtomic("a.txt", []byte("a1\n"))
	cp.BeginTurn("s", 2, "new")
	ws.WriteFileAtomic("b.txt", []byte("b1\n"))
	ws.Close()

	// Age the first turn past the limit.
	path := filepath.Join(store, "index.json")
	var idx checkpointIndex
	json.Unmarshal([]byte(readString(t, path)), &idx)
	idx.Turns[0].Started = time.Now().Add(-40 * 24 * time.Hour)
	data, _ := json.Marshal(idx)
	os.WriteFile(path, data, 0o600)

	_, cp2 := reopen(t, dir, store)
	if l := cp2.List(); len(l) != 1 || l[0].Label != "new" {
		t.Fatalf("aged turn kept: %+v", l)
	}
	if blobs, _ := os.ReadDir(filepath.Join(store, "blobs")); len(blobs) != 1 {
		t.Errorf("the aged turn's snapshot wasn't removed: %d blobs", len(blobs))
	}

	// A missing snapshot can't be restored.
	blobs, _ := os.ReadDir(filepath.Join(store, "blobs"))
	os.Remove(filepath.Join(store, "blobs", blobs[0].Name()))
	_, cp3 := reopen(t, dir, store)
	if _, err := cp3.Undo(false); err == nil || !strings.Contains(err.Error(), "snapshot limit") {
		t.Errorf("undo without its snapshot: %v", err)
	}

	// A damaged index is set aside; the store starts empty.
	os.WriteFile(path, []byte("{nope"), 0o600)
	ws4, err := NewWorkspace(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer ws4.Close()
	cp4, err := OpenCheckpoints(ws4, CheckpointOptions{Dir: store})
	if err == nil || len(cp4.List()) != 0 {
		t.Fatalf("damaged index: %v %+v", err, cp4.List())
	}
	if _, err := os.Stat(path + ".damaged"); err != nil {
		t.Error("the damaged index wasn't kept aside")
	}
}
