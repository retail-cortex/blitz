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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/retail-cortex/blitz/pkg/textutil"
	adksession "google.golang.org/adk/v2/session"
)

// ForkSession starts a new session copied from the active one up to and
// including its prompt turn (1 the first; 0 or more than there are: all
// of it), in the transcript and the event log, and makes it active; the
// source is unchanged (spec_backlog_026 BL-SES-01, spec_parity_027
// PAR-SES-10).
func (w *Workspace) ForkSession(ctx context.Context, turn int) (api.SessionInfo, error) {
	active := w.storage.Active()
	if active == nil {
		return api.SessionInfo{}, api.ErrNoActiveSession
	}
	if w.busy(active.ID) {
		return api.SessionInfo{}, api.ErrSessionBusy
	}
	var prompts []int
	for i, m := range active.Messages {
		if m.IsPrompt() {
			prompts = append(prompts, i)
		}
	}
	if turn < 0 {
		return api.SessionInfo{}, fmt.Errorf("%w: turn %d", api.ErrNotRewindPoint, turn)
	}
	keepMessages, keepEvents := -1, -1
	if turn > 0 && turn < len(prompts) { // cut before the next prompt
		next := active.Messages[prompts[turn]]
		if next.Events == nil {
			return api.SessionInfo{}, api.ErrCantRewindConversation
		}
		keepMessages, keepEvents = prompts[turn], *next.Events
	}
	rec, err := w.storage.Fork(active.ID)
	if err != nil {
		return api.SessionInfo{}, err
	}
	if keepEvents >= 0 {
		if err := w.engine.TruncateSession(ctx, rec.ID, keepEvents); err != nil {
			w.unfork(ctx, active.ID, rec.ID)
			return api.SessionInfo{}, fmt.Errorf("fork the conversation: %w", err)
		}
		if err := w.storage.Truncate(keepMessages); err != nil {
			w.unfork(ctx, active.ID, rec.ID)
			return api.SessionInfo{}, fmt.Errorf("fork the transcript: %w", err)
		}
	}
	w.audit.SetContext(rec.ID, w.Dir())
	w.switched(active, rec.ID, "fork", "fork")
	info, _ := w.ActiveSession()
	return info, nil
}

// unfork deletes fork, which couldn't be cut, and makes its source src
// active again. What it can't undo is a warning.
func (w *Workspace) unfork(ctx context.Context, src, fork string) {
	if _, err := w.storage.Load(src); err != nil {
		w.warn("returning to the forked session: " + err.Error())
		return
	}
	if err := w.storage.Delete(fork); err != nil {
		w.warn("deleting the unfinished fork: " + err.Error())
		return
	}
	w.engine.ForgetSession(ctx, fork)
}

// ExportSession is session id (the active one when "") as Markdown.
func (w *Workspace) ExportSession(id string) (string, error) {
	if id == "" {
		active := w.storage.Active()
		if active == nil {
			return "", api.ErrNoActiveSession
		}
		id = active.ID
	}
	return ExportSession(w.cfg, id)
}

// ExportSession is a stored session as Markdown (spec_parity_027
// PAR-SES-11): its prompts, answers and tool calls (arguments and results
// in brief) with their times, from the event log (else the transcript),
// with configured secrets masked. It reads the files only, so the
// session's workspace needn't be open.
func ExportSession(cfg *config.Config, id string) (string, error) {
	store, err := session.NewStorage(cfg.Session.StorageDir)
	if err != nil {
		return "", err
	}
	rec, err := store.Get(id)
	if err != nil {
		return "", err
	}
	events, err := session.ReadEvents(config.ExpandHome(cfg.Session.StorageDir), id)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	title := rec.Title
	if title == "" {
		title = "Session " + rec.ID
	}
	fmt.Fprintf(&b, "# %s\n\n", title)
	fmt.Fprintf(&b, "Session `%s` in `%s`, started %s", rec.ID, rec.Workspace, rec.CreatedAt.Local().Format("2006-01-02 15:04"))
	if rec.From != "" {
		fmt.Fprintf(&b, ", copied from `%s`", rec.From)
	}
	b.WriteString(".\n")
	if len(events) > 0 {
		writeEvents(&b, events)
	} else {
		writeMessages(&b, rec.Messages)
	}
	return SecretRedactor(cfg).String(b.String()), nil
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return " · " + t.Local().Format("2006-01-02 15:04:05")
}

func writeEvents(b *strings.Builder, events []*adksession.Event) {
	who := ""
	for _, ev := range events {
		if ev.Actions.Compaction != nil {
			b.WriteString("\n*The conversation before this point was summarised.*\n")
			continue
		}
		if ev.Content == nil {
			continue
		}
		var text []string
		for _, p := range ev.Content.Parts {
			switch {
			case p.FunctionCall != nil:
				args, _ := json.Marshal(p.FunctionCall.Args)
				fmt.Fprintf(b, "\n- `%s` %s", p.FunctionCall.Name, textutil.Ellipsize(string(args), 200))
			case p.FunctionResponse != nil:
				fmt.Fprintf(b, "\n  - → %s", resultSummary(p.FunctionResponse.Response))
			case p.Text != "" && !p.Thought:
				text = append(text, p.Text)
			}
		}
		if len(text) == 0 {
			continue
		}
		heading := "You"
		if ev.Author != "user" {
			heading = ev.Author
		}
		if heading != who || ev.Author == "user" {
			fmt.Fprintf(b, "\n\n## %s%s\n", heading, stamp(ev.Timestamp))
			who = heading
		}
		fmt.Fprintf(b, "\n%s\n", strings.TrimSpace(strings.Join(text, "")))
	}
}

// resultSummary is a tool result in brief: its error, else its content.
func resultSummary(r map[string]any) string {
	if e, ok := r["error"].(string); ok && e != "" {
		return "error: " + textutil.Ellipsize(e, 200)
	}
	data, err := json.Marshal(r)
	if err != nil {
		return "(unreadable)"
	}
	return textutil.Ellipsize(string(data), 200)
}

func writeMessages(b *strings.Builder, msgs []session.Message) {
	for _, m := range msgs {
		heading := "Blitz"
		switch {
		case m.Role == "user" && m.IsPrompt():
			heading = "You"
		case m.Role == "user":
			heading = "You (" + cmpKind(m.Kind) + ")"
		}
		fmt.Fprintf(b, "\n## %s%s\n\n%s\n", heading, stamp(m.Timestamp), strings.TrimSpace(m.Content))
	}
}

func cmpKind(k string) string {
	if k == "" {
		return "note"
	}
	return k
}
