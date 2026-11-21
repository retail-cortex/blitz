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
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/retail-cortex/blitz/pkg/redact"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// maxThoughtSteps bounds the steps one call's reasoning splits into: past
// it, paragraphs are grouped so the graph stays readable.
const maxThoughtSteps = 8

// maxAgentDepth bounds how deep sub-agents nest in the graph.
const maxAgentDepth = 3

// priceFunc estimates one call's usage: model answered, um its tokens.
type priceFunc func(model string, um *genai.GenerateContentResponseUsageMetadata) api.Usage

// Reasoning is session id's turns and the reasoning graph of its turn-th
// (from 0; a negative or past-the-end turn is the last), read from its
// event log and its sub-agents' (spec_066_reasoning RSN-01). The
// reasoning is masked as the conversation's is (spec_056 CA-29a), and
// costs are estimated at today's prices.
func (w *Workspace) Reasoning(id string, turn int) (api.Reasoning, error) {
	dir := w.cfg.Session.StorageDir
	evs, err := session.ReadEvents(dir, id)
	if err != nil {
		return api.Reasoning{}, err
	}
	subs, err := session.ReadSubagentEvents(dir, id)
	if err != nil {
		return api.Reasoning{}, err
	}
	price := func(model string, um *genai.GenerateContentResponseUsageMetadata) api.Usage {
		if w.engine == nil {
			return tokensOnly(um)
		}
		return w.engine.EstimateUsage(model, um)
	}
	return buildReasoning(evs, subs, turn, price), nil
}

// tokensOnly is um's tokens, unpriced.
func tokensOnly(um *genai.GenerateContentResponseUsageMetadata) api.Usage {
	u := api.Usage{Calls: 1, Unpriced: 1}
	if um != nil {
		u.Input = int64(um.PromptTokenCount)
		u.Output = int64(um.CandidatesTokenCount) + int64(um.ThoughtsTokenCount)
		u.Reasoning = int64(um.ThoughtsTokenCount)
	}
	return u
}

// turnSpan is one turn's prompt and the events after it.
type turnSpan struct {
	prompt string
	start  time.Time
	evs    []*adksession.Event
}

func buildReasoning(evs, subs []*adksession.Event, want int, price priceFunc) api.Reasoning {
	var turns []turnSpan
	for _, ev := range evs {
		if ev == nil || ev.Content == nil || ev.Partial {
			continue
		}
		if p := promptOf(ev); p != "" {
			turns = append(turns, turnSpan{prompt: p, start: ev.Timestamp})
			continue
		}
		if len(turns) > 0 {
			turns[len(turns)-1].evs = append(turns[len(turns)-1].evs, ev)
		}
	}
	out := api.Reasoning{Turn: -1}
	for i, t := range turns {
		end := time.Time{}
		if i+1 < len(turns) {
			end = turns[i+1].start
		}
		s := api.ReasoningTurn{Index: i, Prompt: firstLine(t.prompt, 120), Start: t.start}
		sum := func(ev *adksession.Event) {
			if !isCall(ev) {
				return
			}
			s.Calls++
			s.Usage.Add(callUsage(ev, price))
			for _, p := range ev.Content.Parts {
				if p.Thought && strings.TrimSpace(p.Text) != "" {
					s.Thoughts += len(thoughtSteps(p.Text))
				}
			}
		}
		for _, ev := range t.evs {
			sum(ev)
		}
		for _, ev := range subs {
			if ev != nil && ev.Content != nil && !ev.Partial && !ev.Timestamp.Before(t.start) && (end.IsZero() || ev.Timestamp.Before(end)) {
				sum(ev)
			}
		}
		out.Turns = append(out.Turns, s)
	}
	if len(turns) == 0 {
		return out
	}
	if want < 0 || want >= len(turns) {
		want = len(turns) - 1
	}
	b := &reasoningBuilder{subs: subs, used: map[int]bool{}, price: price}
	b.turn(turns[want])
	out.Turn, out.Nodes, out.Cut = want, b.nodes, b.cut
	return out
}

// reasoningBuilder draws one turn's graph.
type reasoningBuilder struct {
	nodes []api.ReasoningNode
	cut   bool
	subs  []*adksession.Event
	used  map[int]bool // sub-agent events already drawn
	price priceFunc
}

// add appends n with the next ID, unless the graph is full; it returns
// the node's index, or -1.
func (b *reasoningBuilder) add(n api.ReasoningNode) int {
	if len(b.nodes) >= api.MaxReasoningNodes {
		b.cut = true
		return -1
	}
	n.ID = fmt.Sprintf("n%d", len(b.nodes))
	b.nodes = append(b.nodes, n)
	return len(b.nodes) - 1
}

func (b *reasoningBuilder) turn(t turnSpan) {
	root := b.add(api.ReasoningNode{Kind: api.NodeTurn, Title: firstLine(t.prompt, 120), Text: clipText(t.prompt), Author: "user", Start: t.start, Status: api.NodeOK, Usage: &api.Usage{}})
	end := t.start
	if n := len(t.evs); n > 0 {
		end = t.evs[n-1].Timestamp
	}
	running := b.calls(t.evs, b.nodes[root].ID, t.start, 0, b.nodes[root].Usage)
	b.nodes[root].Duration = max(end.Sub(t.start), 0)
	if running {
		b.nodes[root].Status = api.NodeRunning
	}
}

// calls draws evs's model calls under parent, their tools and their
// sub-agents, adding their usage to total; it reports whether a tool is
// still waiting for its result.
func (b *reasoningBuilder) calls(evs []*adksession.Event, parent string, since time.Time, depth int, total *api.Usage) (running bool) {
	tools := map[string]int{} // a tool call's ID: its node
	for _, ev := range evs {
		switch {
		case isCall(ev):
			b.call(ev, parent, since, depth, total, tools)
		default:
			for _, p := range ev.Content.Parts {
				if fr := p.FunctionResponse; fr != nil {
					if i, ok := tools[fr.ID]; ok {
						b.result(i, fr.Response, ev.Timestamp, depth, total)
						delete(tools, fr.ID)
					}
				}
			}
		}
		since = ev.Timestamp
	}
	return len(tools) > 0
}

func (b *reasoningBuilder) call(ev *adksession.Event, parent string, since time.Time, depth int, total *api.Usage, tools map[string]int) {
	o := originOf(ev)
	u := callUsage(ev, b.price)
	total.Add(u)
	c := b.add(api.ReasoningNode{Parent: parent, Kind: api.NodeCall, Author: ev.Author, Provider: o.Provider, Model: o.Model, Status: api.NodeOK, Start: since, Duration: max(ev.Timestamp.Sub(since), 0), Usage: &u})
	if c < 0 {
		return
	}
	id := b.nodes[c].ID
	var thought, said strings.Builder
	for _, p := range ev.Content.Parts {
		switch {
		case p.Thought && p.Text != "":
			thought.WriteString(p.Text)
		case p.Text != "":
			said.WriteString(p.Text)
		}
	}
	steps := thoughtSteps(redact.Mask(thought.String())) // titles too
	b.nodes[c].Hidden = len(steps) == 0 && u.Reasoning > 0
	for _, s := range steps {
		b.add(api.ReasoningNode{Parent: id, Kind: api.NodeThought, Title: s.title, Text: clipText(s.text), Author: ev.Author, Status: api.NodeOK, Start: ev.Timestamp})
	}
	for _, p := range ev.Content.Parts {
		fc := p.FunctionCall
		if fc == nil {
			continue
		}
		n := api.ReasoningNode{Parent: id, Kind: api.NodeTool, Tool: fc.Name, Title: fc.Name, Args: clipValues(fc.Args), Author: ev.Author, Status: api.NodeRunning, Start: ev.Timestamp}
		if fc.Name == "invoke_agent" {
			name, _ := fc.Args["agent_name"].(string)
			n.Kind, n.Title, n.Usage = api.NodeAgent, name, &api.Usage{}
			if bg, _ := fc.Args["background"].(bool); bg {
				n.Title = name + " (background)"
			}
		}
		if i := b.add(n); i >= 0 {
			tools[fc.ID] = i
		}
	}
	if s := strings.TrimSpace(redact.Mask(said.String())); s != "" {
		b.add(api.ReasoningNode{Parent: id, Kind: api.NodeText, Title: firstLine(s, 80), Text: clipText(s), Author: ev.Author, Status: api.NodeOK, Start: ev.Timestamp})
	}
}

// result ends tool node i with what it returned at end; a sub-agent's
// node gets the calls it made meanwhile.
func (b *reasoningBuilder) result(i int, res map[string]any, end time.Time, depth int, total *api.Usage) {
	n := &b.nodes[i]
	n.Result, n.Duration, n.Status = clipValues(res), max(end.Sub(n.Start), 0), api.NodeOK
	if e, _ := res["error"].(string); e != "" {
		n.Status = api.NodeFailed
	}
	if n.Kind != api.NodeAgent || depth >= maxAgentDepth {
		return
	}
	name, _ := n.Args["agent_name"].(string)
	start, id := n.Start, n.ID
	var own []*adksession.Event
	for j, ev := range b.subs {
		if b.used[j] || ev == nil || ev.Content == nil || ev.Partial || ev.Author != name || ev.Timestamp.Before(start) || ev.Timestamp.After(end) {
			continue
		}
		b.used[j] = true
		own = append(own, ev)
	}
	agent := &api.Usage{}
	b.calls(own, id, start, depth+1, agent)
	b.nodes[i].Usage = agent // b.nodes may have grown: index, not n
	total.Add(*agent)
}

// promptOf is ev's text if it's the user's prompt (a turn's start), "".
func promptOf(ev *adksession.Event) string {
	if ev.Author != "user" {
		return ""
	}
	var sb strings.Builder
	for _, p := range ev.Content.Parts {
		if p.Text != "" && !p.Thought {
			sb.WriteString(p.Text)
		}
	}
	return redact.Mask(strings.TrimSpace(sb.String()))
}

// isCall reports whether ev is a model's answer: one model call.
func isCall(ev *adksession.Event) bool {
	return ev != nil && ev.Content != nil && !ev.Partial && ev.Content.Role == genai.RoleModel && ev.Author != "user"
}

func originOf(ev *adksession.Event) runtime.Origin {
	m, _ := ev.CustomMetadata[runtime.OriginKey].(map[string]any)
	str := func(k string) string { s, _ := m[k].(string); return s }
	o := runtime.Origin{API: str("api"), Provider: str("provider"), Model: str("model")}
	if o.Model == "" {
		o.Model = ev.ModelVersion
	}
	return o
}

func callUsage(ev *adksession.Event, price priceFunc) api.Usage {
	if ev.UsageMetadata == nil {
		return api.Usage{Calls: 1, Unpriced: 1}
	}
	return price(originOf(ev).Model, ev.UsageMetadata)
}

// thoughtStep is one step of a call's reasoning.
type thoughtStep struct{ title, text string }

// thoughtSteps splits reasoning into its steps: at the headings a model
// gives its summary (a line in bold, as Gemini's), or else at its
// paragraphs, grouped to at most maxThoughtSteps.
func thoughtSteps(text string) []thoughtStep {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if text == "" {
		return nil
	}
	var steps []thoughtStep
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if h, ok := boldHeading(t); ok {
			steps = append(steps, thoughtStep{title: h})
			continue
		}
		if len(steps) == 0 {
			steps = append(steps, thoughtStep{})
		}
		s := &steps[len(steps)-1]
		s.text += line + "\n"
	}
	if len(steps) > 1 || steps[0].title != "" {
		for i := range steps {
			steps[i].text = strings.TrimSpace(steps[i].text)
			if steps[i].title == "" {
				steps[i].title = firstLine(steps[i].text, 80)
			}
		}
		return groupSteps(steps)
	}
	var paras []thoughtStep
	for _, p := range strings.Split(text, "\n\n") {
		if p = strings.TrimSpace(p); p != "" {
			paras = append(paras, thoughtStep{title: firstSentence(p, 80), text: p})
		}
	}
	return groupSteps(paras)
}

// boldHeading is a line that's all bold (**Like this**), without its
// stars.
func boldHeading(line string) (string, bool) {
	if len(line) < 5 || !strings.HasPrefix(line, "**") || !strings.HasSuffix(line, "**") {
		return "", false
	}
	h := strings.TrimSpace(line[2 : len(line)-2])
	if h == "" || strings.Contains(h, "**") {
		return "", false
	}
	return h, true
}

// groupSteps joins steps into at most maxThoughtSteps, keeping each
// group's first title.
func groupSteps(steps []thoughtStep) []thoughtStep {
	if len(steps) <= maxThoughtSteps {
		return steps
	}
	per := (len(steps) + maxThoughtSteps - 1) / maxThoughtSteps
	var out []thoughtStep
	for i := 0; i < len(steps); i += per {
		g := steps[i:min(i+per, len(steps))]
		parts := make([]string, len(g))
		for j, s := range g {
			parts[j] = s.text
			if s.title != "" && !strings.HasPrefix(s.text, s.title) {
				parts[j] = "**" + s.title + "**\n" + s.text
			}
		}
		out = append(out, thoughtStep{title: g[0].title, text: strings.Join(parts, "\n\n")})
	}
	return out
}

// firstLine is s's first non-empty line, cut to n runes.
func firstLine(s string, n int) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(strings.Trim(strings.TrimSpace(line), "*#")); line != "" {
			return cutRunes(line, n)
		}
	}
	return ""
}

// firstSentence is s's first sentence (or line), cut to n runes.
func firstSentence(s string, n int) string {
	line := firstLine(s, 1<<20)
	if i := strings.IndexAny(line, ".?!"); i > 0 && i+1 < len(line) {
		line = line[:i+1]
	}
	return cutRunes(line, n)
}

func cutRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

func clipText(s string) string { return clip(s, api.MaxReasoningText) }
