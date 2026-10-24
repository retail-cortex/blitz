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

// Package harness is what a workspace's harness learns from its runs
// (spec_harness_learning_048): the evidence of what went wrong in its
// sessions, drawn from the audit log without a model, for the reflector
// and /skills update and /agents update to propose changes from.
package harness

import (
	"cmp"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/engine/audit"
	"github.com/retail-cortex/blitz/pkg/textutil"
)

// Kind is what an evidence item is about.
type Kind string

// Kinds of evidence (HE-02).
const (
	ToolFailed Kind = "tool_failed" // a tool call that failed, or a command that exited nonzero
	Denied     Kind = "denied"      // an action the user, the auto reviewer or a permission rule refused
	Blocked    Kind = "blocked"     // a guardrail's block
	Revised    Kind = "revised"     // a rule that sent the work back, or blocked it
	Steered    Kind = "steered"     // a message the user sent while the agent worked
	Corrected  Kind = "corrected"   // a prompt that rejects the last answer
	Undone     Kind = "undone"      // /undo, or a rewind
	Stopped    Kind = "stopped"     // a turn the user stopped
	Limited    Kind = "limited"     // a turn that reached a limit (Target: steps, cost or time)
	Skill      Kind = "skill"       // a skill used in turns that went wrong
	Agent      Kind = "agent"       // an agent invoked in turns that went wrong
	// ScratchRemade is a file under .agents/scratch written again in
	// another session; ScratchScript a script there run again in another
	// session (HE-61). Each counts sessions, not writes or runs.
	ScratchRemade Kind = "scratch_remade"
	ScratchScript Kind = "scratch_script"
)

// scratchDir is the harness's scratch folder (config.ProjectScratchDir),
// as paths name it.
const scratchDir = ".agents/scratch/"

// scratchScript finds a script under the scratch folder in a command.
var scratchScript = regexp.MustCompile(`\.agents/scratch/[\w./-]+\.(?:sh|py|js|ts|rb|pl)\b`)

// fileWriters are the tools that write a file at their path argument.
var fileWriters = []string{"create_file", "replace_in_file", "write_file", "edit_file"}

// retried is Seen.Then when the failed call itself worked when made again.
const retried = "the same call, retried"

// MaxItems bounds the evidence, so the reflector's prompt stays small.
const MaxItems = 200

// maxSeen is how many occurrences an item cites.
const maxSeen = 3

// Session is what the evidence says about a session.
type Session struct {
	Title string
	// Origin is a job's ("worker") or a detached ("background") run; ""
	// a chat.
	Origin string
	// Agent is the session's agent.
	Agent string
}

// Inputs are what the evidence is made from.
type Inputs struct {
	// Since is where the evidence starts.
	Since time.Time
	// Audit is the workspace's audit log since then, in the order written.
	Audit []audit.Entry
	// NoAudit means the audit log is off, so there's no evidence.
	NoAudit bool
	// Sessions are the workspace's sessions, by ID. Unless it's nil, the
	// entries of sessions it leaves out (evals' cases) are skipped, and so
	// are those that name neither a session nor a workspace.
	Sessions map[string]Session
	// Fixes are the health judge's latest suggestions, by item
	// ("skill/deploy", HL-10).
	Fixes map[string][]string
}

// Seen is one occurrence of an item.
type Seen struct {
	Session string
	Title   string
	Origin  string
	// Turn counts the session's prompts within the evidence, from 1.
	Turn    int
	Time    time.Time
	Excerpt string
	// Agent is the agent that was refused or whose tool failed, when the
	// audit log says.
	Agent string
	// Then is, for a failed call, what worked after it in the same turn:
	// the next call of the same tool that succeeded, by its command or
	// path, or "the same call, retried" (HE-60); "" when none did.
	Then string
}

// Item is one thing that happened, as often as it happened.
type Item struct {
	Kind Kind
	// Subject is the tool, rule, guardrail, skill or agent it's about.
	Subject string
	// Target is the command or path, or for a limit which one.
	Target string
	Count  int
	First  time.Time
	Last   time.Time
	// Seen are the latest occurrences, at most three, newest first.
	Seen []Seen
	// Fixes are the health judge's suggestions for the subject.
	Fixes []string
	// costly is an item that counts as a pattern on its own.
	costly bool
	// key is what groups occurrences into the item.
	key string
}

// Patterned reports whether the item is a pattern (HE-03 step 1): it
// happened at least twice, or once with a cost: a change undone, a turn
// at a limit, an action the user refused.
func (it Item) Patterned() bool { return it.Count >= 2 || it.costly }

// Evidence is what went wrong in a workspace's sessions since a time.
type Evidence struct {
	Since time.Time
	// Items are patterns first, then the most frequent and most recent.
	Items []Item
	// Dropped is how many items MaxItems left out.
	Dropped int
	// Used counts the turns each skill and agent was in ("skill/deploy",
	// "agent/qa"), whether they went well or not.
	Used map[string]int
	// NoAudit means the audit log is off, so there's no evidence.
	NoAudit bool
}

// Score is what the suggestion to reflect weighs (HE-62): the patterns,
// the costly ones (an undo, a limit, a refusal by the user) twice.
func (e Evidence) Score() int {
	n := 0
	for _, it := range e.Items {
		if it.Patterned() {
			n++
			if it.costly {
				n++
			}
		}
	}
	return n
}

// Patterns is how many items are patterns: what the welcome tile counts
// (HE-05).
func (e Evidence) Patterns() int {
	n := 0
	for _, it := range e.Items {
		if it.Patterned() {
			n++
		}
	}
	return n
}

// About is the evidence about skills, agents or rules (with guardrails)
// only, for /skills update, /agents update and /rules update; "" all of
// it.
func (e Evidence) About(about string) Evidence {
	var kinds []Kind
	prefix := ""
	switch about {
	case "skills":
		kinds, prefix = []Kind{Skill}, "skill/"
	case "agents":
		kinds, prefix = []Kind{Agent, Steered, Corrected, Undone, Stopped, Limited, Denied}, "agent/"
	case "rules":
		kinds, prefix = []Kind{Revised, Blocked, Denied}, "-"
	default:
		return e
	}
	out := Evidence{Since: e.Since, NoAudit: e.NoAudit, Used: map[string]int{}}
	for _, it := range e.Items {
		if slices.Contains(kinds, it.Kind) || about == "skills" && it.Kind == ToolFailed && (it.Subject == "run_skill_script" || it.Subject == "activate_skill") {
			out.Items = append(out.Items, it)
		}
	}
	for k, n := range e.Used {
		if strings.HasPrefix(k, prefix) {
			out.Used[k] = n
		}
	}
	return out
}

// turnState is one turn as the audit tells it.
type turnState struct {
	session  string
	n        int
	prompt   string
	troubled bool
	used     map[string]bool
	at       time.Time
}

// builder groups occurrences into items.
type builder struct {
	in    Inputs
	items map[string]*Item
	// scratch are the sessions that wrote or ran each scratch file, by
	// kind and path.
	scratch map[string]map[string]bool
}

// fix is a failed call's occurrence waiting for what worked after it.
type fix struct {
	key          string // its item
	i            int    // its occurrence
	tool, target string
}

// Build makes the evidence from in.
func Build(in Inputs) Evidence {
	b := &builder{in: in, items: map[string]*Item{}, scratch: map[string]map[string]bool{}}
	// waiting are the session's failed calls this turn, for what worked
	// after them (HE-60).
	waiting := map[string][]fix{}
	used := map[string]int{}
	turns := map[string]*turnState{}
	lastCall := map[string]audit.Entry{}
	// refused is the tool a denial refused last, by session: its result's
	// error repeats the denial, which is counted already.
	refused := map[string]string{}

	// endTurn counts what the turn used, and blames its skills and
	// agents when it went wrong.
	endTurn := func(t *turnState) {
		if t == nil {
			return
		}
		for u := range t.used {
			used[u]++
			if t.troubled {
				kind, name, _ := strings.Cut(u, "/")
				b.add(t, Kind(kind), name, "", t.at, t.prompt, false)
			}
		}
	}
	for _, e := range in.Audit {
		if in.Sessions != nil {
			if e.Session == "" && e.Workspace == "" {
				continue // whose it was isn't known: entries written before they named both
			}
			if _, ok := in.Sessions[e.Session]; e.Session != "" && !ok {
				continue
			}
		}
		t := turns[e.Session]
		if e.Kind == audit.KindPrompt && e.Decision != audit.DecisionSteer {
			endTurn(t)
			n := 1
			if t != nil {
				n = t.n + 1
			}
			t = &turnState{session: e.Session, n: n, prompt: e.Detail, used: map[string]bool{}, at: e.Time}
			turns[e.Session] = t
			delete(waiting, e.Session)
			if n > 1 && IsCorrection(e.Detail) {
				t.troubled = true
				b.add(t, Corrected, b.agent(e.Session), "", e.Time, e.Detail, false)
			}
			continue
		}
		if t == nil { // before the evidence's first prompt in the session
			t = &turnState{session: e.Session, used: map[string]bool{}}
			turns[e.Session] = t
		}
		t.at = e.Time
		switch e.Kind {
		case audit.KindPrompt: // a steer
			t.troubled = true
			b.add(t, Steered, b.agent(e.Session), "", e.Time, e.Detail, false)
		case audit.KindToolCall:
			lastCall[e.Session] = e
			b.scratchUse(t, e)
			switch e.Tool {
			case "activate_skill", "run_skill_script":
				if name := argString(e.Args, "skill_name", "skill", "name"); name != "" {
					t.used["skill/"+name] = true
				}
			case "invoke_agent":
				if name := argString(e.Args, "agent_name", "agent", "name"); name != "" {
					t.used["agent/"+name] = true
				}
			}
		case audit.KindToolResult:
			if e.Decision == "ok" || e.Decision == "" {
				waiting[e.Session] = b.worked(waiting[e.Session], e.Tool, lastCall[e.Session])
				continue
			}
			if refused[e.Session] == e.Tool {
				delete(refused, e.Session)
				continue
			}
			if stopped(e.Error) { // the turn's stop says so (turn_end)
				continue
			}
			t.troubled = true
			why := e.Error
			if e.Detail != "" {
				why += ": " + e.Detail
			}
			tgt := target(e.Args)
			b.add(t, ToolFailed, e.Tool, tgt, e.Time, why, false).Agent = e.Agent
			key := string(ToolFailed) + "\x00" + e.Tool + "\x00" + tgt
			waiting[e.Session] = append(waiting[e.Session], fix{key: key, i: len(b.items[key].Seen) - 1, tool: e.Tool, target: tgt})
		case audit.KindDenial:
			refused[e.Session] = e.Tool
			by, user := denier(e.Decision)
			if by == "" {
				continue
			}
			t.troubled = true
			tgt := target(lastCall[e.Session].Args)
			if c := lastCall[e.Session]; c.Tool != e.Tool || tgt == "" {
				tgt = textutil.Ellipsize(firstLine(e.Detail), 120)
			}
			b.add(t, Denied, e.Tool, tgt, e.Time, by, user).Agent = e.Agent
		case audit.KindGuardrail:
			if e.Decision != "block" && e.Decision != "unjudged-block" {
				continue
			}
			t.troubled = true
			b.add(t, Blocked, strings.TrimPrefix(e.Tool, "guardrail:"), "", e.Time, e.Detail, false)
		case audit.KindRule:
			if e.Decision != "revise" && e.Decision != "block" {
				continue
			}
			t.troubled = true
			b.add(t, Revised, strings.TrimPrefix(e.Tool, "rule:"), "", e.Time, e.Detail, false)
		case audit.KindUndo:
			t.troubled = true
			b.add(t, Undone, b.agent(e.Session), "", e.Time, cmp.Or(t.prompt, e.Detail), true)
		case audit.KindTurnEnd:
			t.troubled = true
			switch e.Decision {
			case audit.DecisionStopped:
				b.add(t, Stopped, b.agent(e.Session), "", e.Time, t.prompt, false)
			case audit.DecisionLimit:
				b.add(t, Limited, b.agent(e.Session), e.Detail, e.Time, t.prompt, true)
			}
		}
	}
	for _, t := range turns {
		endTurn(t)
	}
	return b.evidence(used)
}

// add records one occurrence.
func (b *builder) add(t *turnState, kind Kind, subject, target string, at time.Time, excerpt string, costly bool) *Seen {
	key := string(kind) + "\x00" + subject + "\x00" + target
	it := b.items[key]
	if it == nil {
		it = &Item{Kind: kind, Subject: subject, Target: target, First: at, key: key}
		if fixes := b.in.Fixes[fixKey(kind, subject)]; len(fixes) > 0 {
			it.Fixes = fixes
		}
		b.items[key] = it
	}
	it.Count++
	it.costly = it.costly || costly
	if at.Before(it.First) {
		it.First = at
	}
	if at.After(it.Last) {
		it.Last = at
	}
	s := b.in.Sessions[t.session]
	it.Seen = append(it.Seen, Seen{Session: t.session, Title: s.Title, Origin: s.Origin, Turn: t.n, Time: at, Excerpt: textutil.Ellipsize(strings.TrimSpace(excerpt), 300)})
	return &it.Seen[len(it.Seen)-1]
}

// worked fills in what worked for the failed calls of tool waiting, now
// that its call c succeeded, and returns those still waiting.
func (b *builder) worked(waiting []fix, tool string, c audit.Entry) []fix {
	if c.Tool != tool {
		return waiting
	}
	tgt := target(c.Args)
	rest := waiting[:0]
	for _, f := range waiting {
		if f.tool != tool {
			rest = append(rest, f)
			continue
		}
		then := tgt
		if then == f.target {
			then = retried
		}
		b.items[f.key].Seen[f.i].Then = then
	}
	return rest
}

// scratchUse counts a call that writes a scratch file or runs a scratch
// script, once a session (HE-61).
func (b *builder) scratchUse(t *turnState, e audit.Entry) {
	var kind Kind
	var path string
	switch {
	case slices.Contains(fileWriters, e.Tool):
		p := filepath.ToSlash(argString(e.Args, "path", "file_path"))
		if i := strings.Index(p, scratchDir); i >= 0 && (i == 0 || p[i-1] == '/') {
			kind, path = ScratchRemade, p[i:]
		}
	case e.Tool == "run_shell_command":
		if m := scratchScript.FindString(argString(e.Args, "command")); m != "" {
			kind, path = ScratchScript, m
		}
	}
	if kind == "" || e.Session == "" {
		return
	}
	k := string(kind) + "\x00" + path
	if b.scratch[k] == nil {
		b.scratch[k] = map[string]bool{}
	}
	if b.scratch[k][e.Session] {
		return
	}
	b.scratch[k][e.Session] = true
	b.add(t, kind, "scratch", path, e.Time, t.prompt, false)
}

// agent is a session's agent, what corrections and stops are about.
func (b *builder) agent(session string) string {
	return cmp.Or(b.in.Sessions[session].Agent, "blitz")
}

// evidence orders and caps the items.
func (b *builder) evidence(used map[string]int) Evidence {
	ev := Evidence{Since: b.in.Since, Used: used, NoAudit: b.in.NoAudit}
	for _, it := range b.items {
		if (it.Kind == ScratchRemade || it.Kind == ScratchScript) && it.Count < 2 {
			continue // used in one session: what scratch is for
		}
		slices.SortStableFunc(it.Seen, func(a, b Seen) int { return b.Time.Compare(a.Time) })
		if len(it.Seen) > maxSeen {
			it.Seen = it.Seen[:maxSeen]
		}
		ev.Items = append(ev.Items, *it)
	}
	slices.SortFunc(ev.Items, func(a, b Item) int {
		if a.Patterned() != b.Patterned() {
			if a.Patterned() {
				return -1
			}
			return 1
		}
		return cmp.Or(b.Count-a.Count, b.Last.Compare(a.Last), strings.Compare(a.key, b.key))
	})
	if len(ev.Items) > MaxItems {
		ev.Dropped = len(ev.Items) - MaxItems
		ev.Items = ev.Items[:MaxItems]
	}
	return ev
}

// fixKey is the health item a kind of evidence is about ("" none).
func fixKey(kind Kind, subject string) string {
	switch kind {
	case Skill:
		return "skill/" + subject
	case Agent, Steered, Corrected, Undone, Stopped, Limited:
		return "agent/" + subject
	case Revised:
		return "rule/" + subject
	case Blocked:
		return "guardrail/" + subject
	}
	return ""
}

// stopped reports whether a tool failed because its turn was stopped or
// ran out of time.
func stopped(err string) bool {
	return strings.HasSuffix(err, "context canceled") || strings.HasSuffix(err, "context deadline exceeded")
}

// denier is who refused an action, from a denial's decision ("" not a
// refusal: no one could approve it, or asking failed), and whether it was
// the user.
func denier(decision string) (string, bool) {
	switch {
	case decision == "deny":
		return "denied by the user", true
	case strings.HasPrefix(decision, "auto-deny"):
		return "denied by the auto reviewer: " + strings.TrimSpace(strings.TrimPrefix(decision, "auto-deny:")), false
	case strings.HasPrefix(decision, "rule-deny"):
		return "denied by the permission rule " + strings.TrimSpace(strings.TrimPrefix(decision, "rule-deny")), false
	case decision == "unattended-refused":
		return "refused: it needs approval and nobody was there", false
	}
	return "", false
}

// target is what a call acted on: its command or path.
func target(args map[string]any) string {
	if c := argString(args, "command"); c != "" {
		return textutil.Ellipsize(firstLine(c), 120)
	}
	return argString(args, "path", "file_path", "url", "skill_name", "agent_name")
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}

func argString(args map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := args[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// Text is the evidence as the reflector reads it: one line per item,
// then where it was seen.
func (e Evidence) Text() string {
	var b strings.Builder
	if e.NoAudit {
		return "There's no evidence: the audit log is off ([audit] enabled = false in the settings), and the evidence is drawn from it.\n"
	}
	fmt.Fprintf(&b, "Evidence since %s: %d items, %d of them patterns.\n", e.Since.Format(time.DateOnly), len(e.Items), e.Patterns())
	if e.Dropped > 0 {
		fmt.Fprintf(&b, "(%d less frequent items left out.)\n", e.Dropped)
	}
	for _, it := range e.Items {
		b.WriteString("\n- ")
		if it.Patterned() {
			b.WriteString("[pattern] ")
		}
		fmt.Fprintf(&b, "%s %s", it.Kind, it.Subject)
		if it.Target != "" {
			fmt.Fprintf(&b, " `%s`", it.Target)
		}
		fmt.Fprintf(&b, " ×%d", it.Count)
		if it.Count > 1 {
			fmt.Fprintf(&b, " (%s – %s)", it.First.Format(time.DateOnly), it.Last.Format(time.DateOnly))
		}
		b.WriteByte('\n')
		for _, s := range it.Seen {
			fmt.Fprintf(&b, "  · %s %q", s.Time.Format("2006-01-02 15:04"), cmp.Or(s.Title, s.Session))
			if s.Origin != "" {
				fmt.Fprintf(&b, " [%s run]", s.Origin)
			}
			if s.Turn > 0 {
				fmt.Fprintf(&b, " turn %d", s.Turn)
			}
			if s.Agent != "" {
				fmt.Fprintf(&b, " agent %s", s.Agent)
			}
			if s.Excerpt != "" {
				fmt.Fprintf(&b, ": %s", strings.ReplaceAll(s.Excerpt, "\n", " ⏎ "))
			}
			if s.Then != "" {
				fmt.Fprintf(&b, " → then worked: `%s`", s.Then)
			}
			b.WriteByte('\n')
		}
		for _, f := range it.Fixes {
			fmt.Fprintf(&b, "  · health check suggests: %s\n", f)
		}
	}
	if len(e.Used) > 0 {
		names := make([]string, 0, len(e.Used))
		for k := range e.Used {
			names = append(names, k)
		}
		slices.Sort(names)
		b.WriteString("\nTurns each skill and agent was in:")
		for _, k := range names {
			fmt.Fprintf(&b, " %s %d;", k, e.Used[k])
		}
		b.WriteByte('\n')
	}
	return b.String()
}
