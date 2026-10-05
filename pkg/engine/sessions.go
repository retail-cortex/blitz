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
	"fmt"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/observability"
)

// Saved sessions: listing, starting, resuming, snapshots and renaming.

func sessionInfo(r *session.SessionRecord) api.SessionInfo {
	info := api.SessionInfo{
		ID: r.ID, Title: r.Title, Agent: r.Agent, Workspace: r.Workspace, Snapshot: r.Name, From: r.From,
		MovedFrom: r.MovedFrom, MovedAt: r.MovedAt, Origin: origin(r),
		MessageCount: r.MessageCount, Created: r.CreatedAt, Updated: r.UpdatedAt,
	}
	for _, m := range r.Messages {
		info.Messages = append(info.Messages, api.Message{Role: m.Role, Text: userText(m.Role, m.Content), Time: m.Timestamp, Kind: m.Kind})
	}
	return info
}

// origin is what started a session; a worker's run saved before sessions
// kept it is known by the title RunWorker gives it ("⏰ name date").
func origin(r *session.SessionRecord) string {
	if r.Origin == "" && strings.HasPrefix(r.Title, workerTitlePrefix) {
		return session.OriginWorker
	}
	return r.Origin
}

func sessionInfos(list []*session.SessionRecord) []api.SessionInfo {
	out := make([]api.SessionInfo, len(list))
	for i, r := range list {
		out[i] = sessionInfo(r)
	}
	return out
}

// Dir is the workspace directory.
func (w *Workspace) Dir() string { return w.tools.Workspace().Dir() }

// ListSessions returns this workspace's sessions, or with all every saved
// session, newest first. Messages are left out.
func (w *Workspace) ListSessions(all bool) ([]api.SessionInfo, error) {
	var list []*session.SessionRecord
	var err error
	if all {
		list, err = w.storage.List()
	} else {
		list, err = w.storage.ListWorkspace(w.storage.Workspace())
	}
	return sessionInfos(list), err
}

// ActiveSession returns the session prompts go to, with its messages.
func (w *Workspace) ActiveSession() (api.SessionInfo, bool) {
	r := w.storage.Active()
	if r == nil {
		return api.SessionInfo{}, false
	}
	return sessionInfo(r), true
}

// NewSession starts a new session for the active agent and makes it
// active. It is named after its first prompt.
func (w *Workspace) NewSession() (api.SessionInfo, error) {
	prev := w.storage.Active()
	r, err := w.storage.CreateSession(session.NewSessionID(), "", w.engine.ActiveAgent())
	if err != nil {
		return api.SessionInfo{}, err
	}
	w.switched(prev, r.ID, "new", "new")
	return sessionInfo(r), nil
}

// switched runs session_end for the session that was active (if another)
// and session_start for the one that now is.
func (w *Workspace) switched(prev *session.SessionRecord, id, endReason, startReason string) {
	if prev != nil && prev.ID != id {
		w.sessionEnded(prev.ID, endReason)
	}
	w.sessionStarted(id, startReason)
}

// sessionStarted runs session_start hooks and keeps what they give as
// context for the session's next prompt.
func (w *Workspace) sessionStarted(id, reason string) {
	observability.RecordSession(context.Background(), reason)
	out := w.tools.ScriptHooks().Run(context.Background(), "session_start", "", tools.HookEvent{SessionID: id, Reason: reason})
	if out.Context == "" {
		return
	}
	w.hookCtxMu.Lock()
	defer w.hookCtxMu.Unlock()
	if w.sessionContext == nil {
		w.sessionContext = map[string]string{}
	}
	w.sessionContext[id] = out.Context
}

// sessionEnded runs session_end hooks, in the background.
func (w *Workspace) sessionEnded(id, reason string) {
	w.tools.ScriptHooks().Async(context.Background(), "session_end", "", tools.HookEvent{SessionID: id, Reason: reason})
}

// takeSessionContext returns and forgets a session_start hook's context.
func (w *Workspace) takeSessionContext(id string) string {
	w.hookCtxMu.Lock()
	defer w.hookCtxMu.Unlock()
	c := w.sessionContext[id]
	delete(w.sessionContext, id)
	return c
}

// LoadSession makes the session ref names active: an ID, or the name of a
// snapshot, which starts a new session copied from it (branched). A
// session from another workspace can be loaded by ID.
func (w *Workspace) LoadSession(ref string) (s api.SessionInfo, branched bool, err error) {
	prev := w.storage.Active()
	r, branched, err := w.storage.Open(ref)
	if err != nil {
		return api.SessionInfo{}, false, err
	}
	w.switched(prev, r.ID, "load", "resume")
	return sessionInfo(r), branched, nil
}

// MoveSession carries session id here from the workspace it was in and
// makes it active (/cd, PAR-SES-40): --continue here finds it, rewind
// stops at the move, and the agent's next prompt says the workspace
// changed.
func (w *Workspace) MoveSession(id string) (api.SessionInfo, error) {
	prev := w.storage.Active()
	rec, err := w.storage.Move(id)
	if err != nil {
		return api.SessionInfo{}, err
	}
	w.audit.SetContext(rec.ID, w.Dir())
	if rec.MovedFrom != "" {
		w.hookCtxMu.Lock()
		if w.moveNotes == nil {
			w.moveNotes = map[string]string{}
		}
		w.moveNotes[rec.ID] = "The workspace is now " + w.Dir() + ", moved from " + rec.MovedFrom +
			". Paths and files mentioned earlier in this conversation were in the old workspace; this one's instructions replace its."
		w.hookCtxMu.Unlock()
	}
	w.switched(prev, rec.ID, "cd", "resume")
	return sessionInfo(rec), nil
}

// takeMoveNote removes and returns the note for session id's next prompt
// about the workspace it moved to, if any.
func (w *Workspace) takeMoveNote(id string) string {
	w.hookCtxMu.Lock()
	defer w.hookCtxMu.Unlock()
	n := w.moveNotes[id]
	delete(w.moveNotes, id)
	return n
}

// SaveSnapshot saves a copy of the active session under name. Snapshots are
// never continued in place: loading one starts a new session from it.
func (w *Workspace) SaveSnapshot(name string, force bool) (api.SessionInfo, error) {
	active := w.storage.Active()
	if active == nil {
		return api.SessionInfo{}, api.ErrNoActiveSession
	}
	r, err := w.storage.Snapshot(active.ID, name, force)
	if err != nil {
		return api.SessionInfo{}, err
	}
	return sessionInfo(r), nil
}

// RenameSession sets the active session's title. An empty title is refused.
func (w *Workspace) RenameSession(title string) (api.SessionInfo, error) {
	if w.storage.Active() == nil {
		return api.SessionInfo{}, api.ErrNoActiveSession
	}
	if err := w.storage.Rename(title); err != nil {
		return api.SessionInfo{}, err
	}
	s, _ := w.ActiveSession()
	return s, nil
}

// DeleteSession deletes saved session id, which may be another
// workspace's: its transcript and history. The active session, or one a
// background run uses, is refused (api.ErrSessionOpen).
func (w *Workspace) DeleteSession(ctx context.Context, id string) error {
	if err := w.storage.Delete(id); err != nil {
		return err
	}
	w.engine.ForgetSession(ctx, id)
	return nil
}

// OpenSession picks the session to use and points the audit log at it: the
// session resume names (an ID, or a snapshot to start a new session from),
// the most recent one in this workspace when cont is set or resume is
// "latest", or else a new session. It reports whether a session was resumed.
func (w *Workspace) OpenSession(resume string, cont bool) (api.SessionInfo, bool, error) {
	// A new session is named after its first prompt.
	prev := w.storage.Active()
	rec, resumed, err := selectSession(w.storage, resume, cont, "", w.engine.ActiveAgent())
	if err != nil {
		return api.SessionInfo{}, false, err
	}
	w.audit.SetContext(rec.ID, w.Dir())
	start := "startup"
	if resumed {
		start = "resume"
	}
	w.switched(prev, rec.ID, "new", start)
	return sessionInfo(rec), resumed, nil
}

func selectSession(st *session.Storage, resume string, cont bool, title, agent string) (*session.SessionRecord, bool, error) {
	if resume == "" && !cont {
		rec, err := st.CreateSession("", title, agent)
		return rec, false, err
	}
	if resume == "" || resume == "latest" {
		list, err := st.ListWorkspace(st.Workspace())
		if err != nil {
			return nil, false, err
		}
		// Snapshots are saved copies, not conversations to carry on.
		list = slices.DeleteFunc(list, func(r *session.SessionRecord) bool { return r.Name != "" })
		if len(list) == 0 {
			return nil, false, &api.ResumeError{Err: errNoSavedSessions(st.Workspace())}
		}
		resume = list[0].ID
	}
	rec, _, err := st.Open(resume) // an ID, or a snapshot name to start from
	if err != nil {
		return nil, false, &api.ResumeError{Err: err}
	}
	return rec, true, nil
}

func errNoSavedSessions(workspace string) error {
	return fmt.Errorf("no saved sessions for %s (use --resume <id> for a session from another directory)", workspace)
}

// userText is a message's text as shown: the user's prompts without what
// was added for the agent (DisplayPrompt).
func userText(role, text string) string {
	if role == "user" {
		return DisplayPrompt(text)
	}
	return text
}
