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
	"log/slog"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/engine/memory"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/textutil"
)

// Session summaries (spec_memory_012 MEM-35..37): where each session left
// off, written by the small model when the person moves to another session
// (and when one is asked for and missing or behind), listed in a new
// session's first prompt and read with recall_session.
const (
	// minSummaryMessages is how long a session must be to be worth a
	// summary: two exchanges.
	minSummaryMessages = 4
	// maxSummaryInput bounds the transcript a summary is written from: the
	// latest of it, after the summary so far.
	maxSummaryInput = 48 << 10
	// recentSessions is how many earlier sessions a first prompt lists.
	recentSessions = 5
	// summaryTimeout bounds writing one summary.
	summaryTimeout = 2 * time.Minute
)

// summaryInstruction asks for a session's summary.
const summaryInstruction = `You write the handoff note for a conversation between a user and their AI agent, so that a later session can pick up where this one left off. Write Markdown with exactly these sections, each short and specific (paths, commands, branches, names), leaving out a section with nothing in it:

Where it stands: one or two sentences, first and without a heading: the goal and its state now.
## Done
What was finished.
## Decisions
What the user decided, asked for or ruled out, and why; constraints to keep.
## Open
Unfinished work, unanswered questions, errors not fixed.
## Next
The agreed next step.

When given the summary so far and the conversation since, write the whole note again, up to date. Never repeat secrets, keys, passwords or credentials. No preamble.`

// errNoEarlierSession means the workspace has no earlier session worth
// recalling.
var errNoEarlierSession = errors.New("no earlier session in this workspace to pick up from")

// summariesDir is where the workspace's session summaries are kept.
func (w *Workspace) summariesDir() string {
	return memory.SummariesDir(memory.NotesDir(w.Dir()))
}

// summarizable reports whether rec is a conversation worth a summary: long
// enough, and the person's own (not a snapshot, an eval or a background run).
func summarizable(rec *session.SessionRecord) bool {
	return rec != nil && rec.Name == "" && rec.Origin == "" && rec.MessageCount >= minSummaryMessages
}

// summarize writes session id's summary, or brings it up to date, and
// returns it; one that covers every message is returned as it is.
func (w *Workspace) summarize(ctx context.Context, id string) (memory.Summary, error) {
	w.summaryMu.Lock()
	defer w.summaryMu.Unlock()
	rec, err := w.storage.Get(id) // not Load: that would make it the open session
	if err != nil {
		return memory.Summary{}, err
	}
	dir := w.summariesDir()
	prev, err := memory.FindSummary(dir, id)
	have := err == nil
	if have && prev.Messages >= len(rec.Messages) {
		return prev, nil
	}
	from := 0
	if have {
		from = prev.Messages
	}
	var conv strings.Builder
	for _, m := range rec.Messages[from:] {
		text := strings.TrimSpace(m.Content)
		if m.Role == "tool" || text == "" {
			continue
		}
		who := "Agent"
		if m.Role == "user" {
			who = "User"
		}
		fmt.Fprintf(&conv, "%s: %s\n\n", who, text)
	}
	var in strings.Builder
	fmt.Fprintf(&in, "Session %q, %s.\n\n", rec.Title, rec.UpdatedAt.Format("2006-01-02"))
	if have {
		fmt.Fprintf(&in, "The summary so far:\n%s\n\nThe conversation since:\n", prev.Text)
	} else {
		in.WriteString("The conversation:\n")
	}
	c := conv.String()
	if len(c) > maxSummaryInput {
		c = "…\n" + textutil.Tail(c, maxSummaryInput)
	}
	in.WriteString(c)

	llm, err := w.suggestionsModel(ctx)
	if err != nil {
		return memory.Summary{}, err
	}
	text, served, usage, err := runtime.Ask(ctx, llm, summaryInstruction+i18n.ReplyInstruction(w.replyIn()), in.String())
	if err != nil {
		return memory.Summary{}, err
	}
	if usage != nil {
		slog.Info("session summary written", "workspace", w.Dir(), "session", id, "model", served, "input_tokens", usage.PromptTokenCount, "output_tokens", usage.CandidatesTokenCount)
	}
	return memory.SaveSummary(dir, memory.Summary{
		Session:  id,
		Title:    rec.Title,
		Messages: len(rec.Messages),
		Updated:  rec.UpdatedAt,
		Text:     SecretRedactor(w.settings()).String(text),
	})
}

// summarizeLater brings the summaries of ids up to date in the background,
// one at a time, each once however often it's asked for meanwhile. The work
// stops when the workspace closes.
func (w *Workspace) summarizeLater(ids ...string) {
	if len(ids) == 0 || w.bgCtx == nil || w.bgCtx.Err() != nil || !w.settings().Memory.SummariesOn() {
		return
	}
	w.hookCtxMu.Lock()
	if w.summaryQueued == nil {
		w.summaryQueued = map[string]bool{}
	}
	var todo []string
	for _, id := range ids {
		if !w.summaryQueued[id] {
			w.summaryQueued[id] = true
			todo = append(todo, id)
		}
	}
	w.hookCtxMu.Unlock()
	if len(todo) == 0 {
		return
	}
	w.bg.Add(1)
	go func() {
		defer w.bg.Done()
		for _, id := range todo {
			if w.bgCtx.Err() == nil {
				ctx, cancel := context.WithTimeout(w.bgCtx, summaryTimeout)
				if _, err := w.summarize(ctx, id); err != nil && w.bgCtx.Err() == nil {
					slog.Warn("writing a session summary", "workspace", w.Dir(), "session", id, "error", err)
				}
				cancel()
			}
			w.hookCtxMu.Lock()
			delete(w.summaryQueued, id)
			w.hookCtxMu.Unlock()
		}
	}()
}

// summaryLeft brings a session's summary up to date once the person has
// moved on from it.
func (w *Workspace) summaryLeft(id string) {
	if rec, err := w.storage.Get(id); err == nil && summarizable(rec) {
		w.summarizeLater(id)
	}
}

// earlierSessions are the workspace's latest sessions before current, as
// its first prompt lists them (MEM-37); those whose summaries are missing or
// behind are brought up to date in the background, for the next time.
func (w *Workspace) earlierSessions(current string) string {
	if !w.settings().Memory.SummariesOn() {
		return ""
	}
	recs, err := w.storage.ListWorkspace(w.storage.Workspace())
	if err != nil {
		return ""
	}
	dir := w.summariesDir()
	var recent []memory.RecentSession
	var stale []string
	for _, r := range recs {
		if r.ID == current || !summarizable(r) {
			continue
		}
		rs := memory.RecentSession{ID: r.ID, Title: r.Title, Updated: r.UpdatedAt}
		if s, err := memory.FindSummary(dir, r.ID); err == nil {
			rs.Summary = &s
			if s.Messages < r.MessageCount {
				stale = append(stale, r.ID)
			}
		} else {
			stale = append(stale, r.ID)
		}
		recent = append(recent, rs)
		if len(recent) == recentSessions {
			break
		}
	}
	w.summarizeLater(stale...)
	return memory.RenderRecent(recent)
}

// recallSession is recall_session's answer (MEM-36): session's summary, or
// the latest earlier one's when session is empty, written first when it's
// missing or behind.
func (w *Workspace) recallSession(ctx context.Context, current, id string) (tools.RecallSessionOutput, error) {
	if id == "" {
		recs, err := w.storage.ListWorkspace(w.storage.Workspace())
		if err != nil {
			return tools.RecallSessionOutput{}, err
		}
		for _, r := range recs {
			if r.ID != current && summarizable(r) {
				id = r.ID
				break
			}
		}
		if id == "" {
			return tools.RecallSessionOutput{}, errNoEarlierSession
		}
	}
	if id == current {
		return tools.RecallSessionOutput{}, errors.New("that's this session: its conversation is already in front of you")
	}
	// Only this workspace's: a session ID reaches any of them.
	if rec, err := w.storage.Get(id); err != nil || rec.Workspace != w.storage.Workspace() {
		return tools.RecallSessionOutput{}, fmt.Errorf("no session %q in this workspace", id)
	}
	ctx, cancel := context.WithTimeout(ctx, summaryTimeout)
	defer cancel()
	s, err := w.summarize(ctx, id)
	if err != nil {
		return tools.RecallSessionOutput{}, err
	}
	return tools.RecallSessionOutput{Session: s.Session, Title: s.Title, Updated: s.Updated.Local().Format("2006-01-02 15:04"), Summary: s.Text}, nil
}
