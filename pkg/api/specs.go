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
