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
	"context"
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
	l.SetWorkspace("/ws")

	l.LogContext(WithSession(context.Background(), "sess-1"), Entry{Kind: KindToolCall, Tool: "run_shell_command", Args: map[string]any{"command": "curl -H 'Authorization: super-secret-value'"}})
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
	assert.Empty(t, entries[1].Session, "an entry outside a turn names no session, not the latest one")
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
		t.Run(name, func(t *testing.T) {
			_, err := os.Stat(filepath.Join(dir, name))
			assert.NoError(t, err, "missing %s", name)
		})
	}
}

func TestNilLoggerIsNoOp(t *testing.T) {
	var l *Logger
	l.SetWorkspace("b")
	l.Log(Entry{Kind: KindPrompt})
	assert.NoError(t, l.Close())
	// Negative: an unwritable location fails at Open.
	file := filepath.Join(t.TempDir(), "file")
	os.WriteFile(file, nil, 0o600)
	_, err := Open(filepath.Join(file, "sub"), nil)
	assert.Error(t, err, "expected error for unwritable dir")
}

// TestLogSkipsBadEntries checks that an entry that can't be encoded or a
// directory that can't be written is skipped without failing the caller.
func TestLogSkipsBadEntries(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(dir, nil)
	require.NoError(t, err)
	day := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return day }

	l.Log(Entry{Kind: KindToolCall, Args: map[string]any{"ch": make(chan int)}})
	assert.NoFileExists(t, filepath.Join(dir, "audit-2026-01-01.jsonl"), "an unencodable entry opens no file")

	require.NoError(t, os.RemoveAll(dir))
	l.Log(Entry{Kind: KindPrompt})
	assert.Nil(t, l.f, "a failed rotation leaves no open file")
	assert.NoError(t, l.Close(), "closing without an open file")
}

// TestCloseClosesTheFile checks that Close releases the open day file.
func TestCloseClosesTheFile(t *testing.T) {
	l, err := Open(t.TempDir(), nil)
	require.NoError(t, err)
	l.Log(Entry{Kind: KindPrompt})
	require.NotNil(t, l.f)
	assert.NoError(t, l.Close())
	assert.Nil(t, l.f)
}

func TestRead(t *testing.T) {
	dir := t.TempDir()
	l, _ := Open(dir, nil)
	now := time.Now().UTC()
	at := now.Add(-50 * time.Hour)
	l.now = func() time.Time { return at }
	l.Log(Entry{Kind: KindPrompt, Detail: "too old", Workspace: "/w"})
	at = now.Add(-30 * time.Hour)
	l.Log(Entry{Kind: KindPrompt, Detail: "yesterday", Workspace: "/w"})
	l.Log(Entry{Kind: KindPrompt, Detail: "elsewhere", Workspace: "/other"})
	at = now.Add(-time.Minute)
	l.Log(Entry{Kind: KindPrompt, Detail: "any workspace"})
	l.Close()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "audit-"+now.Format(time.DateOnly)+".jsonl"), append(mustRead(t, filepath.Join(dir, "audit-"+now.Format(time.DateOnly)+".jsonl")), []byte("not json\n")...), 0o600))

	var got []string
	for _, e := range Read(dir, "/w", now.Add(-40*time.Hour)) {
		got = append(got, e.Detail)
	}
	assert.Equal(t, []string{"yesterday", "any workspace"}, got, "since then, this workspace's and no workspace's, in order; a bad line skipped")
	assert.Empty(t, Read("", "/w", now.Add(-time.Hour)), "no log")
	assert.Empty(t, Read(filepath.Join(dir, "missing"), "/w", now.Add(-time.Hour)))
}

func TestStamp(t *testing.T) {
	dir := t.TempDir()
	since := time.Now().Add(-time.Hour)
	empty := Stamp(dir, since)
	assert.Equal(t, empty, Stamp(dir, since), "the same log, the same stamp")
	l, _ := Open(dir, nil)
	l.Log(Entry{Kind: KindPrompt, Detail: "one"})
	first := Stamp(dir, since)
	assert.NotEqual(t, empty, first, "an entry changes it")
	l.Log(Entry{Kind: KindPrompt, Detail: "two"})
	require.NoError(t, l.Close())
	assert.NotEqual(t, first, Stamp(dir, since))
	assert.NotEqual(t, Stamp(dir, since), Stamp(dir, since.Add(-time.Minute)), "so does where it starts")
}

// agentCtx is an agent's context.
type agentCtx struct {
	context.Context
	name string
}

func (c agentCtx) AgentName() string { return c.name }

// strictCtx is a mock that panics when asked.
type strictCtx struct{ context.Context }

func (strictCtx) AgentName() string { panic("not implemented") }

// Denials and failed results name their agent; the rest don't need to.
func TestLogContextNamesTheAgent(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		e    Entry
		want string
	}{
		{"a denial", agentCtx{context.Background(), "qa"}, Entry{Kind: KindDenial, Tool: "run_shell_command"}, "qa"},
		{"a failed result", agentCtx{context.Background(), "qa"}, Entry{Kind: KindToolResult, Decision: "exit"}, "qa"},
		{"an ok result", agentCtx{context.Background(), "qa"}, Entry{Kind: KindToolResult, Decision: "ok"}, ""},
		{"a call", agentCtx{context.Background(), "qa"}, Entry{Kind: KindToolCall}, ""},
		{"not an agent's context", context.Background(), Entry{Kind: KindDenial}, ""},
		{"a strict mock", strictCtx{context.Background()}, Entry{Kind: KindDenial}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			l, err := Open(dir, nil)
			require.NoError(t, err)
			l.LogContext(tt.ctx, tt.e)
			require.NoError(t, l.Close())
			got := Read(dir, "", time.Now().Add(-time.Hour))
			require.Len(t, got, 1)
			assert.Equal(t, tt.want, got[0].Agent)
		})
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return b
}
