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
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os/user"
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

// ErrSpecNotReady means a spec approved without force has errors or open
// questions.
var ErrSpecNotReady = errors.New("the spec isn't ready to approve")

// Limits on what verification reads.
const (
	// traceFileBytes is the largest file searched for IDs.
	traceFileBytes = 512 << 10
	// traceCommits is how many commits' messages are searched.
	traceCommits = 500
	// judgeContextLines are the lines shown around each piece of evidence.
	judgeContextLines = 4
	// judgePromptBytes caps the evidence sent to the model.
	judgePromptBytes = 96 << 10
	// judgeOutputPerItem is the tokens a verdict takes, for estimates.
	judgeOutputPerItem = 90
)

// specNow is the clock the ledger's times come from (a variable, for tests).
var specNow = time.Now

// readSpecFile reads the saved spec at p (checked to be one): its file
// name and text.
func (w *Workspace) readSpecFile(p string) (string, string, error) {
	p = path.Clean(filepath.ToSlash(p))
	file := path.Base(p)
	if path.Dir(p) != specdoc.Dir || !strings.EqualFold(path.Ext(file), ".md") || strings.EqualFold(file, specdoc.Index) {
		return "", "", fmt.Errorf("%w: %s", ErrNotASpec, p)
	}
	data, _, err := w.files().UserReadFile(filepath.FromSlash(p), specMaxBytes)
	if err != nil {
		return "", "", err
	}
	return file, string(data), nil
}

// readLedger is file's ledger entry (empty when there's none).
func (w *Workspace) readLedger(file string) (specdoc.Ledger, error) {
	data, _, err := w.files().UserReadFile(filepath.FromSlash(specdoc.LedgerPath(file)), specMaxBytes)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return specdoc.Ledger{}, err
	}
	return specdoc.ReadLedger(file, data)
}

// writeWorkspaceFile writes rel (slash-separated), making its folders.
func (w *Workspace) writeWorkspaceFile(ctx context.Context, rel string, data []byte) error {
	dir := filepath.FromSlash(path.Dir(rel))
	if err := w.files().UserMkdir(dir); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return w.files().UserWriteFile(ctx, filepath.FromSlash(rel), data, nil)
}

// requirements are s's requirements as l traces them, then those removed
// since its approval.
func requirements(s specdoc.Spec, l specdoc.Ledger) []api.SpecRequirement {
	changes := map[string]string{}
	for _, c := range l.Changes(s) {
		changes[c.ID] = c.Kind
	}
	var out []api.SpecRequirement
	add := func(id, text string, line int, tr *specdoc.Trace) {
		r := api.SpecRequirement{Spec: s.File, ID: id, Text: text, Line: line, Change: changes[id]}
		if tr != nil && tr.State != "" {
			r.State, r.Judged, r.Reason = tr.State, tr.Judged, tr.Reason
			r.Stale = tr.VerifiedFingerprint != specdoc.Fingerprint(text)
			if tr.VerifiedAt != nil {
				r.VerifiedAt = *tr.VerifiedAt
			}
			for _, e := range tr.Evidence {
				r.Evidence = append(r.Evidence, api.SpecEvidence(e))
			}
		}
		out = append(out, r)
	}
	for _, it := range s.Items {
		add(it.ID, it.Text, it.Line, l.Items[it.ID])
	}
	for id, kind := range changes {
		if kind == "removed" {
			tr := l.Items[id]
			add(id, tr.Text, 0, tr)
		}
	}
	slices.SortStableFunc(out[len(s.Items):], func(a, b api.SpecRequirement) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// TraceSpecs is the requirements of the spec at p ("" for every spec, in
// order) as the ledger traces them (spec_057_spec_driven SDD-10).
func (w *Workspace) TraceSpecs(p string) ([]api.SpecRequirement, error) {
	all, _, err := w.readSpecs(nil)
	if err != nil {
		return nil, err
	}
	only := ""
	if p != "" {
		file, _, err := w.readSpecFile(p)
		if err != nil {
			return nil, err
		}
		only = file
	}
	var out []api.SpecRequirement
	for _, s := range all {
		if s.Number == 0 && strings.EqualFold(s.File, specdoc.Index) || only != "" && s.File != only {
			continue
		}
		l, err := w.readLedger(s.File)
		if err != nil {
			return nil, err
		}
		out = append(out, requirements(s, l)...)
	}
	return out, nil
}

// approver is who approves: git's user.name, else the computer's user.
func (w *Workspace) approver(ctx context.Context) string {
	if out, err := w.git(ctx, nil, "config", "user.name"); err == nil && strings.TrimSpace(string(out)) != "" {
		return strings.TrimSpace(string(out))
	}
	if u, err := user.Current(); err == nil {
		return cmp.Or(u.Name, u.Username)
	}
	return ""
}

// ApproveSpec approves the saved spec at p (SDD-11): its Status: line says
// approved, its text is kept as the baseline, and the ledger records each
// requirement's fingerprint; when it was approved before with another
// text, a diff of the change is kept in the ledger's diffs. Without force,
// a spec with errors or open questions isn't approved (ErrSpecNotReady).
func (w *Workspace) ApproveSpec(ctx context.Context, p string, force bool) (api.SpecApproved, error) {
	file, text, err := w.readSpecFile(p)
	if err != nil {
		return api.SpecApproved{}, err
	}
	all, _, err := w.readSpecs(nil)
	if err != nil {
		return api.SpecApproved{}, err
	}
	s := specdoc.Parse(file, text)
	if !force {
		var why []string
		for _, f := range specdoc.Lint(s, all) {
			if f.Severity == "error" {
				why = append(why, f.Message)
			}
		}
		if n := len(s.OpenQuestions); n > 0 {
			why = append(why, fmt.Sprintf("%d open question(s)", n))
		}
		if len(why) > 0 {
			return api.SpecApproved{}, fmt.Errorf("%w: %s", ErrSpecNotReady, strings.Join(why, "; "))
		}
	}
	approved := text
	if s.Status != "approved" {
		approved = specdoc.SetStatus(text, "approved")
		rel := filepath.FromSlash(path.Join(specdoc.Dir, file))
		err := w.files().UserWriteFile(ctx, rel, []byte(approved), func(current []byte, _ bool) error {
			if string(current) != text {
				return &FileChangedError{Path: path.Join(specdoc.Dir, file)}
			}
			return nil
		})
		if err != nil {
			return api.SpecApproved{}, err
		}
	}
	at := specNow()
	out := api.SpecApproved{Text: approved}
	if old, _, err := w.files().UserReadFile(filepath.FromSlash(specdoc.BaselinePath(file)), specMaxBytes); err == nil && string(old) != approved {
		out.Diff = specdoc.DiffPath(file, at)
		if err := w.writeWorkspaceFile(ctx, out.Diff, []byte(specdoc.UnifiedDiff(path.Join(specdoc.Dir, file), string(old), approved))); err != nil {
			return api.SpecApproved{}, err
		}
	}
	l, err := w.readLedger(file)
	if err != nil {
		return api.SpecApproved{}, err
	}
	l.Approve(specdoc.Parse(file, approved), approved, at, w.approver(ctx))
	if err := w.writeWorkspaceFile(ctx, specdoc.BaselinePath(file), []byte(approved)); err != nil {
		return api.SpecApproved{}, err
	}
	if err := w.writeWorkspaceFile(ctx, specdoc.LedgerPath(file), l.Bytes()); err != nil {
		return api.SpecApproved{}, err
	}
	return out, nil
}

// findEvidence is SDD-12's pass without a model: each of ids in the
// workspace's files (not the specs or the ledger) and recent commit
// messages.
func (w *Workspace) findEvidence(ctx context.Context, ids []string) map[string][]specdoc.Evidence {
	f := specdoc.NewFinder(ids)
	if f == nil {
		return nil
	}
	paths, err := w.listFiles(ctx, true)
	if err != nil {
		slog.Warn("spec verification: listing files", "err", err)
	}
	for _, p := range paths {
		if ctx.Err() != nil {
			break
		}
		if strings.HasPrefix(p, specdoc.Dir+"/") || strings.HasPrefix(p, specdoc.LedgerDir+"/") {
			continue
		}
		data, _, err := w.files().UserReadFile(filepath.FromSlash(p), traceFileBytes)
		if err != nil || bytes.IndexByte(data[:min(len(data), 8<<10)], 0) >= 0 {
			continue // unreadable, too large or binary
		}
		f.File(p, string(data))
	}
	if out, err := w.git(ctx, nil, "log", "-n", fmt.Sprint(traceCommits), "--format=%h%x1f%s%x1f%B%x1e"); err == nil {
		for rec := range strings.SplitSeq(string(out), "\x1e") {
			parts := strings.SplitN(strings.TrimLeft(rec, "\n"), "\x1f", 3)
			if len(parts) == 3 {
				f.Commit(parts[0], parts[1], parts[2])
			}
		}
	}
	return f.Found
}

// itemIDs are s's requirements' IDs.
func itemIDs(s specdoc.Spec) []string {
	ids := make([]string, len(s.Items))
	for i, it := range s.Items {
		ids[i] = it.ID
	}
	return ids
}

// verdict is the model's judgement of one requirement.
type verdict struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Reason string `json:"reason"`
}

// specVerifyBrief is the model's brief for verification.
const specVerifyBrief = `You verify whether a project's work implements one spec's requirements, from the evidence found for each: lines of the project's files and commit messages that name the requirement's ID, and passages a search found for it. Judge each requirement from the evidence alone:
- implemented: the evidence shows it done as written;
- partial: some of it is done, or done differently in a way that matters;
- missing: nothing shows it done (no evidence, or only plans and mentions);
- contradicted: the work does something the requirement rules out.
Give one short reason each, naming the file and line it rests on. Don't guess beyond the evidence: say missing when it doesn't show the work.
Reply with JSON only, no fences: {"requirements": [{"id": "<ID>", "state": "implemented|partial|missing|contradicted", "reason": "<one sentence>"}]}`

// judgePrompt is the evidence for s's requirements, as the model reads it:
// each piece with the lines around it, and for a requirement nothing names,
// what the search index finds for its text.
func (w *Workspace) judgePrompt(ctx context.Context, s specdoc.Spec, found map[string][]specdoc.Evidence) string {
	redact := w.secretRedactor().String
	var b strings.Builder
	fmt.Fprintf(&b, "The spec: %s (%s).\n", s.File, s.Title)
	budget := judgePromptBytes / max(len(s.Items), 1)
	for _, it := range s.Items {
		var e strings.Builder
		fmt.Fprintf(&e, "\n## %s: %s\n", it.ID, redact(it.Text))
		ev := found[it.ID]
		if len(ev) == 0 {
			e.WriteString("No file or commit names it.\n")
			if res, err := w.Search(ctx, api.SearchQuery{Text: it.Text, Limit: 3}); err == nil {
				for _, h := range res.Hits {
					fmt.Fprintf(&e, "Search found %s:%d:\n%s\n", h.Ref, h.Line, redact(h.Snippet))
				}
			}
		}
		for _, x := range ev {
			if x.Kind == "commit" {
				fmt.Fprintf(&e, "Commit %s: %s\n", x.Path, redact(x.Text))
				continue
			}
			fmt.Fprintf(&e, "%s (%s), line %d:\n%s\n", x.Path, x.Kind, x.Line, redact(w.linesAround(x.Path, x.Line)))
		}
		piece := e.String()
		if len(piece) > budget {
			piece = piece[:budget] + "\n…\n"
		}
		b.WriteString(piece)
	}
	return b.String()
}

// linesAround is p's lines around line, numbered.
func (w *Workspace) linesAround(p string, line int) string {
	data, _, err := w.files().UserReadFile(filepath.FromSlash(p), traceFileBytes)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	var b strings.Builder
	for i := max(1, line-judgeContextLines); i <= min(len(lines), line+judgeContextLines); i++ {
		fmt.Fprintf(&b, "%d: %s\n", i, lines[i-1])
	}
	return b.String()
}

// VerifySpec verifies the saved spec at p (SDD-12): each requirement's ID
// found in the work, traced or untraced; with judge, the suggestions
// model's verdict from that evidence (implemented, partial, missing,
// contradicted); with estimate, only what judging would cost, nothing
// written. The ledger keeps the result.
func (w *Workspace) VerifySpec(ctx context.Context, p string, judge, estimate bool) (api.SpecVerification, error) {
	file, text, err := w.readSpecFile(p)
	if err != nil {
		return api.SpecVerification{}, err
	}
	s := specdoc.Parse(file, text)
	found := w.findEvidence(ctx, itemIDs(s))
	l, err := w.readLedger(file)
	if err != nil {
		return api.SpecVerification{}, err
	}
	var out api.SpecVerification
	verdicts := map[string]verdict{}
	if judge {
		llm, err := w.suggestionsModel(ctx)
		if err != nil {
			out.Problem = ModelErrorSummary(err, w.settings())
		} else {
			prompt := w.judgePrompt(ctx, s, found)
			if estimate {
				u := w.engine.EstimateUsage(llm.Name(), &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: int32((len(specVerifyBrief) + len(prompt)) / 4), CandidatesTokenCount: int32(judgeOutputPerItem * len(s.Items))})
				out.EstimatedUSD, out.Priced = u.CostUSD, u.Priced
				out.Requirements = requirements(s, l)
				return out, nil
			}
			reply, served, usage, err := runtime.Ask(ctx, llm, specVerifyBrief, prompt)
			if usage != nil {
				out.CostUSD = w.engine.EstimateUsage(served, usage).CostUSD
				slog.Info("spec verified", "spec", file, "model", served, "input_tokens", usage.PromptTokenCount, "output_tokens", usage.CandidatesTokenCount)
			}
			switch v, ok := parseVerdicts(reply); {
			case err != nil:
				out.Problem = ModelErrorSummary(err, w.settings())
			case !ok:
				out.Problem = "the model's verification couldn't be read; try again"
			default:
				verdicts = v
			}
		}
	}
	at := specNow()
	was := map[string]string{}
	for id, tr := range l.Items {
		was[id] = tr.State
	}
	for _, it := range s.Items {
		tr := l.Items[it.ID]
		if tr == nil {
			tr = &specdoc.Trace{Text: it.Text, Fingerprint: specdoc.Fingerprint(it.Text)}
			l.Items[it.ID] = tr
		}
		ev, fp := found[it.ID], specdoc.Fingerprint(it.Text)
		state := specdoc.StateOf(ev)
		switch v, ok := verdicts[it.ID]; {
		case ok:
			tr.State, tr.Judged, tr.Reason = v.State, true, v.Reason
		case tr.Judged && tr.VerifiedFingerprint == fp && state == specdoc.Traced && !judge:
			// The model's verdict stands while the requirement is unchanged and still found.
		default:
			tr.State, tr.Judged, tr.Reason = state, false, ""
		}
		tr.Evidence, tr.VerifiedAt, tr.VerifiedFingerprint = ev, &at, fp
	}
	if err := w.writeWorkspaceFile(ctx, specdoc.LedgerPath(file), l.Bytes()); err != nil {
		return api.SpecVerification{}, err
	}
	out.Requirements = requirements(s, l)
	if l.Approved != nil {
		for _, r := range out.Requirements {
			if why := specdoc.Drift(r.Change, was[r.ID], r.State, r.Judged); why != "" {
				out.Drift = append(out.Drift, api.SpecDrift{ID: r.ID, Why: why})
			}
		}
	}
	return out, nil
}

// PlanSpec is how the saved spec at p would be implemented (SDD-30): its
// requirements not implemented yet (by the ledger's last verification),
// the dependencies not done it waits for, and the prompt for an agent:
// tests first from Done when, each requirement's ID cited where it's done.
func (w *Workspace) PlanSpec(p string) (api.SpecPlan, error) {
	file, text, err := w.readSpecFile(p)
	if err != nil {
		return api.SpecPlan{}, err
	}
	all, _, err := w.readSpecs(nil)
	if err != nil {
		return api.SpecPlan{}, err
	}
	s := specdoc.Parse(file, text)
	l, err := w.readLedger(file)
	if err != nil {
		return api.SpecPlan{}, err
	}
	var out api.SpecPlan
	done := w.doneSpecs(all)
	for _, d := range s.DependsOn {
		if d != s.Number && !done[d] {
			out.BlockedBy = append(out.BlockedBy, d)
		}
	}
	pending := specdoc.Pending(s, l)
	byID := map[string]api.SpecRequirement{}
	for _, r := range requirements(s, l) {
		byID[r.ID] = r
	}
	for _, it := range pending {
		out.Pending = append(out.Pending, byID[it.ID])
	}
	if len(pending) == 0 {
		return out, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Implement %s (.agents/specs/%s). Read it first, with the specs it depends on and .agents/AGENTS.md (how to build and test here).\n\nThe requirements to do, in order:\n", s.Title, file)
	for _, it := range pending {
		fmt.Fprintf(&b, "- %s: %s\n", it.ID, it.Text)
	}
	if dw := specdoc.DoneWhen(text); dw != "" {
		fmt.Fprintf(&b, "\nDone when:\n%s\n", dw)
	}
	b.WriteString("\nWork tests first: for each requirement, write or update the test that proves it, see it fail, then make it pass. Cite each requirement's ID where it's done: in a comment at the code that does it, in the test that proves it, and in the commit message (Implements: ID). Run the project's tests before you say it's done.")
	if len(out.BlockedBy) > 0 {
		nums := make([]string, len(out.BlockedBy))
		for i, d := range out.BlockedBy {
			nums[i] = fmt.Sprintf("%03d", d)
		}
		fmt.Fprintf(&b, " It depends on %s, which isn't done yet: check what it needs is there, and say so if it isn't.", strings.Join(nums, ", "))
	}
	out.Prompt = b.String()
	return out, nil
}

// doneSpecs are the numbers of the specs that are done (specdoc.Done).
func (w *Workspace) doneSpecs(all []specdoc.Spec) map[int]bool {
	done := map[int]bool{}
	for _, s := range all {
		if s.Number == 0 {
			continue
		}
		if l, err := w.readLedger(s.File); err == nil && specdoc.Done(s, l) {
			done[s.Number] = true
		}
	}
	return done
}

// parseVerdicts reads the model's verdicts by ID; ok is false for a reply
// that isn't any. An unknown state is dropped.
func parseVerdicts(reply string) (map[string]verdict, bool) {
	s := strings.TrimSpace(reply)
	if i, j := strings.Index(s, "{"), strings.LastIndex(s, "}"); i >= 0 && j > i {
		s = s[i : j+1]
	}
	var got struct {
		Requirements []verdict `json:"requirements"`
	}
	if err := json.Unmarshal([]byte(s), &got); err != nil {
		return nil, false
	}
	out := map[string]verdict{}
	for _, v := range got.Requirements {
		v.State = strings.ToLower(strings.TrimSpace(v.State))
		if slices.Contains([]string{specdoc.Implemented, specdoc.Partial, specdoc.Missing, specdoc.Contradicted}, v.State) {
			v.Reason = strings.TrimSpace(v.Reason)
			out[strings.TrimSpace(v.ID)] = v
		}
	}
	return out, true
}

// specCounts adds s's trace to its summary: approved, traced (named by the
// work, or judged implemented), implemented, changed since approval.
func (w *Workspace) specCounts(sum *api.SpecSummary, s specdoc.Spec) {
	l, err := w.readLedger(s.File)
	if err != nil {
		return
	}
	sum.Approved = l.Approved != nil
	for _, r := range requirements(s, l) {
		if r.Change != "" {
			sum.Changed++
		}
		if r.Line == 0 {
			continue
		}
		if r.State == specdoc.Traced || r.State == specdoc.Implemented {
			sum.Traced++
		}
		if r.State == specdoc.Implemented {
			sum.Implemented++
		}
	}
}
