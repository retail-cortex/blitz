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

package atomicfile

import (
	"io/fs"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWrite(t *testing.T) {
	tests := []struct {
		name string
		old  bool
		perm fs.FileMode
	}{
		{name: "new file owner-only", old: false, perm: 0o600},
		{name: "replace existing readable by others", old: true, perm: 0o644},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "SKILL.md")
			if tt.old {
				require.NoError(t, os.WriteFile(p, []byte("old"), 0o600))
			}
			require.NoError(t, Write(p, []byte("new"), tt.perm))
			data, err := os.ReadFile(p)
			require.NoError(t, err)
			assert.Equal(t, "new", string(data))
			if goruntime.GOOS != "windows" {
				info, err := os.Stat(p)
				require.NoError(t, err)
				assert.Equal(t, tt.perm, info.Mode().Perm())
			}
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			assert.Len(t, entries, 1, "no temporary file left")
		})
	}

	t.Run("missing parent directory", func(t *testing.T) {
		assert.Error(t, Write(filepath.Join(t.TempDir(), "missing", "x"), nil, 0o600))
	})

	t.Run("target is an existing directory", func(t *testing.T) {
		dir := t.TempDir()
		taken := filepath.Join(dir, "taken")
		require.NoError(t, os.Mkdir(taken, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(taken, "x"), nil, 0o600))
		assert.Error(t, Write(taken, []byte("x"), 0o600))
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		assert.Len(t, entries, 1, "no temporary file left on rename error")
	})
}

func TestMkdirWrite(t *testing.T) {
	t.Run("creates parent directories", func(t *testing.T) {
		base := t.TempDir()
		p := filepath.Join(base, "nested", "dir", "file.txt")
		require.NoError(t, MkdirWrite(p, []byte("hello"), 0o700, 0o600))
		data, err := os.ReadFile(p)
		require.NoError(t, err)
		assert.Equal(t, "hello", string(data))
		if goruntime.GOOS != "windows" {
			info, err := os.Stat(p)
			require.NoError(t, err)
			assert.Equal(t, fs.FileMode(0o600), info.Mode().Perm())
			dinfo, err := os.Stat(filepath.Dir(p))
			require.NoError(t, err)
			assert.Equal(t, fs.FileMode(0o700), dinfo.Mode().Perm())
		}
	})

	t.Run("parent path is a file", func(t *testing.T) {
		dir := t.TempDir()
		file := filepath.Join(dir, "file")
		require.NoError(t, os.WriteFile(file, nil, 0o600))
		assert.Error(t, MkdirWrite(filepath.Join(file, "child"), []byte("x"), 0o700, 0o600))
	})
}

func TestWriteJSON(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "f.json")
	require.NoError(t, WriteJSON(p, map[string]int{"a": 1}, 0o600))
	require.NoError(t, WriteJSON(p, map[string]int{"a": 2}, 0o600))

	data, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, "{\n  \"a\": 2\n}\n", string(data))
	if goruntime.GOOS != "windows" {
		info, err := os.Stat(p)
		require.NoError(t, err)
		assert.Equal(t, fs.FileMode(0o600), info.Mode().Perm())
	}

	t.Run("unmarshalable value", func(t *testing.T) {
		bad := filepath.Join(dir, "chan.json")
		assert.Error(t, WriteJSON(bad, make(chan int), 0o600))
		assert.NoFileExists(t, bad)
	})
}

func TestReadJSONOrQuarantine(t *testing.T) {
	type payload struct {
		A int `json:"a"`
	}
	tests := []struct {
		name      string
		setup     func(t *testing.T, p string)
		wantOK    bool
		wantVal   payload
		wantErr   string
		wantAside bool
	}{
		{
			name:    "missing file",
			setup:   func(*testing.T, string) {},
			wantOK:  false,
			wantVal: payload{},
		},
		{
			name: "valid JSON",
			setup: func(t *testing.T, p string) {
				require.NoError(t, os.WriteFile(p, []byte(`{"a":42}`), 0o600))
			},
			wantOK:  true,
			wantVal: payload{A: 42},
		},
		{
			name: "corrupt JSON is quarantined",
			setup: func(t *testing.T, p string) {
				require.NoError(t, os.WriteFile(p, []byte("{not json"), 0o600))
			},
			wantOK:    false,
			wantErr:   "damaged JSON in state.json (set aside)",
			wantAside: true,
		},
		{
			name: "corrupt JSON when quarantine rename fails",
			setup: func(t *testing.T, p string) {
				require.NoError(t, os.WriteFile(p, []byte("{not json"), 0o600))
				// A non-empty directory at p+".damaged" blocks os.Rename on all platforms.
				damaged := p + ".damaged"
				require.NoError(t, os.Mkdir(damaged, 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(damaged, "keep"), []byte("x"), 0o600))
			},
			wantOK:  false,
			wantErr: "damaged JSON in state.json (set aside)",
		},
		{
			name: "unreadable file",
			setup: func(t *testing.T, p string) {
				require.NoError(t, os.Mkdir(p, 0o700))
			},
			wantOK:  false,
			wantErr: "reading state.json",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "state.json")
			tc.setup(t, p)

			var got payload
			ok, err := ReadJSONOrQuarantine(p, &got)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.wantVal, got)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			if tc.wantAside {
				assert.NoFileExists(t, p)
				data, rerr := os.ReadFile(p + ".damaged")
				require.NoError(t, rerr)
				assert.Equal(t, "{not json", string(data))
			}
		})
	}
}
