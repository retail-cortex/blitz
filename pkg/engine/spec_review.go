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
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	specdoc "github.com/retail-cortex/blitz/pkg/engine/specs"
)

// specMaxBytes is the most of a spec file that's read.
const specMaxBytes = 1 << 20

// maxJudgedFindings caps the model's review.
const maxJudgedFindings = 12

// readSpecs reads the specification's Markdown files, in order, with
// their texts by file; edited replaces one file's text (the form's,
// unsaved). No folder: none.
func (w *Workspace) readSpecs(edited map[string]string) ([]specdoc.Spec, map[string]string, error) {
	entries, err := w.files().UserReadDir(filepath.FromSlash(specdoc.Dir))
	if errors.Is(err, fs.ErrNotExist) {
		entries = nil
	} else if err != nil {
		return nil, nil, err
	}
	texts := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(path.Ext(e.Name()), ".md") {
			continue
		}
		data, _, err := w.files().UserReadFile(filepath.FromSlash(path.Join(specdoc.Dir, e.Name())), specMaxBytes)
		if err != nil {
			continue // unreadable (blocked, gone): not listed
		}
		texts[e.Name()] = string(data)
	}
	for f, t := range edited {
		texts[f] = t
	}
	var all []specdoc.Spec
	for f, t := range texts {
		all = append(all, specdoc.Parse(f, t))
	}
	specdoc.Sort(all)
	return all, texts, nil
}

// specFindings are spec's findings from the checks without a model; the
// index's are the specs it doesn't list.
func specFindings(s specdoc.Spec, all []specdoc.Spec, texts map[string]string) []api.SpecFinding {
	var out []api.SpecFinding
	if strings.EqualFold(s.File, specdoc.Index) {
		for _, f := range specdoc.Unlisted(texts[s.File], all) {
			out = append(out, api.SpecFinding{Severity: "warning", Code: "unlisted", Message: fmt.Sprintf("%s isn't in the index", f)})
		}
		return out
	}
	for _, f := range specdoc.Lint(s, all) {
		out = append(out, api.SpecFinding{Line: f.Line, ID: f.ID, Severity: f.Severity, Code: f.Code, Message: f.Message})
	}
	return out
}

// ListSpecs is the project's specification, in order, each with its
// status and what the checks found (spec_057_spec_driven SDD-01).
func (w *Workspace) ListSpecs() ([]api.SpecSummary, error) {
	all, texts, err := w.readSpecs(nil)
	if err != nil {
		return nil, err
	}
	out := make([]api.SpecSummary, 0, len(all))
	for _, s := range all {
		sum := api.SpecSummary{File: s.File, Path: path.Join(specdoc.Dir, s.File), Number: s.Number, Title: s.Title, Status: s.Status, DependsOn: s.DependsOn, Items: len(s.Items), OpenQuestions: len(s.OpenQuestions)}
		for _, f := range specFindings(s, all, texts) {
			switch f.Severity {
			case "error":
				sum.Errors++
			case "warning":
				sum.Warnings++
			}
		}
		out = append(out, sum)
	}
	return out, nil
}

// ErrNotASpec means a path that isn't a Markdown file in the specs folder.
var ErrNotASpec = errors.New("not a spec: a Markdown file in " + specdoc.Dir)

// ReviewSpec reviews the spec at p (its text as edited): the checks
// without a model (SDD-02), and with judge the model's review too (SDD-03),
// which can fail on its own: Problem says why.
func (w *Workspace) ReviewSpec(ctx context.Context, p, text string, judge bool) (api.SpecReview, error) {
	p = path.Clean(filepath.ToSlash(p))
	file := path.Base(p)
	if path.Dir(p) != specdoc.Dir || !strings.EqualFold(path.Ext(file), ".md") {
		return api.SpecReview{}, fmt.Errorf("%w: %s", ErrNotASpec, p)
	}
	all, texts, err := w.readSpecs(map[string]string{file: text})
	if err != nil {
		return api.SpecReview{}, err
	}
	i := slices.IndexFunc(all, func(s specdoc.Spec) bool { return s.File == file })
	s := all[i]
	out := api.SpecReview{Findings: specFindings(s, all, texts)}
	if !judge || strings.EqualFold(file, specdoc.Index) {
		return out, nil
	}
	judged, cost, problem := w.judgeSpec(ctx, s, text, all)
	out.Findings = append(out.Findings, judged...)
	out.CostUSD, out.Problem = cost, problem
	return out, nil
}

// specJudgeBrief is the model's brief for a spec's review.
const specJudgeBrief = `You review one spec of a project's specification, written before the work is done so people and agents can do it and check it (the project may be software, research, data analysis or writing). Find what would make it fail as a spec:
- a requirement that can't be checked as written: say what to measure or how it's tested;
- wording two readers would take differently;
- a contradiction with another spec (name its ID);
- a case a reader would ask about that it doesn't settle (errors, limits, who may, what happens when it fails);
- a requirement that says how instead of what, or a goal no requirement serves.
Don't report what a mechanical check finds (IDs, status lines, numbering, "Done when" missing). At most 12 findings, the most important first; none if it's sound.
Reply with JSON only, no fences: {"findings": [{"line": <the line number shown, 0 for the whole spec>, "id": "<the requirement's ID, or empty>", "severity": "error" or "warning", "message": "<what's wrong and how to fix it, one or two sentences>"}]}`

// judgeSpec has the suggestions model review s (text, numbered by line)
// against the rest of the specification.
func (w *Workspace) judgeSpec(ctx context.Context, s specdoc.Spec, text string, all []specdoc.Spec) ([]api.SpecFinding, float64, string) {
	redact := w.secretRedactor().String
	var b strings.Builder
	fmt.Fprintf(&b, "The spec, %s, by line:\n", s.File)
	for i, line := range strings.Split(text, "\n") {
		fmt.Fprintf(&b, "%d: %s\n", i+1, redact(line))
	}
	b.WriteString("\nThe rest of the specification:\n")
	for _, o := range all {
		if o.File == s.File || o.Number == 0 {
			continue
		}
		fmt.Fprintf(&b, "- %s (%s)", o.File, o.Title)
		if len(o.Kind) > 0 {
			fmt.Fprintf(&b, ", kind %s", strings.Join(o.Kind, ", "))
		}
		b.WriteString("\n")
		for _, it := range o.Items {
			fmt.Fprintf(&b, "  - %s %s\n", it.ID, redact(it.Text))
		}
	}
	llm, err := w.suggestionsModel(ctx)
	if err != nil {
		return nil, 0, ModelErrorSummary(err, w.settings())
	}
	reply, served, usage, err := runtime.Ask(ctx, llm, specJudgeBrief, b.String())
	cost := 0.0
	if usage != nil {
		cost = w.engine.EstimateUsage(served, usage).CostUSD
		slog.Info("spec reviewed", "spec", s.File, "model", served, "input_tokens", usage.PromptTokenCount, "output_tokens", usage.CandidatesTokenCount)
	}
	if err != nil {
		return nil, cost, ModelErrorSummary(err, w.settings())
	}
	found, ok := parseJudged(reply)
	if !ok {
		return nil, cost, "the model's review couldn't be read; try again"
	}
	return found, cost, ""
}

// parseJudged reads the model's findings (fences and text around them
// ignored); ok is false for a reply that isn't any.
func parseJudged(reply string) ([]api.SpecFinding, bool) {
	s := strings.TrimSpace(reply)
	if i, j := strings.Index(s, "{"), strings.LastIndex(s, "}"); i >= 0 && j > i {
		s = s[i : j+1]
	}
	var got struct {
		Findings []struct {
			Line     int    `json:"line"`
			ID       string `json:"id"`
			Severity string `json:"severity"`
			Message  string `json:"message"`
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(s), &got); err != nil {
		return nil, false
	}
	var out []api.SpecFinding
	for _, f := range got.Findings {
		msg := strings.TrimSpace(f.Message)
		if msg == "" || len(out) == maxJudgedFindings {
			continue
		}
		sev := f.Severity
		if sev != "error" {
			sev = "warning"
		}
		out = append(out, api.SpecFinding{Line: max(f.Line, 0), ID: strings.TrimSpace(f.ID), Severity: sev, Code: "judged", Message: msg, Judged: true})
	}
	return out, true
}
