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
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rewindPoints are two prompts; the first changed a file.
func rewindPoints(conversation bool) func() ([]api.RewindPoint, error) {
	return func() ([]api.RewindPoint, error) {
		return []api.RewindPoint{
			{Index: 1, Text: "first", Time: time.Now(), Files: []string{"a.go"}, Conversation: conversation},
			{Index: 2, Text: "second", Time: time.Now(), Conversation: conversation},
		}, nil
	}
}

// /rewind on a pipe picks a mode from what the prompt allows, and reports
// each outcome.
func TestRewindOutcomes(t *testing.T) {
	type got struct {
		mode  api.RewindMode
		force bool
	}
	cases := map[string]struct {
		line         string
		conversation bool
		res          api.RewindResult
		err          error
		want         []string
		wantCall     got
	}{
		"code only for an old prompt": {
			line: "/rewind 2 -f", res: api.RewindResult{Mode: api.RewindCode},
			want: []string{"No files to restore from there."}, wantCall: got{api.RewindCode, true},
		},
		"both when files changed": {
			line: "/rewind 2", conversation: true,
			res:  api.RewindResult{Mode: api.RewindBoth, Restored: []string{"a.go"}, Prompt: "first"},
			want: []string{"Restored: a.go", "Conversation rewound", "The prompt was: first"}, wantCall: got{api.RewindBoth, false},
		},
		"conversation when none did": {
			line: "/rewind 1 --force", conversation: true,
			res:  api.RewindResult{Mode: api.RewindConversation, Prompt: "second"},
			want: []string{"Conversation rewound"}, wantCall: got{api.RewindConversation, true},
		},
		"summarize": {
			line: "/rewind 1 summarize-from", conversation: true,
			res: api.RewindResult{Mode: api.RewindSummarizeFrom, Compacted: api.CompactResult{EventsCompacted: 3, SummaryChars: 40,
				After: api.Usage{Calls: 1, Input: 9}}},
			want: []string{"Summarizing earlier conversation", "Replaced 3 earlier events", "↳"}, wantCall: got{api.RewindSummarizeFrom, false},
		},
		"code restored": {
			line: "/rewind 2 code", conversation: true, res: api.RewindResult{Mode: api.RewindCode, Restored: []string{"a.go"}},
			want: []string{"Restored: a.go"}, wantCall: got{api.RewindCode, false},
		},
		"conflict": {
			line: "/rewind 2 code", err: api.ErrUndoConflict,
			want: []string{api.ErrUndoConflict.Error()}, wantCall: got{api.RewindCode, false},
		},
		"nothing to compact": {
			line: "/rewind 1 summarize-up-to", conversation: true, err: api.ErrNothingToCompact,
			want: []string{api.ErrNothingToCompact.Error()}, wantCall: got{api.RewindSummarizeUpTo, false},
		},
		"too old": {
			line: "/rewind 1 conversation", conversation: true, err: api.ErrCantRewindConversation,
			want: []string{"recorded by an older Blitz"}, wantCall: got{api.RewindConversation, false},
		},
		"fails": {
			line: "/rewind 1 both", conversation: true, err: errBoom,
			want: []string{"✗ boom"}, wantCall: got{api.RewindBoth, false},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			app, s := stubApp(t, "")
			s.rewindPoints = rewindPoints(c.conversation)
			var call got
			s.rewind = func(_ int, mode api.RewindMode, force bool) (api.RewindResult, error) {
				call = got{mode, force}
				return c.res, c.err
			}
			out := runCmd(t, app, c.line)
			for _, w := range c.want {
				assert.Contains(t, out, w)
			}
			assert.Equal(t, c.wantCall, call)
		})
	}
}

// /rewind says when there is nothing to go back to, or the points can't be
// read; the list shows the files each prompt changed.
func TestRewindPointsListed(t *testing.T) {
	app, s := stubApp(t, "")
	s.rewindPoints = func() ([]api.RewindPoint, error) { return nil, errBoom }
	assert.Contains(t, runCmd(t, app, "/rewind"), "✗ boom")
	s.rewindPoints = func() ([]api.RewindPoint, error) { return nil, nil }
	assert.Contains(t, runCmd(t, app, "/rewind"), "No prompts to rewind to yet.")
	s.rewindPoints = rewindPoints(true)
	assert.Contains(t, runCmd(t, app, "/rewind"), "· a.go")
}

// On a terminal, a conflict asks before overwriting, and leaving a picker
// changes nothing.
func TestRewindPickersOnATerminal(t *testing.T) {
	app, s := stubApp(t, "")
	f, keys := pickTerminal(t)
	app.Input = f.in
	s.rewindPoints = rewindPoints(true)
	var forced []bool
	s.rewind = func(_ int, mode api.RewindMode, force bool) (api.RewindResult, error) {
		forced = append(forced, force)
		if !force {
			return api.RewindResult{}, api.ErrUndoConflict
		}
		return api.RewindResult{Mode: mode, Restored: []string{"a.go"}}, nil
	}

	// Esc at the prompt picker.
	keys.in <- []byte("\x1b")
	runCmd(t, app, "/rewind")
	assert.Empty(t, forced, "rewound after Esc")

	// Esc at the mode picker.
	keys.in <- []byte("\x1b[B\r") // the first prompt, which changed a file
	keys.in <- []byte("\x1b")
	runCmd(t, app, "/rewind")
	assert.Empty(t, forced, "rewound after Esc")

	// A conflict, kept.
	keys.in <- []byte("\r")
	runCmd(t, app, "/rewind 2 code")
	assert.Equal(t, []bool{false}, forced)

	// A conflict, overwritten.
	forced = nil
	keys.in <- []byte("\x1b[A\r")
	out := runCmd(t, app, "/rewind 2 code")
	require.Equal(t, []bool{false, true}, forced)
	assert.Contains(t, out, "Restored: a.go")
	assert.Contains(t, f.output(), "Overwrite them")
}
