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

package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRefs(t *testing.T) {
	if Ref("global/llm.gemini.api_key") != "keychain:global/llm.gemini.api_key" {
		t.Error(Ref("x"))
	}
	for v, want := range map[string]string{"keychain:a/b": "a/b", "keychain:": "", "sk-plain": "", "xor:0102": ""} {
		got, ok := ParseRef(v)
		if got != want || ok != (want != "") {
			t.Errorf("ParseRef(%q) = %q, %v", v, got, ok)
		}
	}
}

// exercise runs a store through set, get, replace and delete.
func exercise(t *testing.T, s Store, name string) {
	t.Helper()
	if _, err := s.Get(name); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get before set: %v", err)
	}
	for _, v := range []string{"sk-first \"quoted\" \\ value", "sk-second"} {
		if err := s.Set(name, v); err != nil {
			t.Fatal(err)
		}
		if got, err := s.Get(name); err != nil || got != v {
			t.Fatalf("get = %q, %v; want %q", got, err, v)
		}
	}
	if err := s.Delete(name); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(name); !errors.Is(err, ErrNotFound) {
		t.Errorf("get after delete: %v", err)
	}
	if err := s.Delete(name); err != nil {
		t.Errorf("deleting what's gone: %v", err)
	}
}

func TestMemory(t *testing.T) { exercise(t, &Memory{}, "a") }

func TestFileStoreIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blitz", "secrets.toml")
	exercise(t, &FileStore{Path: path}, "global/llm.gemini.api_key")
	s := &FileStore{Path: path}
	if err := s.Set("k", "v"); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]os.FileMode{path: 0o600, filepath.Dir(path): 0o700} {
		if info, err := os.Stat(p); err != nil || info.Mode().Perm() != want {
			t.Errorf("%s: %v %v, want %v", p, info.Mode().Perm(), err, want)
		}
	}
}

// The OS's own store; opt in, since it writes to your keychain (and
// removes what it wrote).
func TestOSStore(t *testing.T) {
	if os.Getenv("BLITZ_KEYCHAIN_TESTS") == "" {
		t.Skip("set BLITZ_KEYCHAIN_TESTS=1 (and --test_env=HOME=$HOME: the keychain is in your home) to use this machine's keychain")
	}
	s := Default(t.TempDir())
	t.Logf("store: %s", s.Kind())
	exercise(t, s, "test/blitz-secrets-test")
}
