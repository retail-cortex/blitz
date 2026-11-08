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
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
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

// pickModel offers every chat model of every provider set up, grouped by
// provider (spec_061_models MOD-20), the current one first; and the pinned
// models and those with settings that the providers don't list (/model on
// a terminal). refresh asks the providers again.
func pickModel(ctx context.Context, app *App, refresh bool) (string, bool) {
	p, ok := terminalPicker(app)
	if !ok {
		return "", false
	}
	current := currentModelRef(app)
	refs := []string{current}
	detail := map[string]string{current: i18n.T("picker.model_current")}
	seen := map[string]bool{current: true}
	if list, err := app.Workspace.ListModels(ctx, refresh); err == nil {
		for _, pm := range list.Providers {
			for _, m := range pm.Models {
				r := m.Ref(pm.Provider)
				if seen[r] {
					detail[r] = i18n.T("picker.model_current") + " · " + modelDetail(pm.Provider, m)
					continue
				}
				seen[r] = true
				refs = append(refs, r)
				detail[r] = modelDetail(pm.Provider, m)
			}
		}
	} else {
		fmt.Printf("%s%s%s\n", Dim, i18n.T("model.list_failed", "error", safe(err.Error())), Reset)
	}
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

// currentModelRef is the model the next turn runs on, as provider/name:
// the conversation's own, else the workspace's.
func currentModelRef(app *App) string {
	if cur, ok := app.Workspace.ActiveSession(); ok && cur.Model != "" {
		return cur.Model
	}
	m := app.Workspace.Model()
	if m.Provider != "" && !strings.HasPrefix(m.Name, m.Provider+"/") {
		return m.Provider + "/" + m.Name
	}
	return m.Name
}

// modelDetail is a listed model's line: its provider, price per million
// tokens in and out, context window, and "no tools" when it can't call
// them (spec_061_models MOD-12).
func modelDetail(provider string, m api.ListedModel) string {
	parts := []string{provider}
	if m.DisplayName != "" && m.DisplayName != m.ID {
		parts = append(parts, m.DisplayName)
	}
	if m.HasPrice {
		parts = append(parts, i18n.T("picker.model_price", "in", priceText(m.InputPerMTok), "out", priceText(m.OutputPerMTok)))
	}
	if m.Context > 0 {
		parts = append(parts, i18n.T("picker.model_context", "tokens", contextText(m.Context)))
	}
	if m.Context > 0 && !m.Tools {
		parts = append(parts, i18n.T("picker.model_no_tools"))
	}
	return strings.Join(parts, " · ")
}

// contextText is a context window as people say it: 128K, 1M, 2.5M.
func contextText(n int) string {
	if n >= 1_000_000 {
		return strconv.FormatFloat(float64(n)/1_000_000, 'f', -1, 64) + "M"
	}
	return strconv.Itoa((n+500)/1000) + "K"
}

// priceText is dollars per million tokens, as short as it reads.
func priceText(usd float64) string {
	return "$" + strconv.FormatFloat(usd, 'f', -1, 64)
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
		fmt.Printf("%s✗ %s%s\n", Red, i18n.T("session.list_failed", "error", safe(err.Error())), Reset)
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

// printModels lists every chat model of every provider set up, by
// provider, where there's no picker (spec_061_models MOD-20).
func printModels(ctx context.Context, app *App, refresh bool) {
	list, err := app.Workspace.ListModels(ctx, refresh)
	if err != nil {
		fmt.Printf("%s%s%s\n", Dim, i18n.T("model.list_failed", "error", safe(err.Error())), Reset)
		return
	}
	for _, pm := range list.Providers {
		fmt.Printf("\n%s%s%s\n", Bold, safe(pm.Provider), Reset)
		if pm.Error != "" {
			fmt.Printf("  %s%s%s\n", Yellow, safe(pm.Error), Reset)
		}
		for _, m := range pm.Models {
			fmt.Printf("  %s  %s%s%s\n", safe(m.Ref(pm.Provider)), Dim, safe(modelDetail(pm.Provider, m)), Reset)
		}
	}
	if len(list.NotSetUp) > 0 {
		fmt.Printf("\n%s%s%s\n", Dim, i18n.T("model.not_set_up", "providers", strings.Join(list.NotSetUp, ", ")), Reset)
	}
}
