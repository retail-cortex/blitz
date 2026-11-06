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

// Package modelfacts keeps what Blitz has learned about models from their
// own refusals (spec_056_conversation_adapters CA-13, CA-14): a model that
// refused tools, images, a setting or a stream, or stated its context
// window or maximum output, has the fact saved per provider and model in
// ~/.blitz/models.learned.toml, with when and why, so every workspace and
// every restart knows it. A fact is tried again after Recheck, and `blitz
// models forget <ref>` clears a model's.
package modelfacts

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
)

// Recheck is how long a learned fact holds before the model is asked
// again: servers and models are updated.
const Recheck = 30 * 24 * time.Hour

// The facts a refusal teaches.
const (
	NoTools       = "no_tools"         // the model doesn't call tools
	NoImages      = "no_images"        // the model doesn't read images
	NoStream      = "no_stream"        // the model (or its server) doesn't stream
	NoParam       = "no_param"         // the model refuses a setting: Value names it
	ContextWindow = "context_window"   // the model's context window: Value in tokens
	MaxOutput     = "max_output"       // the model's maximum output: Value in tokens
	NoSystem      = "no_system"        // the model refuses system instructions
	NoStructured  = "no_structured"    // the model refuses a response schema
	NoThinking    = "no_thinking"      // the model refuses thinking or effort settings
	NoProvTool    = "no_provider_tool" // the model refuses a provider tool: Value names it
)

// Fact is one thing learned about a model.
type Fact struct {
	Kind  string    `toml:"kind"`
	Value string    `toml:"value,omitempty"`
	Why   string    `toml:"why"`
	At    time.Time `toml:"at"`
}

// Store is the learned facts, in one file.
type Store struct {
	path string

	mu    sync.Mutex
	stamp time.Time // the file's modification time when last read
	facts map[string][]Fact
}

// Path is where the facts are kept: models.learned.toml in dir (Blitz's
// home, ~/.blitz).
func Path(dir string) string { return filepath.Join(dir, "models.learned.toml") }

// Open is the store at path; the file is read on first use and again
// whenever it changes (another process learned or forgot something).
func Open(path string) *Store { return &Store{path: path} }

// Key names a model: its provider and its name, lower case.
func Key(provider, model string) string {
	return strings.ToLower(strings.TrimSpace(provider)) + "/" + strings.ToLower(strings.TrimSpace(model))
}

// file is the TOML on disk: one table per model, each a list of facts.
type file struct {
	Models map[string][]Fact `toml:"models"`
}

// load reads the file when it changed since it was last read. Callers hold
// s.mu.
func (s *Store) load() {
	info, err := os.Stat(s.path)
	if err != nil {
		if s.facts == nil || errors.Is(err, fs.ErrNotExist) {
			s.facts, s.stamp = map[string][]Fact{}, time.Time{}
		}
		return
	}
	if s.facts != nil && info.ModTime().Equal(s.stamp) {
		return
	}
	var f file
	if _, err := toml.DecodeFile(s.path, &f); err != nil {
		f.Models = nil // a broken file teaches nothing; the next fact rewrites it
	}
	s.facts, s.stamp = f.Models, info.ModTime()
	if s.facts == nil {
		s.facts = map[string][]Fact{}
	}
}

// Facts are what's known of provider's model, without those due to be
// asked again.
func (s *Store) Facts(provider, model string, now time.Time) []Fact {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	var out []Fact
	for _, f := range s.facts[Key(provider, model)] {
		if now.Sub(f.At) < Recheck {
			out = append(out, f)
		}
	}
	return out
}

// Learn saves f for provider's model, replacing an earlier fact of its
// kind (and value, for the kinds that name one). It reports whether the
// fact is new, so the person is told once.
func (s *Store) Learn(provider, model string, f Fact) (bool, error) {
	if s == nil {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	key := Key(provider, model)
	list := s.facts[key]
	fresh := true
	for i, old := range list {
		if old.Kind == f.Kind && (old.Value == f.Value || !named(f.Kind)) {
			fresh = old.Value != f.Value || time.Since(old.At) >= Recheck
			list = append(list[:i:i], list[i+1:]...)
			break
		}
	}
	s.facts[key] = append(list, f)
	return fresh, s.save()
}

// named reports whether facts of kind are told apart by their value.
func named(kind string) bool { return kind == NoParam || kind == NoProvTool }

// Forget clears what's known of the models ref names: a provider's model
// ("anthropic/claude-x"), or a bare model name, under any provider. It
// returns the keys it cleared.
func (s *Store) Forget(ref string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	ref = strings.ToLower(strings.TrimSpace(ref))
	var gone []string
	for key := range s.facts {
		_, name, _ := strings.Cut(key, "/")
		if key == ref || name == ref {
			gone = append(gone, key)
			delete(s.facts, key)
		}
	}
	sort.Strings(gone)
	if len(gone) == 0 {
		return nil, nil
	}
	return gone, s.save()
}

// All is every model's facts, by key, for listing.
func (s *Store) All() map[string][]Fact {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	out := make(map[string][]Fact, len(s.facts))
	for k, v := range s.facts {
		out[k] = append([]Fact(nil), v...)
	}
	return out
}

// save writes the facts whole, through a temporary file renamed over the
// old one. Callers hold s.mu.
func (s *Store) save() error {
	var buf bytes.Buffer
	buf.WriteString("# What Blitz learned about models from their refusals (spec_056 CA-13).\n# `blitz models forget <model>` clears a model's; each is tried again after 30 days.\n\n")
	if err := toml.NewEncoder(&buf).Encode(file{Models: s.facts}); err != nil {
		return fmt.Errorf("models.learned.toml: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".models.learned-*.toml")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if info, err := os.Stat(s.path); err == nil {
		s.stamp = info.ModTime()
	}
	return nil
}
