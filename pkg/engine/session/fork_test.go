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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForkGetAndReadEvents(t *testing.T) {
	st, _, rec, dir := conversation(t)

	fork, err := st.Fork(rec.ID)
	require.NoError(t, err)
	assert.Equal(t, rec.ID, fork.From)
	assert.Equal(t, fork.ID, st.Active().ID, "the copy is active")

	got, err := st.Get(rec.ID)
	require.NoError(t, err)
	assert.Len(t, got.Messages, 2)

	events, err := ReadEvents(dir, fork.ID)
	require.NoError(t, err)
	require.Len(t, events, 2, "the copy has the event log")
	assert.Equal(t, "remember pineapple", events[0].Content.Parts[0].Text)

	none, err := ReadEvents(dir, NewSessionID())
	require.NoError(t, err)
	assert.Nil(t, none)
	_, err = ReadEvents(dir, "../bad")
	assert.Error(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, fork.ID+eventsSuffix), []byte("{not json\n"), 0o600))
	events, err = ReadEvents(dir, fork.ID)
	require.NoError(t, err, "a bad line is skipped, as replay skips it")
	assert.Empty(t, events)
	_, err = st.Get("missing-session")
	assert.Error(t, err)
}

// A snapshot and a fork carry the sub-agent log, and a snapshot replaced
// takes its sub-agent log with it (N26 engine-sub-2#14).
func TestCopiesKeepTheSubagentLog(t *testing.T) {
	tests := map[string]func(s *Storage, id string) (*SessionRecord, error){
		"snapshot": func(s *Storage, id string) (*SessionRecord, error) { return s.Snapshot(id, "keep", false) },
		"fork":     func(s *Storage, id string) (*SessionRecord, error) { return s.Fork(id) },
	}
	for name, copySession := range tests {
		t.Run(name, func(t *testing.T) {
			s, err := NewStorage(t.TempDir())
			require.NoError(t, err)
			t.Cleanup(func() { s.Release() })
			rec, err := s.CreateSession("", "", "blitz")
			require.NoError(t, err)
			require.NoError(t, s.AddMessage("user", "hi"))
			require.NoError(t, s.AppendSubagentEvent(rec.ID, map[string]string{"author": "qa"}))
			cp, err := copySession(s, rec.ID)
			require.NoError(t, err)
			data, err := os.ReadFile(s.path(cp.ID, subagentsSuffix))
			require.NoError(t, err)
			assert.Contains(t, string(data), `"qa"`)
		})
	}
	t.Run("replaced snapshot", func(t *testing.T) {
		s, err := NewStorage(t.TempDir())
		require.NoError(t, err)
		t.Cleanup(func() { s.Release() })
		rec, err := s.CreateSession("", "", "blitz")
		require.NoError(t, err)
		require.NoError(t, s.AddMessage("user", "hi"))
		require.NoError(t, s.AppendSubagentEvent(rec.ID, map[string]string{"author": "qa"}))
		first, err := s.Snapshot(rec.ID, "keep", false)
		require.NoError(t, err)
		_, err = s.Snapshot(rec.ID, "keep", true)
		require.NoError(t, err)
		_, err = os.Stat(s.path(first.ID, subagentsSuffix))
		assert.ErrorIs(t, err, os.ErrNotExist)
	})
}
