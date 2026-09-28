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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRefs(t *testing.T) {
	assert.Equal(t, "keychain:global/llm.gemini.api_key", Ref("global/llm.gemini.api_key"), Ref("x"))
	for v, want := range map[string]string{"keychain:a/b": "a/b", "keychain:": "", "sk-plain": "", "xor:0102": ""} {
		t.Run(v, func(t *testing.T) {
			got, ok := ParseRef(v)
			assert.Equal(t, want, got, "ParseRef(%q) = %q, %v", v, got, ok)
			assert.Equal(t, (want != ""), ok, "ParseRef(%q) = %q, %v", v, got, ok)
		})
	}
}

// exercise runs a store through set, get, replace and delete.
func exercise(t *testing.T, s Store, name string) {
	t.Helper()
	_, err := s.Get(name)
	require.ErrorIs(t, err, ErrNotFound, "get before set: %v", err)
	for _, v := range []string{"sk-first \"quoted\" \\ value", "sk-second"} {
		t.Run(v, func(t *testing.T) {
			require.NoError(t, s.Set(name, v))
			got, err := s.Get(name)
			require.NoError(t, err, "get = %q, %v; want %q", got, err, v)
			require.Equal(t, v, got, "get = %q, %v; want %q", got, err, v)
		})
	}
	require.NoError(t, s.Delete(name))
	_, err = s.Get(name)
	assert.ErrorIs(t, err, ErrNotFound, "get after delete: %v", err)
	assert.NoError(t, s.Delete(name), "deleting what's gone")
}

func TestMemory(t *testing.T) { exercise(t, &Memory{}, "a") }

func TestFileStoreIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blitz", "secrets.toml")
	exercise(t, &FileStore{Path: path}, "global/llm.gemini.api_key")
	s := &FileStore{Path: path}
	require.NoError(t, s.Set("k", "v"))
	for p, want := range map[string]os.FileMode{path: 0o600, filepath.Dir(path): 0o700} {
		t.Run(p, func(t *testing.T) {
			info, err := os.Stat(p)
			assert.NoError(t, err, "%s: %v %v, want %v", p, info.Mode().Perm(), err, want)
			assert.Equal(t, want, info.Mode().Perm(), "%s: %v %v, want %v", p, info.Mode().Perm(), err, want)
		})
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
