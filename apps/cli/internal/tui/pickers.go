package tui

import (
	"context"
	"fmt"
	"sort"

	"github.com/retail-cortex/blitz/pkg/i18n"
)

// pickAgent offers the agents in a picker (/agent on a terminal).
func pickAgent(ctx context.Context, app *App) (string, bool) {
	p, ok := terminalPicker(app)
	if !ok {
		return "", false
	}
	agents := app.Workspace.ListAgents()
	items := make([]PickItem, len(agents))
	current := 0
	for i, a := range agents {
		items[i] = PickItem{Label: a.DisplayName, Detail: a.Name + " · " + a.Description}
		if a.Active {
			current = i
		}
	}
	i, err := p.Pick(ctx, i18n.T("picker.agent"), items, current)
	if err != nil || agents[i].Active { // the active one: just show it
		return "", false
	}
	return agents[i].Name, true
}

// pickModel offers the models Blitz knows of: the current one, pinned
// ones and those with settings (/model on a terminal).
func pickModel(ctx context.Context, app *App) (string, bool) {
	p, ok := terminalPicker(app)
	if !ok {
		return "", false
	}
	current := app.Workspace.Model()
	currentRef := current.Name
	if current.Provider != "" {
		currentRef = current.Provider + "/" + current.Name
	}
	refs := []string{currentRef}
	detail := map[string]string{currentRef: i18n.T("picker.model_current")}
	seen := map[string]bool{currentRef: true, current.Name: true}
	var more []string
	for _, a := range app.Workspace.ListAgents() {
		if r := a.PinnedModel; r != "" && !seen[r] {
			seen[r] = true
			more = append(more, r)
			detail[r] = i18n.T("picker.model_pinned", "agent", a.Name)
		}
	}
	for r := range app.Workspace.AllModelSettings() {
		if !seen[r] {
			seen[r] = true
			more = append(more, r)
			detail[r] = i18n.T("picker.model_settings")
		}
	}
	sort.Strings(more)
	refs = append(refs, more...)
	items := make([]PickItem, len(refs))
	for i, r := range refs {
		items[i] = PickItem{Label: r, Detail: detail[r]}
	}
	i, err := p.Pick(ctx, i18n.T("picker.model"), items, 0)
	if err != nil || i == 0 { // the current one: just show it
		return "", false
	}
	return refs[i], true
}

// resumeWithPicker picks a session to resume when there's a terminal; ok
// is false when there isn't one (so /resume prints its usage).
func resumeWithPicker(ctx context.Context, app *App) (ref string, picked, ok bool) {
	if _, ok := terminalPicker(app); !ok {
		return "", false, false
	}
	ref, picked = pickSession(ctx, app)
	return ref, picked, true
}

// pickSession offers this workspace's sessions and all snapshots (/resume
// on a terminal), newest first.
func pickSession(ctx context.Context, app *App) (string, bool) {
	p, ok := terminalPicker(app)
	if !ok {
		return "", false
	}
	list, err := app.Workspace.ListSessions(false)
	if err != nil {
		return "", false
	}
	active, _ := app.Workspace.ActiveSession()
	var refs []string
	var items []PickItem
	for _, s := range list {
		if s.ID == active.ID || s.Snapshot != "" || s.MessageCount == 0 {
			continue
		}
		refs = append(refs, s.ID)
		items = append(items, PickItem{Label: sessionTitle(s), Detail: fmt.Sprintf("%s · %s · %s", s.ID, i18n.N("session.messages", s.MessageCount), s.Updated.Local().Format("2006-01-02 15:04"))})
		if len(items) == 50 {
			break
		}
	}
	if all, err := app.Workspace.ListSessions(true); err == nil {
		for _, s := range all {
			if s.Snapshot != "" {
				refs = append(refs, s.Snapshot)
				items = append(items, PickItem{Label: s.Snapshot, Detail: i18n.T("picker.snapshot") + " · " + sessionTitle(s)})
			}
		}
	}
	if len(items) == 0 {
		fmt.Printf("%s%s%s\n", Dim, i18n.T("picker.no_sessions"), Reset)
		return "", false
	}
	i, err := p.Pick(ctx, i18n.T("picker.session"), items, 0)
	if err != nil {
		return "", false
	}
	return refs[i], true
}
