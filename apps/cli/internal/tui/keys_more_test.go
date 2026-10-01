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

package tui

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An editor that fails, or saves nothing, says so and gives the line back.
func TestCtrlGEditorFailsOrSavesNothing(t *testing.T) {
	cases := map[string]struct {
		edit func(string) (string, error)
		says string
	}{
		"fails": {func(string) (string, error) { return "", errBoom }, "The editor failed: boom"},
		"empty": {func(string) (string, error) { return " \n", nil }, "Nothing saved; back to the prompt."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeTerminal(t)
			f.in.edit = c.edit
			f.keys(t, "draft", "\x07")
			go func() {
				for !strings.Contains(f.output(), c.says) {
					time.Sleep(5 * time.Millisecond)
				}
				f.w.Write([]byte("!\r"))
			}()
			line, err := within(t, func() (string, error) { return f.in.ReadInput(context.Background(), "> ") })
			require.NoError(t, err)
			assert.Equal(t, "draft!", line)
		})
	}
}

// editText fails without a temporary directory, and when the editor
// removes the file.
func TestEditTextFailures(t *testing.T) {
	script := filepath.Join(t.TempDir(), "ed")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\nrm \"$1\"\n"), 0o755))
	t.Setenv("VISUAL", script)
	_, err := editText("x", nil, io.Discard, io.Discard)
	assert.ErrorIs(t, err, os.ErrNotExist, "the edited file is gone")
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	_, err = editText("x", nil, io.Discard, io.Discard)
	assert.Error(t, err, "no temporary directory")
}

// A key typed while no read is in progress passes through, except Esc.
func TestFilterKeyBetweenReads(t *testing.T) {
	f := newFakeTerminal(t)
	_, ok := f.in.filterKey('a')
	assert.True(t, ok)
	_, ok = f.in.filterKey(keyEsc)
	assert.False(t, ok)
}

// A multiple-choice question on a line reader takes several answers.
func TestUserPrompterMultiSelect(t *testing.T) {
	ask := NewUserPrompter(discardInput("1, 3\n"))
	got, err := ask(api.WithMultiSelect(context.Background()), "Which?", []string{"a", "b", "c"})
	require.NoError(t, err)
	assert.Equal(t, "a\nc", got)
	_, err = ask(api.WithMultiSelect(context.Background()), "Which?", []string{"a"})
	assert.ErrorIs(t, err, io.EOF)
}

// A followed run that fails says so, and one followed on an Input with an
// interrupt handler hands it back.
func TestFollowRunFails(t *testing.T) {
	app, _ := stubApp(t, "")
	in := &ctrlCInput{}
	app.Input = in
	app.FollowID = "task-3"
	app.Follow = func(context.Context, func(api.Event)) (api.TurnResult, error) {
		return api.TurnResult{}, errBoom
	}
	out := captureStdout(t, func() { followRun(context.Background(), app, nil) })
	assert.Contains(t, out, "boom")
	assert.Nil(t, in.handler, "the interrupt handler was left set")
}
