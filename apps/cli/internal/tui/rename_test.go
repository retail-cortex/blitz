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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenameTitleAndResumeHint(t *testing.T) {
	app, _ := newCommandApp(t, "/session list\nfix the \x1b[31mlogin\x07 bug\n/session list\n/rename\n/rename Login work\n/exit\n")
	app.TerminalTitle = true
	out := captureStdout(t, func() { RunREPL(context.Background(), app) })

	for _, want := range []string{
		"(untitled)",                    // before the first prompt
		"\033]0;Blitz · (untitled)\007", // terminal title
		"fix the [31mlogin bug",         // named by the prompt, control characters dropped
		"\033]0;Blitz · fix the [31mlogin bug\007",
		"Usage: /rename <name>",
		"Session renamed to Login work.",
		"\033]0;\007", // restored at exit
		"Resume with: blitz --resume=" + local(app).Storage().Active().ID,
	} {
		t.Run(want, func(t *testing.T) {
			assert.Contains(t, out, want, "output lacks %q:\n%q", want, out)
		})
	}
	require.Equal(t, "Login work", local(app).Storage().Active().Title, "title %q", local(app).Storage().Active().Title)
}

func TestNoTerminalTitleOrHintWhenOff(t *testing.T) {
	app, _ := newCommandApp(t, "/exit\n")
	out := captureStdout(t, func() { RunREPL(context.Background(), app) })
	require.NotContains(t, out, "\033]0;", "output:\n%q", out)
	require.NotContains(t, out, "Resume with", "output:\n%q", out)
}
