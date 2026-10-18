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

package health

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// Keep is how long verdicts are kept, for trends (HL-11).
const Keep = 90 * 24 * time.Hour

// Verdict is the judge's on one item as it was (its hash), under one
// rubric version and model.
type Verdict struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Hash   string `json:"hash"`
	Rubric int    `json:"rubric"`
	// Model is the model that answered; Ref the one set to judge ("" the
	// default), which a check compares.
	Model  string    `json:"model"`
	Ref    string    `json:"ref,omitempty"`
	At     time.Time `json:"at"`
	Scores []Score   `json:"scores"`
	// Overall is 1–5.
	Overall float64 `json:"overall"`
	Summary string  `json:"summary,omitempty"`
	// Fixes are concrete changes, each a suggestion.
	Fixes   []string `json:"fixes,omitempty"`
	CostUSD float64  `json:"cost_usd,omitempty"`
}

// Key is the verdict's item's: kind and name.
func (v Verdict) Key() string { return v.Kind + "/" + v.Name }

// Score is one rubric line's.
type Score struct {
	Line   string `json:"line"`
	Score  int    `json:"score"`
	Reason string `json:"reason"`
}

// Grade is a score's letter (HL-12): A from 4.5, B from 3.5, C from 2.5,
// else D; "" for none.
func Grade(score float64) string {
	switch {
	case score <= 0:
		return ""
	case score >= 4.5:
		return "A"
	case score >= 3.5:
		return "B"
	case score >= 2.5:
		return "C"
	}
	return "D"
}

// problemScore is what an item that doesn't load counts as.
const problemScore = 1.0

// Scores are the workspace's and each kind's scores: their items'
// verdicts averaged, weighted by use (at least 1), an item with a problem
// counting as a D; items not judged leave them out. 0 is not judged.
func Scores(items []Item, latest map[string]Verdict) (overall float64, byKind map[string]float64) {
	byKind = map[string]float64{}
	sum, weight := 0.0, 0.0
	kindSum, kindWeight := map[string]float64{}, map[string]float64{}
	for _, it := range items {
		score := 0.0
		switch v, ok := latest[it.Key()]; {
		case it.Problem != "":
			score = problemScore
		case ok:
			score = v.Overall
		default:
			continue
		}
		w := float64(max(it.Uses, 1))
		sum += score * w
		weight += w
		kindSum[it.Kind] += score * w
		kindWeight[it.Kind] += w
	}
	for k, s := range kindSum {
		byKind[k] = s / kindWeight[k]
	}
	if weight > 0 {
		overall = sum / weight
	}
	return overall, byKind
}

// Due reports whether an item needs judging: it loads, and has no verdict
// for its content under this rubric version and model setting (or force).
func Due(it Item, latest map[string]Verdict, model string, force bool) bool {
	if it.Problem != "" {
		return false
	}
	v, ok := latest[it.Key()]
	return force || !ok || v.Hash != it.Hash || v.Rubric != RubricVersion || v.Ref != model
}

// Store keeps a workspace's verdicts and its checks' state, in a folder of
// the service's storage.
type Store struct {
	dir string
	mu  sync.Mutex
}

// NewStore is the store in dir (made when first written).
func NewStore(dir string) *Store { return &Store{dir: dir} }

func (s *Store) file() string { return filepath.Join(s.dir, "verdicts.jsonl") }

// All are the verdicts kept, oldest first.
func (s *Store) All() []Verdict {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readLocked()
}

func (s *Store) readLocked() []Verdict {
	data, err := os.ReadFile(s.file())
	if err != nil {
		return nil
	}
	var out []Verdict
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		var v Verdict
		if json.Unmarshal(sc.Bytes(), &v) == nil && v.Kind != "" {
			out = append(out, v)
		}
	}
	return out
}

// Latest is each item's latest verdict, by Key.
func (s *Store) Latest() map[string]Verdict {
	out := map[string]Verdict{}
	for _, v := range s.All() {
		if cur, ok := out[v.Key()]; !ok || !v.At.Before(cur.At) {
			out[v.Key()] = v
		}
	}
	return out
}

// History is an item's verdicts, oldest first.
func (s *Store) History(key string) []Verdict {
	var out []Verdict
	for _, v := range s.All() {
		if v.Key() == key {
			out = append(out, v)
		}
	}
	return out
}

// Add keeps v, and lets verdicts older than Keep go.
func (s *Store) Add(v Verdict) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := append(s.readLocked(), v)
	cut := time.Now().Add(-Keep)
	all = slices.DeleteFunc(all, func(x Verdict) bool { return x.At.Before(cut) })
	var b bytes.Buffer
	for _, x := range all {
		line, err := json.Marshal(x)
		if err != nil {
			return err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return writeFile(s.file(), b.Bytes())
}

// State is what the store remembers of the checks.
type State struct {
	// LastCheck is when the last check ended (zero: never checked, and
	// the schedule waits for a first check by hand).
	LastCheck time.Time `json:"last_check"`
	LastCost  float64   `json:"last_cost_usd,omitempty"`
	// Judged and Errors are what the last check judged, and the items it
	// couldn't, with why.
	Judged int      `json:"judged,omitempty"`
	Errors []string `json:"errors,omitempty"`
	// Skipped is why the last scheduled check didn't run.
	Skipped string `json:"skipped,omitempty"`
}

func (s *Store) stateFile() string { return filepath.Join(s.dir, "state.json") }

// State reads the checks' state.
func (s *Store) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	var st State
	if data, err := os.ReadFile(s.stateFile()); err == nil {
		_ = json.Unmarshal(data, &st)
	}
	return st
}

// SetState writes the checks' state.
func (s *Store) SetState(st State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return writeFile(s.stateFile(), data)
}

// writeFile writes data to path through a temporary file, so a reader
// never sees half of it.
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return errors.Join(err, os.Remove(tmp))
	}
	return nil
}
