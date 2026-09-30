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

package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotesDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, b := NotesDir("/work/shop app"), NotesDir("/other/shop app")
	assert.Regexp(t, `^shop-app-[0-9a-f]{8}$`, filepath.Base(a), "named for the workspace")
	assert.NotEqual(t, a, b, "same name, other workspace")
	assert.Equal(t, a, NotesDir("/work/shop app"), "stable")
}

func TestSaveNote(t *testing.T) {
	tests := []struct {
		name, kind, text, wantErr string
	}{
		{name: "fact", kind: NoteFact, text: "  The API tests need Docker.  "},
		{name: "correction", kind: NoteCorrection, text: "Use bazel, not go test"},
		{name: "unknown kind", kind: "rumour", text: "x", wantErr: "a fact, a preference or a correction"},
		{name: "empty", kind: NoteFact, text: "   ", wantErr: "empty"},
		{name: "too long", kind: NoteFact, text: strings.Repeat("a", maxNoteBytes+1), wantErr: "at most"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			n, err := SaveNote(dir, tt.kind, tt.text)
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			info, err := os.Stat(n.Path)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "only the owner reads notes")
			got, err := Notes(dir)
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, strings.TrimSpace(tt.text), got[0].Text)
			assert.Equal(t, tt.kind, got[0].Kind)
			assert.WithinDuration(t, time.Now(), got[0].Time, time.Minute)
		})
	}
}

func TestFindForgetAndRenderNotes(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name+".md"), []byte(body), 0o600))
	}
	write("20260101-000000-old", "---\nkind: preference\nsaved: 2026-01-01T00:00:00Z\n---\nShort replies.\n")
	write("20260202-000000-new", "Edited by hand, no front matter.\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.txt"), []byte("not a note"), 0o600))

	notes, err := Notes(dir)
	require.NoError(t, err)
	require.Len(t, notes, 2)
	assert.Equal(t, "20260202-000000-new", notes[0].Name, "newest first")
	assert.Equal(t, NoteFact, notes[0].Kind, "a fact when it doesn't say")
	assert.Equal(t, NotePreference, notes[1].Kind)

	out := RenderNotes(notes)
	assert.Contains(t, out, "never grant permission")
	assert.Less(t, strings.Index(out, "Edited by hand"), strings.Index(out, "Short replies"))
	assert.Empty(t, RenderNotes(nil))

	for _, tt := range []struct{ name, find, want, wantErr string }{
		{name: "exact", find: "20260101-000000-old", want: "20260101-000000-old"},
		{name: "unique prefix", find: "202602", want: "20260202-000000-new"},
		{name: "ambiguous prefix", find: "2026", wantErr: "no such note"},
		{name: "missing", find: "nope", wantErr: "no such note"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			n, err := FindNote(dir, tt.find)
			if tt.wantErr != "" {
				assert.ErrorIs(t, err, ErrNoNote)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, n.Name)
		})
	}

	require.NoError(t, DeleteNote(dir, "202601"))
	notes, _ = Notes(dir)
	assert.Len(t, notes, 1)
	missing, err := Notes(filepath.Join(dir, "none"))
	assert.NoError(t, err)
	assert.Empty(t, missing)
}

func TestRenderNotesIsBounded(t *testing.T) {
	var notes []Note
	for range 20 {
		notes = append(notes, Note{Kind: NoteFact, Text: strings.Repeat("x", 1000)})
	}
	out := RenderNotes(notes)
	assert.Less(t, len(out), maxNotesBytes+1000)
	assert.Contains(t, out, "older notes left out")
}
