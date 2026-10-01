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

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/engine/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryCommand(t *testing.T) {
	isolate(t)
	ws := t.TempDir()
	n, err := memory.SaveNote(memory.NotesDir(ws), memory.NotePreference, "Answer in short sentences.\nSecond line.")
	require.NoError(t, err)

	out, err := runCLI(t, "memory", "list", "--dir", ws)
	require.NoError(t, err)
	assert.Contains(t, out, n.Name+"\tpreference\tAnswer in short sentences.")

	out, err = runCLI(t, "memory", "show", n.Name[:8], "--dir", ws)
	require.NoError(t, err)
	assert.Contains(t, out, "Second line.")

	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	_, err = runCLI(t, "memory", "edit", n.Name, "--dir", ws)
	assert.ErrorContains(t, err, "set $VISUAL or $EDITOR")
	t.Setenv("EDITOR", "printf 'Answer briefly.' >")
	_, err = runCLI(t, "memory", "edit", n.Name, "--dir", ws)
	require.NoError(t, err)
	got, err := memory.FindNote(memory.NotesDir(ws), n.Name)
	require.NoError(t, err)
	assert.Equal(t, "Answer briefly.", got.Text)

	_, err = runCLI(t, "memory", "forget", "nope", "--dir", ws)
	assert.ErrorContains(t, err, "no such note")
	out, err = runCLI(t, "memory", "forget", n.Name, "--dir", ws)
	require.NoError(t, err)
	assert.Contains(t, out, "Forgot")
	out, err = runCLI(t, "memory", "list", "--dir", ws)
	require.NoError(t, err)
	assert.Contains(t, out, "No notes")
}

// Long first lines are cut; without --dir the notes are the current
// directory's; a broken notes folder fails the commands.
func TestMemoryCommandEdges(t *testing.T) {
	isolate(t)
	ws := t.TempDir()
	t.Chdir(ws)
	long := strings.Repeat("word ", 30)
	_, err := memory.SaveNote(memory.NotesDir(ws), memory.NotePreference, long)
	require.NoError(t, err)
	out, err := runCLI(t, "memory", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "…")

	broken := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Dir(memory.NotesDir(broken)), 0o755))
	require.NoError(t, os.WriteFile(memory.NotesDir(broken), nil, 0o644)) // a file where the folder goes
	for _, args := range [][]string{{"memory", "list"}, {"memory", "show", "x"}, {"memory", "edit", "x"}, {"memory", "forget", "x"}} {
		_, err := runCLI(t, append(args, "--dir", broken)...)
		assert.Error(t, err, "%v", args)
	}
}
