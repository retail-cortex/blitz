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
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/audit"
	"github.com/retail-cortex/blitz/pkg/engine/harness"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/retail-cortex/blitz/pkg/engine/workers"
	"github.com/retail-cortex/blitz/pkg/textutil"
)

// What the harness learns from the workspace's runs
// (spec_harness_learning_048).

// HarnessEvidence is what went wrong in the workspace's sessions since
// then (HE-02): from the audit log, its chats', jobs' and detached runs'
// (not evals' cases or snapshots), with the health judge's latest fixes
// for the items involved. No model is called.
func (w *Workspace) HarnessEvidence(since time.Time) harness.Evidence {
	in := harness.Inputs{Since: since, Sessions: map[string]harness.Session{}, Fixes: map[string][]string{}}
	if !w.cfg.Audit.Enabled {
		in.NoAudit = true
		return harness.Build(in)
	}
	if list, err := w.storage.ListWorkspace(w.Dir()); err == nil {
		for _, s := range list {
			if s.Name != "" || s.Origin == session.OriginEval || s.UpdatedAt.Before(since) {
				continue
			}
			in.Sessions[s.ID] = harness.Session{Title: s.Title, Origin: origin(s), Agent: s.Agent}
		}
	}
	for key, v := range w.healthStore().Latest() {
		if len(v.Fixes) > 0 {
			in.Fixes[key] = v.Fixes
		}
	}
	in.Audit = audit.Read(config.ExpandHome(w.cfg.Audit.Dir), w.Dir(), since)
	return harness.Build(in)
}

const (
	// reflectWindow is how far back the evidence goes before a first
	// reflection; maxReflectWindow, however long ago the last one was.
	reflectWindow    = 14 * 24 * time.Hour
	maxReflectWindow = 90 * 24 * time.Hour
	// reflectMaxTurns and reflectTimeout bound a scheduled reflection.
	reflectMaxTurns = 30
	reflectTimeout  = 10 * time.Minute
)

// harnessState is what the service keeps of a workspace's reflections,
// outside the repository.
type harnessState struct {
	// LastReflect is when the last /reflect began (zero: never).
	LastReflect time.Time `json:"last_reflect,omitzero"`
	// Proposals are the changes the last scheduled reflection proposed,
	// waiting for /reflect, and Proposed when (HE-06).
	Proposals []string  `json:"proposals,omitempty"`
	Proposed  time.Time `json:"proposed,omitzero"`
	// LastScheduled is when the schedule last reflected, or chose not to
	// (Skipped says why); LastCost is what it cost.
	LastScheduled time.Time `json:"last_scheduled,omitzero"`
	Skipped       string    `json:"skipped,omitempty"`
	LastCost      float64   `json:"last_cost,omitempty"`
}

// harnessStateMu serializes changes to the harness states.
var harnessStateMu sync.Mutex

// harnessStatePath is the workspace's state, beside its health checks.
func (w *Workspace) harnessStatePath() string {
	return filepath.Join(w.StorageDir(), "harness", w.storeID()+".json")
}

func (w *Workspace) harnessState() harnessState {
	var st harnessState
	if data, err := os.ReadFile(w.harnessStatePath()); err == nil {
		_ = json.Unmarshal(data, &st) // a damaged state is no reflection yet
	}
	return st
}

func (w *Workspace) saveHarnessState(st harnessState) {
	path := w.harnessStatePath()
	data, _ := json.Marshal(st)
	err := os.MkdirAll(filepath.Dir(path), 0o700)
	if err == nil {
		err = writeFileAtomic(filepath.Dir(path), path, data)
	}
	if err != nil {
		slog.Warn("saving the harness state", "error", err)
	}
}

// updateHarnessState changes the workspace's state with change.
func (w *Workspace) updateHarnessState(change func(*harnessState)) {
	harnessStateMu.Lock()
	defer harnessStateMu.Unlock()
	st := w.harnessState()
	change(&st)
	w.saveHarnessState(st)
}

// reflected records a /reflect that began at start: the next one's
// evidence starts there (HE-04), and the proposals it reviewed are gone.
func (w *Workspace) reflected(start time.Time) {
	w.updateHarnessState(func(st *harnessState) {
		st.LastReflect = start
		st.Proposals, st.Proposed = nil, time.Time{}
	})
}

// reflectSince is where the evidence starts by default: the last
// reflection, else reflectWindow ago; never before maxReflectWindow ago.
// A window's start is on the hour, so the evidence it holds is cached
// for an hour at a time.
func (w *Workspace) reflectSince() time.Time {
	now := time.Now()
	last := w.harnessState().LastReflect
	switch {
	case last.IsZero():
		return now.Add(-reflectWindow).Truncate(time.Hour)
	case last.Before(now.Add(-maxReflectWindow)):
		return now.Add(-maxReflectWindow).Truncate(time.Hour)
	}
	return last
}

// readEvidence is the harness_evidence tool's reader: the evidence since
// then (zero: reflectSince), about skills, agents or rules ("" all), as
// text, after a scheduled reflection's proposals waiting for review.
func (w *Workspace) readEvidence(_ context.Context, since time.Time, about string) (string, time.Time) {
	if since.IsZero() {
		since = w.reflectSince()
	}
	text := w.HarnessEvidence(since).About(about).Text()
	if st := w.harnessState(); about == "" && len(st.Proposals) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "A scheduled reflection on %s proposed these changes from this evidence. Offer them in step 5 with any new ones, after checking each still applies (drop one the harness has since):\n", st.Proposed.Format(time.DateOnly))
		for _, c := range st.Proposals {
			fmt.Fprintf(&b, "- %s\n", c)
		}
		text = b.String() + "\n" + text
	}
	if about == "" {
		text += w.globalExperienceText()
	}
	return text, since
}

// maxOtherWorkspaces and maxOtherEntries bound what the reflector reads of
// the other workspaces' experience; maxGlobalShown, of the global file.
const (
	maxOtherWorkspaces = 20
	maxOtherEntries    = 30
	maxGlobalShown     = 4096
)

// howToWorkHeading is the experience file's section of what's true in any
// project (HE-20).
const howToWorkHeading = "## How to work"

// globalExperienceText is what the reflector reads to offer a "How to
// work" entry for the user's global file (HE-21): the entries of the
// user's other workspaces (those with sessions here), and what the global
// file holds. "" when no other workspace has any.
func (w *Workspace) globalExperienceText() string {
	list, err := w.storage.List()
	if err != nil {
		return ""
	}
	seen := map[string]bool{w.Dir(): true}
	var b strings.Builder
	n := 0
	for _, r := range list {
		dir := r.Workspace
		if dir == "" || seen[dir] || n >= maxOtherWorkspaces {
			continue
		}
		seen[dir] = true
		entries := sectionEntries(filepath.Join(dir, ".agents", "EXPERIENCE.md"), howToWorkHeading)
		if len(entries) == 0 {
			continue
		}
		n++
		for _, e := range entries {
			fmt.Fprintf(&b, "- `%s`: %s\n", dir, e)
		}
	}
	if n == 0 {
		return ""
	}
	global := config.ExpandHome(w.cfg.Memory.Global)
	out := "\n\"How to work\" entries in your other workspaces' .agents/EXPERIENCE.md (HE-21):\n" + b.String()
	if data, err := os.ReadFile(global); err == nil && len(strings.TrimSpace(string(data))) > 0 {
		out += fmt.Sprintf("\nThe global file (%s) holds:\n%s\n", global, textutil.Ellipsize(string(data), maxGlobalShown))
	} else {
		out += fmt.Sprintf("\nThe global file (%s) is empty.\n", global)
	}
	return out
}

// sectionEntries are the bullets under heading in the Markdown file at
// path, without their "- ", at most maxOtherEntries.
func sectionEntries(path, heading string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []string
	in := false
	for _, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "## "):
			in = strings.EqualFold(t, heading)
		case in && strings.HasPrefix(t, "- ") && len(out) < maxOtherEntries:
			out = append(out, strings.TrimSpace(t[2:]))
		}
	}
	return out
}

// saveGlobalExperience is the save_global_experience tool's saver: it adds
// entry under "## How to work" in the user's global file (memory.global),
// unless it's there already, and reloads the instructions.
func (w *Workspace) saveGlobalExperience(ctx context.Context, entry string) (string, error) {
	path := config.ExpandHome(w.cfg.Memory.Global)
	if path == "" {
		return "", errors.New("there's no global instructions file (memory.global)")
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	text := string(data)
	if slices.Contains(sectionEntries(path, howToWorkHeading), entry) {
		return path, nil
	}
	text = addToSection(text, howToWorkHeading, "- "+entry)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := writeFileAtomic(filepath.Dir(path), path, []byte(text)); err != nil {
		return "", err
	}
	_, err = w.ReloadMemory(context.WithoutCancel(ctx))
	return path, err
}

// addToSection adds line at the end of heading's section of text, adding
// the section at the end when there's none.
func addToSection(text, heading, line string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	start := slices.IndexFunc(lines, func(l string) bool { return strings.EqualFold(strings.TrimSpace(l), heading) })
	if start < 0 {
		if strings.TrimSpace(text) == "" {
			return heading + "\n\n" + line + "\n"
		}
		return strings.TrimRight(text, "\n") + "\n\n" + heading + "\n\n" + line + "\n"
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
			end = i
			break
		}
	}
	at := end // after the section's last non-blank line
	for at > start+1 && strings.TrimSpace(lines[at-1]) == "" {
		at--
	}
	if at == start+1 {
		lines = slices.Insert(lines, at, "", line)
	} else {
		lines = slices.Insert(lines, at, line)
	}
	return strings.Join(lines, "\n") + "\n"
}

// saveProposals is the harness_propose tool's saver: they replace the
// ones kept before.
func (w *Workspace) saveProposals(_ context.Context, changes []string) error {
	w.updateHarnessState(func(st *harnessState) {
		st.Proposals, st.Proposed = changes, time.Now()
	})
	return nil
}

// hasProjectInstructions reports whether the workspace has
// .agents/AGENTS.md, which the experience belongs to (HE-01).
func (w *Workspace) hasProjectInstructions() bool {
	info, err := os.Stat(filepath.Join(w.Dir(), ".agents", "AGENTS.md"))
	return err == nil && info.Mode().IsRegular()
}

// Reflection is what the harness has to learn, for the welcome screens
// (HE-05): the patterns since the last reflection, counted without a model
// (again only when the audit log changed), and the proposals waiting.
// It's empty without .agents/AGENTS.md or the audit log; the error is
// always nil (it's the remote's).
func (w *Workspace) Reflection() (api.Reflection, error) {
	if !w.cfg.Audit.Enabled || !w.hasProjectInstructions() {
		return api.Reflection{}, nil
	}
	r := api.Reflection{Proposals: len(w.harnessState().Proposals)}
	r.Patterns = w.cachedEvidence(w.reflectSince()).Patterns()
	return r, nil
}

// maxCachedEvidence bounds the evidence cachedEvidence keeps.
const maxCachedEvidence = 8

// cachedEvidence is HarnessEvidence(since), built again only when the
// audit log changed since it was last built for that time.
func (w *Workspace) cachedEvidence(since time.Time) harness.Evidence {
	stamp := audit.Stamp(config.ExpandHome(w.cfg.Audit.Dir), since)
	w.evidenceMu.Lock()
	ev, ok := w.evidenceCache[stamp]
	w.evidenceMu.Unlock()
	if ok {
		return ev
	}
	ev = w.HarnessEvidence(since)
	w.evidenceMu.Lock()
	if w.evidenceCache == nil || len(w.evidenceCache) >= maxCachedEvidence {
		w.evidenceCache = map[string]harness.Evidence{}
	}
	w.evidenceCache[stamp] = ev
	w.evidenceMu.Unlock()
	return ev
}

// harnessFailing says which of the project's skills or agents the runs
// since they last changed hold patterns about ("skills", "agents" or ""),
// for the update tile (HE-12).
func (w *Workspace) harnessFailing() string {
	if !w.cfg.Audit.Enabled {
		return ""
	}
	at := func(parts ...string) string { return filepath.Join(append([]string{w.Dir(), ".agents"}, parts...)...) }
	for _, which := range []string{"skills", "agents"} {
		names := projectNames(at(which), which == "agents")
		changed := newestFile(at(which), "")
		if len(names) == 0 || changed.IsZero() {
			continue
		}
		for _, it := range w.cachedEvidence(changed).About(which).Items {
			if it.Patterned() && aboutOneOf(it, names) {
				return which
			}
		}
	}
	return ""
}

// projectNames are the names of the skills (folders) or agents (.md
// files) in dir.
func projectNames(dir string, files bool) map[string]bool {
	entries, _ := os.ReadDir(dir)
	out := map[string]bool{}
	for _, e := range entries {
		switch name := e.Name(); {
		case files && !e.IsDir() && strings.HasSuffix(name, ".md"):
			out[strings.TrimSuffix(name, ".md")] = true
		case !files && e.IsDir():
			out[name] = true
		}
	}
	return out
}

// aboutOneOf reports whether it is about one of names: its subject, or an
// agent refused or failing in it.
func aboutOneOf(it harness.Item, names map[string]bool) bool {
	if names[it.Subject] {
		return true
	}
	return slices.ContainsFunc(it.Seen, func(s harness.Seen) bool { return names[s.Agent] })
}

// ReflectDue reports whether the reflection schedule ([harness]
// reflect_schedule) wants a reflection now: its next time after the last
// reflection, scheduled or not. The schedule counts from when it's first
// seen, so turning it on doesn't reflect at once.
func (w *Workspace) ReflectDue(now time.Time) bool {
	s := strings.TrimSpace(w.cfg.Harness.ReflectSchedule)
	if s == "" || strings.EqualFold(s, "off") {
		return false
	}
	sched, err := workers.ParseSchedule(s, "")
	if err != nil {
		return false
	}
	st := w.harnessState()
	base := st.LastScheduled
	if st.LastReflect.After(base) {
		base = st.LastReflect
	}
	if base.IsZero() {
		w.updateHarnessState(func(st *harnessState) { st.LastScheduled = now })
		return false
	}
	return !sched.Next(base).After(now)
}

// scheduledReflectPrompt is what a scheduled reflection is asked.
const scheduledReflectPrompt = "You are running unattended, on this workspace's reflection schedule: nobody is watching or can answer, and nothing in the workspace may change. " +
	"Follow your steps 1 to 4. Then, instead of asking (step 5), call harness_propose once with the options you would have offered, " +
	"or with none if there's nothing worth changing, and stop: don't write any file, and end with one line saying how many you proposed."

// ScheduledReflect runs the schedule's reflection (HE-06): the reflector
// reads the evidence and keeps the changes it would offer for /reflect,
// in a read-only session of its own, deleted after, within [harness]
// max_cost_usd. Nothing in the workspace changes. Without new patterns
// since the last one, it doesn't run.
func (w *Workspace) ScheduledReflect(ctx context.Context) error {
	start := time.Now()
	ev := w.HarnessEvidence(w.reflectSince())
	last := w.harnessState().LastScheduled
	skip := ""
	switch {
	case ev.NoAudit:
		skip = "the audit log is off"
	case !w.hasProjectInstructions():
		skip = "there's no .agents/AGENTS.md (/setup makes one)"
	case !slices.ContainsFunc(ev.Items, func(it harness.Item) bool { return it.Patterned() && it.Last.After(last) }):
		skip = "nothing new to learn since the last scheduled reflection"
	}
	if skip != "" {
		w.updateHarnessState(func(st *harnessState) { st.LastScheduled, st.Skipped = start, skip })
		return nil
	}
	_, cost, _, err := w.unattended(ctx, "reflect "+start.Format("2006-01-02 15:04"), "harness-reflect", api.Turn{
		Text: "Scheduled reflection", Prompt: scheduledReflectPrompt, ReadOnly: "a scheduled reflection",
		MaxTurns: reflectMaxTurns, MaxCostUSD: w.cfg.Harness.MaxCostUSD, Timeout: reflectTimeout,
	}, nil, nil)
	w.updateHarnessState(func(st *harnessState) {
		st.LastScheduled, st.LastCost, st.Skipped = start, cost, ""
		if err != nil {
			st.Skipped = "it stopped: " + err.Error()
		}
	})
	return err
}

// isReflect reports whether a command turn's text runs /reflect.
func isReflect(text string) bool {
	name, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimSpace(text), "/"), " ")
	return strings.EqualFold(name, "reflect")
}
