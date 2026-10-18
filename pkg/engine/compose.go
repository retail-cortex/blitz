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
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/compose"
	"github.com/retail-cortex/blitz/pkg/engine/flows"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
)

// The composer (spec_composer_045): a guided conversation that makes a
// workspace's component, checked with the engine's own rules.

// composeSessions are the composer's conversations going, in memory.
var composeSessions compose.Store

// ErrNoRecipe means the composer can't make that kind (yet).
var ErrNoRecipe = errors.New("the composer can't make that kind of component")

// ComposeKinds are the kinds the composer makes.
func ComposeKinds() []string { return []string{"workflow"} }

// recipe is kind's recipe for this workspace.
func (w *Workspace) recipe(kind string) (compose.Recipe, error) {
	switch kind {
	case "workflow":
		return compose.Recipe{Kind: "workflow", Brief: workflowBrief, Check: checkWorkflowDraft}, nil
	}
	return compose.Recipe{}, fmt.Errorf("%w: %q", ErrNoRecipe, kind)
}

// checkWorkflowDraft is what doesn't hold in a drafted workflow file.
func checkWorkflowDraft(name, file string) []string {
	if !flows.ValidName(name) {
		return []string{fmt.Sprintf("name %q: lowercase letters, digits and single hyphens, up to 64", name)}
	}
	f := flows.Parse(name, "", []byte(file))
	if f.Problem == "" {
		return nil
	}
	return strings.Split(f.Problem, "; ")
}

// workflowBrief tells the model what a workflow is and its file.
const workflowBrief = `A workflow is steps run in order, each step's output the next one's input (.agents/workflows/<name>.flow.yaml). The file:

description: one line
mode: auto            # leave out for auto (a reviewer model decides what would ask); or accept-edits, default (ask), dont-ask
nodes:                # the steps; ids lowercase-with-hyphens; position lays them out left to right (x += 280 per step)
  - {id: notes, type: read_file, title: Read the notes, path: notes.md, position: {x: 0, y: 80}}
  - id: draft
    type: agent
    title: Draft the brief
    instructions: Write a one-page brief from the notes.
    position: {x: 280, y: 80}
  - {id: save, type: save_file, title: Save it, path: "briefs/{{ input }}.md", position: {x: 560, y: 80}}
edges:                # links: each step's output is the next one's input
  - {from: notes, to: draft}
  - {from: draft, to: save}

Step types and their settings:
- agent: instructions (what to do; required), agent (a named agent, else the active one), skills [names], rules [names]
- refine: instructions (how to improve the previous output); summarize: instructions (what to focus on); browser: instructions (a goal on the web)
- extract: fields [{name, type: string|number|boolean|list, description}] pulled out of the input as data
- accumulate: joins its inputs; read_file: path; save_file: path (writes its input)
- script: skill, script (a skill's script name), args [strings]
- mcp_call: server, tool, arguments {…}; api_call: url, method, query [{name, value}], headers [{name, value}], body or json, auth
- workflow: workflow (another workflow's name, run as one step)
- human_task: form (A2UI v0.9 components: Column/Row/Text/TextField/CheckBox/ChoicePicker/Button with action {event: {name: approve, context: {…}}}); its answer's data has action and the values
- timer: wait (a duration: 2h) or until (a time)
- exclusive / inclusive / parallel: gateways; their links take when (a condition in plain words, judged by a model) or if (a CEL expression on output.data / steps.<id>.data), and one default: true
Any step: retry {attempts, delay}, timeout (30s), for_each (a CEL list; the step sees {{ item }}), error links (an edge with error: true catches its failure).
Settings take {{ templates }}: {{ input }}, {{ steps.<id>.text }}, {{ steps.<id>.data.<field> }}; secrets as {secrets:name}.
Keep it as small as the job allows.`

// composeContext describes the workspace for the model: what a workflow may
// refer to, by name with its description.
func (w *Workspace) composeContext(ctx context.Context) string {
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	b.WriteString("Agents:\n")
	for _, a := range w.ListAgents() {
		line("- %s: %s", a.Name, a.Description)
	}
	b.WriteString("Skills:\n")
	for _, s := range w.ListSkills() {
		scripts := ""
		for _, sc := range s.Scripts {
			scripts += " " + sc.Name
		}
		if scripts != "" {
			scripts = " (scripts:" + scripts + ")"
		}
		line("- %s: %s%s", s.Name, s.Description, scripts)
	}
	b.WriteString("Rules:\n")
	for _, r := range w.Rules() {
		if r.When != "" {
			line("- %s: %s", r.Name, cmp.Or(r.Description, r.Text))
		}
	}
	b.WriteString("Workflows:\n")
	for _, f := range w.Workflows() {
		line("- %s: %s", f.Name, f.Description)
	}
	if servers := w.ListMCPServers(); len(servers) > 0 {
		b.WriteString("MCP servers:\n")
		for _, s := range servers {
			line("- %s", s.Name)
		}
	}
	if names := w.secretNames(); len(names) > 0 {
		line("Secrets (names only): %s", strings.Join(names, ", "))
	}
	_ = ctx
	return b.String()
}

// secretNames are the names of the secrets the workspace can use.
func (w *Workspace) secretNames() []string {
	var out []string
	vault := config.Vault(w.cfg.Dir)
	for _, scope := range []string{config.SecretScope(w.cfg.Dir, w.Dir()), config.SecretScope(w.cfg.Dir, "")} {
		list, err := vault.List(scope)
		if err != nil {
			continue
		}
		for _, e := range list {
			out = append(out, e.Key)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// composeAsker asks the composer's model ([suggestions] model, else the
// auto mode's reviewer, else the default), its cost estimated.
func (w *Workspace) composeAsker() compose.Asker {
	return func(ctx context.Context, instruction, prompt string) (string, float64, error) {
		llm, err := w.newModel(ctx, w.cfg, cmp.Or(w.cfg.Suggestions.Model, w.cfg.Permissions.Auto.Model))
		if err != nil {
			return "", 0, fmt.Errorf("%s", ModelErrorSummary(err, w.cfg))
		}
		reply, served, usage, err := runtime.Ask(ctx, llm, instruction, prompt)
		return reply, w.engine.EstimateUsage(served, usage).CostUSD, err
	}
}

// StartCompose starts a composer conversation for kind with the user's
// request (current: the component being changed, as its file, or "") and
// runs its first turn.
func (w *Workspace) StartCompose(ctx context.Context, kind, request, current string) (compose.Session, error) {
	r, err := w.recipe(kind)
	if err != nil {
		return compose.Session{}, err
	}
	s := composeSessions.New(kind, w.Dir(), current, request)
	s.Lock()
	defer s.Unlock()
	if err := compose.Step(ctx, r, w.composeContext(ctx), s, w.composeAsker()); err != nil {
		composeSessions.Drop(w.Dir(), s.ID)
		return compose.Session{}, err
	}
	return s.Snapshot(), nil
}

// AnswerCompose gives session id the user's answer (or a change to its
// draft) and runs the composer's next turn.
func (w *Workspace) AnswerCompose(ctx context.Context, id, answer string) (compose.Session, error) {
	s, err := composeSessions.Get(w.Dir(), id)
	if err != nil {
		return compose.Session{}, err
	}
	r, err := w.recipe(s.Kind)
	if err != nil {
		return compose.Session{}, err
	}
	s.Lock()
	defer s.Unlock()
	compose.Answer(s, answer)
	if err := compose.Step(ctx, r, w.composeContext(ctx), s, w.composeAsker()); err != nil {
		return s.Snapshot(), err
	}
	return s.Snapshot(), nil
}

// DropCompose forgets session id.
func (w *Workspace) DropCompose(id string) { composeSessions.Drop(w.Dir(), id) }
