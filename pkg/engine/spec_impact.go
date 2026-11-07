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
	"fmt"
	"log/slog"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	specdoc "github.com/retail-cortex/blitz/pkg/engine/specs"
	"google.golang.org/genai"
)

// Limits on an impact scan.
const (
	// impactSymbols is how many declarations are followed.
	impactSymbols = 12
	// impactUses is how many uses of one symbol are kept.
	impactUses = 30
	// impactPlaces is how many places a report lists.
	impactPlaces = 120
	// impactReferenceTimeout bounds a language server's answer.
	impactReferenceTimeout = 20 * time.Second
	// impactOutput is the tokens a report takes, for estimates.
	impactOutput = 1200
)

// specChanges are s's changes since its approval, with their texts.
func specChanges(s specdoc.Spec, l specdoc.Ledger) []api.SpecChange {
	texts := map[string]string{}
	for _, it := range s.Items {
		texts[it.ID] = it.Text
	}
	var out []api.SpecChange
	for _, c := range l.Changes(s) {
		ch := api.SpecChange{ID: c.ID, Kind: c.Kind, New: texts[c.ID]}
		if tr := l.Items[c.ID]; tr != nil {
			ch.Old = tr.Text
		}
		out = append(out, ch)
	}
	return out
}

// RecordSpecChange records the saved change to the approved spec at p
// (spec_057_spec_driven SDD-20): its Status: line says changed, and a diff
// from the approved text is kept in the ledger's diffs (not again when it's
// the same as the last one kept). A spec never approved, or unchanged since,
// records nothing.
func (w *Workspace) RecordSpecChange(ctx context.Context, p string) (api.SpecRecorded, error) {
	file, text, err := w.readSpecFile(p)
	if err != nil {
		return api.SpecRecorded{}, err
	}
	l, err := w.readLedger(file)
	if err != nil || l.Approved == nil {
		return api.SpecRecorded{}, err
	}
	s := specdoc.Parse(file, text)
	out := api.SpecRecorded{Changes: specChanges(s, l)}
	if len(out.Changes) == 0 {
		return out, nil
	}
	if s.Status != "changed" {
		changed := specdoc.SetStatus(text, "changed")
		rel := filepath.FromSlash(path.Join(specdoc.Dir, file))
		err := w.files().UserWriteFile(ctx, rel, []byte(changed), func(current []byte, _ bool) error {
			if string(current) != text {
				return &FileChangedError{Path: path.Join(specdoc.Dir, file)}
			}
			return nil
		})
		if err != nil {
			return api.SpecRecorded{}, err
		}
		out.Text, text = changed, changed
	}
	base, _, err := w.files().UserReadFile(filepath.FromSlash(specdoc.BaselinePath(file)), specMaxBytes)
	if err != nil {
		return out, nil // no baseline to diff against (approved by hand)
	}
	diff := specdoc.UnifiedDiff(path.Join(specdoc.Dir, file), string(base), text)
	if last := w.lastSpecDiff(file); last == diff {
		return out, nil
	}
	out.Diff = specdoc.DiffPath(file, specNow())
	if err := w.writeWorkspaceFile(ctx, out.Diff, []byte(diff)); err != nil {
		return api.SpecRecorded{}, err
	}
	return out, nil
}

// lastSpecDiff is the latest diff kept for file ("" for none).
func (w *Workspace) lastSpecDiff(file string) string {
	entries, err := w.files().UserReadDir(filepath.FromSlash(path.Join(specdoc.LedgerDir, "diffs")))
	if err != nil {
		return ""
	}
	suffix := "-" + strings.TrimSuffix(file, path.Ext(file)) + ".diff"
	last := ""
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), suffix) && e.Name() > last {
			last = e.Name()
		}
	}
	if last == "" {
		return ""
	}
	data, _, err := w.files().UserReadFile(filepath.FromSlash(path.Join(specdoc.LedgerDir, "diffs", last)), specMaxBytes)
	if err != nil {
		return ""
	}
	return string(data)
}

// impactScan is what an impact scan finds without a model.
type impactScan struct {
	places  []api.SpecPlace
	symbols []api.SpecSymbol
	seen    map[string]bool
}

func (sc *impactScan) add(e specdoc.Evidence, why string) {
	key := fmt.Sprintf("%s:%d", e.Path, e.Line)
	if sc.seen[key] || len(sc.places) >= impactPlaces || strings.HasPrefix(e.Path, specdoc.Dir+"/") || strings.HasPrefix(e.Path, specdoc.LedgerDir+"/") {
		return
	}
	sc.seen[key] = true
	sc.places = append(sc.places, api.SpecPlace{Path: e.Path, Line: e.Line, Kind: e.Kind, Text: e.Text, Why: why})
}

// symbolUses are the uses of sym: its language server's references, else
// a whole-word search of the workspace's files (paths).
func (w *Workspace) symbolUses(ctx context.Context, sym specdoc.Symbol, paths []string) []specdoc.Evidence {
	var out []specdoc.Evidence
	if m, err := w.languages(); err == nil {
		rctx, cancel := context.WithTimeout(ctx, impactReferenceTimeout)
		locs, err := m.References(rctx, filepath.Join(w.Dir(), filepath.FromSlash(sym.Path)), sym.Line, sym.Column)
		cancel()
		if err == nil && len(locs) > 0 {
			for _, l := range locs {
				p := filepath.ToSlash(l.Path)
				if (p == sym.Path && l.Line == sym.Line) || filepath.IsAbs(l.Path) {
					continue // its declaration, or outside the workspace
				}
				out = append(out, specdoc.Evidence{Path: p, Line: l.Line, Kind: specdoc.EvidenceKind(p), Text: strings.TrimSpace(l.Text)})
				if len(out) == impactUses {
					break
				}
			}
			return out
		}
	}
	for _, p := range paths {
		if ctx.Err() != nil || len(out) >= impactUses {
			break
		}
		if strings.HasPrefix(p, specdoc.Dir+"/") || strings.HasPrefix(p, specdoc.LedgerDir+"/") {
			continue
		}
		data, _, err := w.files().UserReadFile(filepath.FromSlash(p), traceFileBytes)
		if err != nil || !strings.Contains(string(data), sym.Name) {
			continue
		}
		decl := 0
		if p == sym.Path {
			decl = sym.Line
		}
		out = append(out, specdoc.Uses(p, string(data), sym.Name, decl)...)
	}
	return out[:min(len(out), impactUses)]
}

// scanImpact is SDD-20's scan without a model: from where the changed and
// removed requirements were found (the ledger's evidence and their IDs in
// the work now), the declarations that holds them and those
// declarations' uses; and for the added and changed, what the search index
// finds for their new text.
func (w *Workspace) scanImpact(ctx context.Context, l specdoc.Ledger, changes []api.SpecChange) impactScan {
	sc := impactScan{seen: map[string]bool{}}
	var ids []string
	for _, c := range changes {
		if c.Kind != "added" {
			ids = append(ids, c.ID)
		}
	}
	found := w.findEvidence(ctx, ids)
	var seeds []specdoc.Evidence
	for _, id := range ids {
		var ev []specdoc.Evidence
		if tr := l.Items[id]; tr != nil {
			ev = append(ev, tr.Evidence...)
		}
		ev = append(ev, found[id]...)
		for _, e := range ev {
			sc.add(e, id)
			if e.Kind == "code" || e.Kind == "test" {
				seeds = append(seeds, e)
			}
		}
	}
	paths, _ := w.listFiles(ctx, true)
	texts := map[string]string{}
	for _, e := range seeds {
		if len(sc.symbols) >= impactSymbols {
			break
		}
		text, ok := texts[e.Path]
		if !ok {
			data, _, err := w.files().UserReadFile(filepath.FromSlash(e.Path), traceFileBytes)
			if err != nil {
				continue
			}
			text = string(data)
			texts[e.Path] = text
		}
		sym, ok := specdoc.Declaration(e.Path, text, e.Line)
		if !ok || slices.ContainsFunc(sc.symbols, func(s api.SpecSymbol) bool { return s.Name == sym.Name && s.Path == sym.Path }) {
			continue
		}
		uses := w.symbolUses(ctx, sym, paths)
		sc.symbols = append(sc.symbols, api.SpecSymbol{Name: sym.Name, Path: sym.Path, Line: sym.Line, Uses: len(uses)})
		for _, u := range uses {
			sc.add(u, sym.Name)
		}
	}
	for _, c := range changes {
		if c.Kind == "removed" || c.New == "" {
			continue
		}
		res, err := w.Search(ctx, api.SearchQuery{Text: c.New, Limit: 4, Sources: []string{api.SearchFiles}})
		if err != nil {
			continue
		}
		for _, h := range res.Hits {
			sc.add(specdoc.Evidence{Path: h.Ref, Line: h.Line, Kind: specdoc.EvidenceKind(h.Ref), Text: strings.TrimSpace(h.Snippet)}, "search: "+c.ID)
		}
	}
	return sc
}

// specImpactBrief is the model's brief for an impact report.
const specImpactBrief = `A project's spec changed after its work was done. You get each requirement that changed (its text before and after; added and removed ones too), the places in the work it reaches (where the requirement is named, the declarations that hold that work and their uses, and passages a search found), and the rest of the specification. Write what the change means for the work:
- summary: two or three sentences: what has to change, and the risk;
- steps: the ordered steps to bring the work in line, tests first (update or add the tests that prove the new requirement), then the code, then documentation; each with a short title, what to do in a sentence or two (naming functions and files), and the files it touches;
- conflicts: other specs' requirements the change contradicts, by ID, and why.
Only what the evidence supports; say when the evidence doesn't show where something is done.
Reply with JSON only, no fences: {"summary": "…", "steps": [{"title": "…", "detail": "…", "files": ["path"]}], "conflicts": [{"id": "…", "why": "…"}]}`

// impactPrompt is what the model reads for an impact report.
func (w *Workspace) impactPrompt(file string, sc impactScan, changes []api.SpecChange, all []specdoc.Spec) string {
	redact := w.secretRedactor().String
	var b strings.Builder
	fmt.Fprintf(&b, "The spec: %s.\n\nWhat changed:\n", file)
	for _, c := range changes {
		switch c.Kind {
		case "added":
			fmt.Fprintf(&b, "- %s added: %s\n", c.ID, redact(c.New))
		case "removed":
			fmt.Fprintf(&b, "- %s removed (was: %s)\n", c.ID, redact(c.Old))
		default:
			fmt.Fprintf(&b, "- %s changed from: %s\n  to: %s\n", c.ID, redact(c.Old), redact(c.New))
		}
	}
	if len(sc.symbols) > 0 {
		b.WriteString("\nDeclarations that hold the work:\n")
		for _, s := range sc.symbols {
			fmt.Fprintf(&b, "- %s in %s:%d (%d uses found)\n", s.Name, s.Path, s.Line, s.Uses)
		}
	}
	b.WriteString("\nPlaces it reaches (file:line, kind, why, the line):\n")
	if len(sc.places) == 0 {
		b.WriteString("None found: nothing in the work names these requirements.\n")
	}
	for _, p := range sc.places {
		if p.Kind == "commit" {
			fmt.Fprintf(&b, "- commit %s (%s): %s\n", p.Path, p.Why, redact(p.Text))
			continue
		}
		fmt.Fprintf(&b, "- %s:%d (%s, %s): %s\n", p.Path, p.Line, p.Kind, p.Why, redact(p.Text))
	}
	b.WriteString("\nThe rest of the specification:\n")
	for _, o := range all {
		if o.File == file || o.Number == 0 {
			continue
		}
		for _, it := range o.Items {
			fmt.Fprintf(&b, "- %s (%s) %s\n", it.ID, o.File, redact(it.Text))
		}
	}
	out := b.String()
	if len(out) > judgePromptBytes {
		out = out[:judgePromptBytes] + "\n…\n"
	}
	return out
}

// ImpactSpec scans what the saved spec at p's changes since its approval
// reach in the work (SDD-20) without a model; with judge, the suggestions
// model writes the report (a summary, the steps to fix it, the conflicts:
// SDD-21), kept in the ledger's impact folder; with estimate, only what
// that would cost, nothing written.
func (w *Workspace) ImpactSpec(ctx context.Context, p string, judge, estimate bool) (api.SpecImpact, error) {
	file, text, err := w.readSpecFile(p)
	if err != nil {
		return api.SpecImpact{}, err
	}
	l, err := w.readLedger(file)
	if err != nil {
		return api.SpecImpact{}, err
	}
	s := specdoc.Parse(file, text)
	out := api.SpecImpact{Changes: specChanges(s, l)}
	if len(out.Changes) == 0 {
		return out, nil
	}
	sc := w.scanImpact(ctx, l, out.Changes)
	out.Symbols, out.Places = sc.symbols, sc.places
	if !judge {
		return out, nil
	}
	all, _, err := w.readSpecs(nil)
	if err != nil {
		return api.SpecImpact{}, err
	}
	llm, err := w.suggestionsModel(ctx)
	if err != nil {
		out.Problem = ModelErrorSummary(err, w.settings())
		return out, nil
	}
	prompt := w.impactPrompt(file, sc, out.Changes, all)
	if estimate {
		u := w.engine.EstimateUsage(llm.Name(), &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: int32((len(specImpactBrief) + len(prompt)) / 4), CandidatesTokenCount: impactOutput})
		out.EstimatedUSD, out.Priced = u.CostUSD, u.Priced
		return out, nil
	}
	reply, served, usage, err := runtime.Ask(ctx, llm, specImpactBrief, prompt)
	if usage != nil {
		out.CostUSD = w.engine.EstimateUsage(served, usage).CostUSD
		slog.Info("spec impact", "spec", file, "model", served, "input_tokens", usage.PromptTokenCount, "output_tokens", usage.CandidatesTokenCount)
	}
	if err != nil {
		out.Problem = ModelErrorSummary(err, w.settings())
		return out, nil
	}
	if !parseImpact(reply, &out) {
		out.Problem = "the model's report couldn't be read; try again"
		return out, nil
	}
	out.Report = path.Join(specdoc.LedgerDir, "impact", specNow().UTC().Format("20060102-150405")+"-"+strings.TrimSuffix(file, path.Ext(file))+".md")
	if err := w.writeWorkspaceFile(ctx, out.Report, []byte(impactReport(file, out))); err != nil {
		return api.SpecImpact{}, err
	}
	return out, nil
}

// parseImpact reads the model's report into out; false for a reply that
// isn't one.
func parseImpact(reply string, out *api.SpecImpact) bool {
	s := strings.TrimSpace(reply)
	if i, j := strings.Index(s, "{"), strings.LastIndex(s, "}"); i >= 0 && j > i {
		s = s[i : j+1]
	}
	var got struct {
		Summary string `json:"summary"`
		Steps   []struct {
			Title  string   `json:"title"`
			Detail string   `json:"detail"`
			Files  []string `json:"files"`
		} `json:"steps"`
		Conflicts []struct {
			ID  string `json:"id"`
			Why string `json:"why"`
		} `json:"conflicts"`
	}
	if err := json.Unmarshal([]byte(s), &got); err != nil || (strings.TrimSpace(got.Summary) == "" && len(got.Steps) == 0) {
		return false
	}
	out.Summary = strings.TrimSpace(got.Summary)
	for _, st := range got.Steps {
		if strings.TrimSpace(st.Title) != "" {
			out.Steps = append(out.Steps, api.SpecStep{Title: strings.TrimSpace(st.Title), Detail: strings.TrimSpace(st.Detail), Files: st.Files})
		}
	}
	for _, c := range got.Conflicts {
		if strings.TrimSpace(c.Why) != "" {
			out.Conflicts = append(out.Conflicts, api.SpecConflict{ID: strings.TrimSpace(c.ID), Why: strings.TrimSpace(c.Why)})
		}
	}
	return true
}

// impactReport is the report as kept: Markdown an agent (or a person) can
// follow, in English like the ledger's other files.
func impactReport(file string, im api.SpecImpact) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Impact of the changes to %s\n\n%s\n\n## What changed\n\n", file, im.Summary)
	for _, c := range im.Changes {
		switch c.Kind {
		case "added":
			fmt.Fprintf(&b, "- **%s** added: %s\n", c.ID, c.New)
		case "removed":
			fmt.Fprintf(&b, "- **%s** removed (was: %s)\n", c.ID, c.Old)
		default:
			fmt.Fprintf(&b, "- **%s** changed from \"%s\" to \"%s\"\n", c.ID, c.Old, c.New)
		}
	}
	b.WriteString("\n## Steps\n\n")
	for i, st := range im.Steps {
		fmt.Fprintf(&b, "%d. **%s** %s", i+1, st.Title, st.Detail)
		if len(st.Files) > 0 {
			fmt.Fprintf(&b, " (%s)", strings.Join(st.Files, ", "))
		}
		b.WriteString("\n")
	}
	if len(im.Conflicts) > 0 {
		b.WriteString("\n## Conflicts with other specs\n\n")
		for _, c := range im.Conflicts {
			fmt.Fprintf(&b, "- **%s**: %s\n", c.ID, c.Why)
		}
	}
	if len(im.Places) > 0 {
		b.WriteString("\n## Places it reaches\n\n")
		for _, p := range im.Places {
			if p.Kind == "commit" {
				fmt.Fprintf(&b, "- commit %s (%s)\n", p.Path, p.Why)
				continue
			}
			fmt.Fprintf(&b, "- %s:%d (%s, %s)\n", p.Path, p.Line, p.Kind, p.Why)
		}
	}
	b.WriteString("\nWhen it's done: verify the spec (Studio › Specs, or `blitz spec verify`) and approve it again.\n")
	return b.String()
}
