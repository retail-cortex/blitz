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
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/workers"
)

// WorkerRewrite is a worker's instructions as the model rewrote them, the
// permissions they need, and what changed. A model that fails leaves it
// empty with Problem saying why: the user's own instructions stand.
type WorkerRewrite struct {
	Prompt      string
	Permissions []string // "kind:pattern", each valid
	Changes     []string // what changed and why, a line each
	Problem     string
}

// ErrNoInstructions is a rewrite of a worker with no instructions yet.
var ErrNoInstructions = errors.New("write what the worker should do first")

// rewriteInstruction is the brief for rewriting a worker's instructions.
const rewriteInstruction = `You rewrite the instructions of a Blitz worker: a prompt an AI agent carries out on a schedule, unattended. Nobody watches the run or can answer its questions, and it may do only what its permissions allow (anything else is refused). Every model call in the run carries everything fetched so far, so fewer, smaller steps make it faster and cheaper.

Rewrite the instructions so the agent can do the job well in one pass:
- Keep the user's intent, scope and language. Don't add goals they didn't ask for.
- Make them self-contained: say so, and that the agent needn't look up the worker's definition, list folders to orient itself, or open a browser unless the task needs one.
- Name the sources: exact URLs or feeds when the task names a site and you know them for certain; otherwise say what to look for. Fetch each once. Prefer web_fetch to the browser.
- Bound the work: how many items, how far back, what to skip.
- Say exactly what to produce: the file path (relative to the workspace, with any date in the worker's time zone), and its format as a short template.
- Say what to do when a step fails (carry on with what worked, or stop and say so), and to overwrite rather than fail when the output exists.
- End with a short summary: what was written where, and anything it couldn't do.
- Never invent facts, figures or quotes; tell the agent to use only what its sources say.
- Use Markdown headings and short lists; plain words.

Then list the permissions the rewritten instructions need, as narrowly as they allow, each "kind:pattern": web:<host> for each site fetched (a host, not "*"), write:<path glob> for each file written (relative, no leading "./", "*" doesn't cross "/"), delete:<path glob>, shell:<exact command or glob>, mcp:<server>:<tool>. Reading files needs no permission.

Reply with JSON only, no fences: {"instructions": "<the rewritten instructions>", "permissions": ["kind:pattern", ...], "changes": ["<what you changed and why, one short line each, at most five>"]}`

// RewriteWorker has the suggestions model (else the reviewer's, else the
// main one) rewrite a worker's instructions so an unattended run does the
// job well and quickly, and list the permissions they need. The spec is
// the form as the user has it: nothing is saved.
func (w *Workspace) RewriteWorker(ctx context.Context, spec api.WorkerSpec) (WorkerRewrite, error) {
	if strings.TrimSpace(spec.Prompt) == "" {
		return WorkerRewrite{}, ErrNoInstructions
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Worker: %s\n", cmp.Or(spec.Name, "(not named yet)"))
	if spec.Description != "" {
		fmt.Fprintf(&b, "Description: %s\n", spec.Description)
	}
	if spec.Schedule != "" {
		fmt.Fprintf(&b, "Schedule: %s\n", spec.Schedule)
	}
	if spec.Timezone != "" {
		fmt.Fprintf(&b, "Time zone: %s\n", spec.Timezone)
	}
	if len(spec.Permissions) > 0 {
		fmt.Fprintf(&b, "Permissions now: %s\n", strings.Join(spec.Permissions, ", "))
	}
	fmt.Fprintf(&b, "\nThe instructions:\n%s\n", w.secretRedactor().String(spec.Prompt))
	llm, err := w.suggestionsModel(ctx)
	if err != nil {
		return WorkerRewrite{Problem: ModelErrorSummary(err, w.settings())}, nil
	}
	reply, served, usage, err := runtime.Ask(ctx, llm, rewriteInstruction, b.String())
	if err != nil {
		return WorkerRewrite{Problem: ModelErrorSummary(err, w.settings())}, nil
	}
	if usage != nil {
		slog.Info("worker instructions rewritten", "model", served, "input_tokens", usage.PromptTokenCount, "output_tokens", usage.CandidatesTokenCount)
	}
	return parseRewrite(reply), nil
}

// parseRewrite reads the model's JSON reply (fences and text around it
// tolerated). Permissions that don't parse are dropped; a reply without
// instructions is a problem.
func parseRewrite(reply string) WorkerRewrite {
	s := strings.TrimSpace(reply)
	if i, j := strings.Index(s, "{"), strings.LastIndex(s, "}"); i >= 0 && j > i {
		s = s[i : j+1]
	}
	var got struct {
		Instructions string   `json:"instructions"`
		Permissions  []string `json:"permissions"`
		Changes      []string `json:"changes"`
	}
	if err := json.Unmarshal([]byte(s), &got); err != nil || strings.TrimSpace(got.Instructions) == "" {
		return WorkerRewrite{Problem: "the model's reply wasn't a rewrite; try again"}
	}
	out := WorkerRewrite{Prompt: strings.TrimSpace(got.Instructions)}
	seen := map[string]bool{}
	for _, p := range got.Permissions {
		perm, err := workers.ParsePermission(p)
		if err != nil || seen[perm.String()] {
			continue
		}
		seen[perm.String()] = true
		out.Permissions = append(out.Permissions, perm.String())
	}
	for _, c := range got.Changes {
		if c = strings.TrimSpace(c); c != "" && len(out.Changes) < 5 {
			out.Changes = append(out.Changes, c)
		}
	}
	return out
}
