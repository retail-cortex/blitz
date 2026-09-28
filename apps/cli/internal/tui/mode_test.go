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
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModeCommand(t *testing.T) {
	app, _ := newCommandApp(t, "/mode\n/mode acceptEdits\n/mode yolo\n/set\n/exit\n")
	app.Workspace.SetPermissionMode("default") // newCommandApp may start in bypass
	var prompts bytes.Buffer
	app.Input = NewLineReader(strings.NewReader("/mode\n/mode acceptEdits\n/mode yolo\n/set\n/exit\n"), &prompts)
	out := captureStdout(t, func() { RunREPL(context.Background(), app) }) + prompts.String()
	for _, want := range []string{
		"Permission mode: default", "accept-edits", "make file changes in the workspace without asking",
		"Permission mode: accept-edits", "Usage: /mode default|accept-edits|plan|dont-ask|bypass",
		"[accept-edits]", // the prompt shows a mode other than default
	} {
		assert.Contains(t, out, want, "missing %q in:\n%s", want, out)
	}
	got := app.Workspace.Settings().PermissionMode
	assert.Equal(t, "accept-edits", got, "mode %q", got)
}

func TestEffortCommand(t *testing.T) {
	in := "/effort\n/effort high\n/effort extreme\n/set\n/effort auto\n/exit\n"
	app, _ := newCommandApp(t, in)
	out := captureStdout(t, func() { RunREPL(context.Background(), app) })
	for _, want := range []string{
		"Reasoning effort: auto (each model's own)", "Reasoning effort: high",
		"Usage: /effort minimal|low|medium|high|max|auto", "Effort:",
	} {
		assert.Contains(t, out, want, "missing %q in:\n%s", want, out)
	}
	got := app.Workspace.Settings().Effort
	assert.Equal(t, "", got, "effort after auto: %q", got)
}

func TestCycleModeSkipsAnUnavailableBypass(t *testing.T) {
	app, _ := newCommandApp(t, "")
	app.Workspace.SetPermissionMode("default")
	var seen []string
	for range 4 {
		cycleMode(app, true)
		seen = append(seen, app.Workspace.Settings().PermissionMode)
	}
	want := []string{"accept-edits", "plan"}
	if _, err := app.Workspace.SetPermissionMode("bypass"); err == nil {
		want = append(want, "bypass", "default") // this machine has the OS sandbox
	} else {
		want = append(want, "default", "accept-edits")
	}
	require.Equal(t, strings.Join(want, ","), strings.Join(seen, ","), "cycle %v, want %v", seen, want)
}
