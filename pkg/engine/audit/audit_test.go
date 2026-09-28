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

package audit

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/redact"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readEntries(t *testing.T, path string) []Entry {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	var out []Entry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e Entry
		require.NoError(t, json.Unmarshal(sc.Bytes(), &e), "bad line %q", sc.Text())
		out = append(out, e)
	}
	return out
}

func TestAuditLogWritesRedactedEntries(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "audit")
	l, err := Open(dir, redact.New("super-secret-value"))
	require.NoError(t, err)
	fixed := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return fixed }
	l.SetContext("sess-1", "/ws")

	l.Log(Entry{Kind: KindToolCall, Tool: "run_shell_command", Args: map[string]any{"command": "curl -H 'Authorization: super-secret-value'"}})
	l.Log(Entry{Kind: KindDenial, Tool: "delete_file", Detail: "token=abcdefghij", Decision: "deny"})
	l.Close()

	path := filepath.Join(dir, "audit-2026-09-23.jsonl")
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o600), info.Mode().Perm(), "audit file mode %v", info.Mode().Perm())
	d, _ := os.Stat(dir)
	assert.Equal(t, fs.FileMode(0o700), d.Mode().Perm(), "audit dir mode %v", d.Mode().Perm())
	entries := readEntries(t, path)
	require.Len(t, entries, 2, "expected 2 entries, got %d", len(entries))
	assert.Equal(t, "sess-1", entries[0].Session, "context not recorded: %+v", entries[0])
	assert.Equal(t, "/ws", entries[0].Workspace, "context not recorded: %+v", entries[0])
	raw, _ := os.ReadFile(path)
	assert.NotContains(t, string(raw), "super-secret-value", "secret written to audit log: %s", raw)
	assert.NotContains(t, string(raw), "abcdefghij", "secret written to audit log: %s", raw)
}

func TestAuditRotatesDaily(t *testing.T) {
	dir := t.TempDir()
	l, _ := Open(dir, nil)
	day := time.Date(2026, 1, 1, 23, 59, 0, 0, time.UTC)
	l.now = func() time.Time { return day }
	l.Log(Entry{Kind: KindPrompt})
	day = day.Add(2 * time.Minute)
	l.Log(Entry{Kind: KindPrompt})
	l.Close()
	for _, name := range []string{"audit-2026-01-01.jsonl", "audit-2026-01-02.jsonl"} {
		_, err := os.Stat(filepath.Join(dir, name))
		assert.NoError(t, err, "missing %s", name)
	}
}

func TestNilLoggerIsNoOp(t *testing.T) {
	var l *Logger
	l.SetContext("a", "b")
	l.Log(Entry{Kind: KindPrompt})
	assert.NoError(t, l.Close())
	// Negative: an unwritable location fails at Open.
	file := filepath.Join(t.TempDir(), "file")
	os.WriteFile(file, nil, 0o600)
	_, err := Open(filepath.Join(file, "sub"), nil)
	assert.Error(t, err, "expected error for unwritable dir")
}
