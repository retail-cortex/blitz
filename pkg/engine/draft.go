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
	"fmt"
	"log/slog"
	"strings"

	"github.com/retail-cortex/blitz/pkg/engine/runtime"
)

// ManifestDraft is the model's draft of a manifest field (an agent's
// instructions, a skill's, a script): the text, what changed, or why
// there's none. Nothing is saved: the editor offers it.
type ManifestDraft struct {
	Text    string
	Changes []string
	Problem string
}

// DraftRequest is what a draft is asked for: the kind and field, the
// item's name and description, its text now, what the user asks
// ("" to improve it), and for a script its language.
type DraftRequest struct {
	Kind        string
	Name        string
	Description string
	Current     string
	Request     string
	Language    string
}

// draftBriefs are the model's briefs, by kind (spec_manifest_040 MAN-05).
var draftBriefs = map[string]string{
	"agent":         `You write the instructions (system prompt) of a Blitz agent: an AI coding agent working in a software workspace with tools to read, search and edit files, run commands and browse. Write instructions that make it good at its role: who it is, what it does and doesn't do, how it works step by step, what it hands back. Keep the user's intent, scope and language; don't add goals they didn't ask for. Use Markdown headings and short lists; plain words.`,
	"skill":         `You write the instructions of a Blitz skill (SKILL.md's body): a capability an agent activates when a task calls for it. Say when to use it, the steps, the conventions to follow, and what to hand back; name its references (references/) and scripts (scripts/) where they help. Keep the user's intent, scope and language. Use Markdown headings and short lists; plain words.`,
	"script":        `You write a script a Blitz skill runs (run_skill_script) to do a precise job its instructions describe. It runs in a sandbox: it may read the workspace, write only under .blitz/skill-output, and use the network only if allowed. Take inputs as command-line arguments, print the result to stdout (JSON when it's data), write errors to stderr and exit non-zero on failure. Keep it short, dependency-light, and commented where it isn't obvious. Reply with the code alone as the text.`,
	"rule":          `You write a Blitz rule: one check, in plain English, that a fast model judges before an agent runs (on the prompt) or after (on its answer and changes). Make it specific and judgeable: what passes, what fails, and the reason to give. One rule, a few sentences.`,
	"guardrail":     `You write a Blitz guardrail: a safety check, in plain English, that a fast model judges on a prompt, a tool's result, an answer or an action. Say precisely what to catch and what not to (to avoid false alarms), and what to report. A few sentences.`,
	"eval":          `You write a Blitz eval case: a prompt for an agent, and the output expected from it. Make the prompt self-contained and the expected output precise enough to judge.`,
	"workflow_step": `You write the instructions of one step in a Blitz workflow: what the step's agent does with the previous step's output, and exactly what it hands to the next. Keep it short and specific.`,
}

// draftReply is how every brief is answered.
const draftReply = `

Reply with JSON only, no fences: {"text": "<the text>", "changes": ["<what you wrote or changed and why, one short line each, at most five>"]}`

// DraftManifest has the suggestions model (else the reviewer's, else the
// main one) write or improve a manifest field. A model that fails leaves
// the draft empty with Problem saying why.
func (w *Workspace) DraftManifest(ctx context.Context, r DraftRequest) (ManifestDraft, error) {
	brief, ok := draftBriefs[r.Kind]
	if !ok {
		return ManifestDraft{}, fmt.Errorf("no drafts for %q", r.Kind)
	}
	if strings.TrimSpace(r.Current) == "" && strings.TrimSpace(r.Request) == "" && strings.TrimSpace(r.Description) == "" {
		return ManifestDraft{}, fmt.Errorf("say what it should do first: a description or a request")
	}
	redact := SecretRedactor(w.cfg).String
	var b strings.Builder
	fmt.Fprintf(&b, "Name: %s\n", cmp.Or(r.Name, "(not named yet)"))
	if r.Description != "" {
		fmt.Fprintf(&b, "Description: %s\n", redact(r.Description))
	}
	if r.Language != "" {
		fmt.Fprintf(&b, "Language: %s\n", r.Language)
	}
	if r.Request != "" {
		fmt.Fprintf(&b, "\nWhat the user asks: %s\n", redact(r.Request))
	} else {
		b.WriteString("\nWhat the user asks: improve it.\n")
	}
	if r.Current != "" {
		fmt.Fprintf(&b, "\nThe text now:\n%s\n", redact(r.Current))
	}
	llm, err := w.newModel(ctx, w.cfg, cmp.Or(w.cfg.Suggestions.Model, w.cfg.Permissions.Auto.Model))
	if err != nil {
		return ManifestDraft{Problem: ModelErrorSummary(err, w.cfg)}, nil
	}
	reply, served, usage, err := runtime.Ask(ctx, llm, brief+draftReply, b.String())
	if err != nil {
		return ManifestDraft{Problem: ModelErrorSummary(err, w.cfg)}, nil
	}
	if usage != nil {
		slog.Info("manifest drafted", "kind", r.Kind, "model", served, "input_tokens", usage.PromptTokenCount, "output_tokens", usage.CandidatesTokenCount)
	}
	return parseDraft(reply), nil
}

// parseDraft reads the model's JSON reply (fences and text around it
// tolerated); one without text is a problem.
func parseDraft(reply string) ManifestDraft {
	s := strings.TrimSpace(reply)
	if i, j := strings.Index(s, "{"), strings.LastIndex(s, "}"); i >= 0 && j > i {
		s = s[i : j+1]
	}
	var got struct {
		Text    string   `json:"text"`
		Changes []string `json:"changes"`
	}
	if err := json.Unmarshal([]byte(s), &got); err != nil || strings.TrimSpace(got.Text) == "" {
		return ManifestDraft{Problem: "the model's reply wasn't a draft; try again"}
	}
	out := ManifestDraft{Text: strings.TrimSpace(got.Text)}
	for _, c := range got.Changes {
		if c = strings.TrimSpace(c); c != "" && len(out.Changes) < 5 {
			out.Changes = append(out.Changes, c)
		}
	}
	return out
}
