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

package api

import "time"

// SpecSummary is one file of the project's specification (.agents/specs),
// as read: what spec_057_spec_driven's list shows (SDD-01).
type SpecSummary struct {
	// File is its name (001-overview.md); Path is it in the workspace.
	File string
	Path string
	// Number is its place in the order (0 for the index or an unnumbered file).
	Number int
	Title  string
	// Status is its Status: line ("" when there's none).
	Status    string
	DependsOn []int
	// Items counts its requirements with IDs.
	Items int
	// Errors and Warnings count what the checks without a model found.
	Errors   int
	Warnings int
	// OpenQuestions counts its open questions.
	OpenQuestions int
	// Approved reports that the ledger has its approval (SDD-11).
	Approved bool
	// Traced counts its requirements the work names (or the model found
	// implemented); Implemented those the model judged so; Changed those
	// added or changed since it was approved (SDD-10, SDD-12).
	Traced      int
	Implemented int
	Changed     int
}

// SpecFinding is something a review found in a spec.
type SpecFinding struct {
	// Line is where (1-based; 0 for the whole spec).
	Line int
	// ID is the requirement it's about, if one.
	ID string
	// Severity is "error", "warning" or "info".
	Severity string
	// Code names the check (no-title, vague, …; "judged" for the model's).
	Code    string
	Message string
	// Judged marks the model's findings (SDD-03), not the checks'.
	Judged bool
}

// SpecReview is a spec's review: the checks' findings, and the model's
// when asked, with what it cost. Problem says why the model's review
// didn't come (the checks' findings are there all the same).
type SpecReview struct {
	Findings []SpecFinding
	CostUSD  float64
	Problem  string
}

// SpecRequirement is one requirement as traced (SDD-10, SDD-12): where it
// is in its spec, how it differs from what was approved, and its last
// verification.
type SpecRequirement struct {
	// Spec is its spec's file; Line its line there (0 when removed).
	Spec string
	ID   string
	Text string
	Line int
	// Change is "added" or "changed" since the spec was approved, or
	// "removed" (still in the approval, gone from the spec); "" otherwise.
	Change string
	// State is its last verification's: traced or untraced (the check
	// without a model), implemented, partial, missing or contradicted (the
	// model's, Judged); "" when never verified.
	State  string
	Judged bool
	Reason string
	// Stale reports that its text changed since it was verified.
	Stale      bool
	VerifiedAt time.Time
	Evidence   []SpecEvidence
}

// SpecEvidence is where a requirement was found: a file's line (code,
// test or doc) or a commit (Path is its hash).
type SpecEvidence struct {
	Path string
	Line int
	Kind string
	Text string
}

// SpecApproved is an approved spec: its text as written (Status:
// approved) and the diff kept of what changed since its last approval
// ("" when there was none).
type SpecApproved struct {
	Text string
	Diff string
}

// SpecVerification is a spec's verification: its requirements as found;
// with the model, what it cost, or (estimate) what it would cost and
// whether the model has a price.
type SpecVerification struct {
	Requirements []SpecRequirement
	CostUSD      float64
	EstimatedUSD float64
	Priced       bool
	Problem      string
}

// SpecChange is a requirement that differs from its spec's approval: its
// kind (added, changed, removed) and its text then and now.
type SpecChange struct {
	ID   string
	Kind string
	Old  string
	New  string
}

// SpecRecorded is a saved change to an approved spec (SDD-20): what
// changed, the diff kept in the ledger, and the spec's text now that it
// says Status: changed ("" when nothing changed, or it said so already).
type SpecRecorded struct {
	Changes []SpecChange
	Diff    string
	Text    string
}

// SpecPlace is a place a change reaches: where a requirement was found,
// a use of a symbol it sits in, or a passage search found; Why says which.
type SpecPlace struct {
	Path string
	Line int
	Kind string
	Text string
	Why  string
}

// SpecSymbol is a declaration a changed requirement's work sits in, and
// how many uses of it were found.
type SpecSymbol struct {
	Name string
	Path string
	Line int
	Uses int
}

// SpecStep is one step of an impact report's fix, with the files it touches.
type SpecStep struct {
	Title  string
	Detail string
	Files  []string
}

// SpecConflict is another spec's requirement a change contradicts.
type SpecConflict struct {
	ID  string
	Why string
}

// SpecImpact is a change's impact on the work (SDD-20, SDD-21): the
// changes, the symbols and places they reach (found without a model), and
// with the model its summary, the steps to fix it and the conflicts, the
// report written, and what it cost (or, estimated, would cost).
type SpecImpact struct {
	Changes      []SpecChange
	Symbols      []SpecSymbol
	Places       []SpecPlace
	Summary      string
	Steps        []SpecStep
	Conflicts    []SpecConflict
	Report       string
	CostUSD      float64
	EstimatedUSD float64
	Priced       bool
	Problem      string
}
