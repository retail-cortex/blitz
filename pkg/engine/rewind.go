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

package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/engine/audit"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
)

// Rewinding a session to one of its prompts: its files, its conversation,
// or both, or summarizing the conversation on either side of the prompt.

func (w *Workspace) turnStarted(session string) {
	w.turnsMu.Lock()
	defer w.turnsMu.Unlock()
	if w.inTurn == nil {
		w.inTurn = map[string]int{}
	}
	w.inTurn[session]++
}

func (w *Workspace) turnEnded(session string) {
	w.turnsMu.Lock()
	defer w.turnsMu.Unlock()
	if w.inTurn[session]--; w.inTurn[session] <= 0 {
		delete(w.inTurn, session)
	}
}

// Busy reports whether a turn is running in any of the workspace's
// sessions.
func (w *Workspace) Busy() bool {
	w.turnsMu.Lock()
	defer w.turnsMu.Unlock()
	return len(w.inTurn) > 0
}

func (w *Workspace) busy(session string) bool {
	w.turnsMu.Lock()
	defer w.turnsMu.Unlock()
	return w.inTurn[session] > 0
}

// RewindPoints lists the active session's prompts, oldest first.
func (w *Workspace) RewindPoints() ([]api.RewindPoint, error) {
	active := w.storage.Active()
	if active == nil {
		return nil, api.ErrNoActiveSession
	}
	var points []api.RewindPoint
	for i, m := range active.Messages {
		if m.IsPrompt() {
			points = append(points, api.RewindPoint{Index: i, Text: m.Content, Time: m.Timestamp, Conversation: m.Events != nil})
		}
	}
	// A prompt's files: those of the checkpoints between it and the next.
	for _, c := range w.tools.Checkpoints().List() {
		if c.Session != active.ID || c.Prompt < 0 {
			continue
		}
		for j := len(points) - 1; j >= 0; j-- {
			if points[j].Index <= c.Prompt {
				points[j].Files = appendNew(points[j].Files, c.Files...)
				break
			}
		}
	}
	return points, nil
}

func appendNew(list []string, add ...string) []string {
	for _, a := range add {
		found := false
		for _, x := range list {
			found = found || x == a
		}
		if !found {
			list = append(list, a)
		}
	}
	return list
}

// Rewind takes the active session back to the prompt at transcript index
// index. Files are restored first (refusing on conflicts unless force), so
// a refused rewind changes nothing; then the conversation is cut before the
// prompt, whose text is returned to send again.
func (w *Workspace) Rewind(ctx context.Context, index int, mode api.RewindMode, force bool) (api.RewindResult, error) {
	active := w.storage.Active()
	if active == nil {
		return api.RewindResult{}, api.ErrNoActiveSession
	}
	id := active.ID
	if w.busy(id) {
		return api.RewindResult{}, api.ErrSessionBusy
	}
	if index < 0 || index >= len(active.Messages) || !active.Messages[index].IsPrompt() {
		return api.RewindResult{}, fmt.Errorf("%w: %d", api.ErrNotRewindPoint, index)
	}
	prompt := active.Messages[index]
	res := api.RewindResult{Mode: mode, Prompt: DisplayPrompt(prompt.Content)}
	conversation := mode == api.RewindBoth || mode == api.RewindConversation
	switch mode {
	case api.RewindBoth, api.RewindConversation, api.RewindCode:
	case api.RewindSummarizeFrom, api.RewindSummarizeUpTo:
		if prompt.Events == nil {
			return api.RewindResult{}, api.ErrCantRewindConversation
		}
		before := w.engine.Usage(id)
		c, err := w.engine.CompactAt(ctx, id, "", *prompt.Events, mode == api.RewindSummarizeUpTo)
		if err != nil {
			return api.RewindResult{}, err
		}
		res.Compacted = api.CompactResult{EventsCompacted: c.EventsCompacted, SummaryChars: c.SummaryChars, Before: before, After: w.engine.Usage(id)}
		return res, nil
	default:
		return api.RewindResult{}, fmt.Errorf("%w %q", api.ErrUnknownRewindMode, mode)
	}
	if conversation && prompt.Events == nil {
		return api.RewindResult{}, api.ErrCantRewindConversation
	}

	if mode != api.RewindConversation {
		undone, err := w.tools.Checkpoints().Rewind(id, index, force)
		if err != nil && !errors.Is(err, tools.ErrNothingToUndo) {
			if len(undone.Restored) == 0 {
				return api.RewindResult{}, err
			}
			// Some files were put back: report the rest, and go on.
			w.warn(err.Error())
		}
		res.Restored = undone.Restored
		if len(undone.Restored) > 0 {
			w.tools.Hooks().Audit().Log(audit.Entry{Kind: audit.KindUndo, Session: id, Detail: "rewind: " + strings.Join(undone.Restored, ", ")})
		}
	}
	if conversation {
		if err := w.engine.TruncateSession(ctx, id, *prompt.Events); err != nil {
			return res, fmt.Errorf("rewind the conversation: %w", err)
		}
		if err := w.storage.Truncate(index); err != nil {
			return res, fmt.Errorf("rewind the transcript: %w", err)
		}
		// The files the dropped prompts changed (if kept) now come before
		// the next prompt.
		w.tools.Checkpoints().Detach(id, index)
	}
	return res, nil
}
