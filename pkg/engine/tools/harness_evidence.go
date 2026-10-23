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

package tools

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// EvidenceReader returns what went wrong in the workspace's sessions since
// then (the zero time: since the last reflection, else the default
// window), only about skills, agents or rules when about says so, as text,
// and the time it starts from (spec_harness_learning_048 HE-02); the
// engine supplies it.
type EvidenceReader func(ctx context.Context, since time.Time, about string) (string, time.Time)

// SetEvidenceReader supplies the harness_evidence tool's reader; nil turns
// it off.
func (r *Registry) SetEvidenceReader(read EvidenceReader) {
	r.evidenceMu.Lock()
	defer r.evidenceMu.Unlock()
	r.evidence = read
}

func (r *Registry) evidenceReader() EvidenceReader {
	r.evidenceMu.Lock()
	defer r.evidenceMu.Unlock()
	return r.evidence
}

// HarnessEvidenceInput is what the harness_evidence tool takes.
type HarnessEvidenceInput struct {
	Since string `json:"since,omitempty" jsonschema:"Where the evidence starts: a date (2026-10-01) or a number of days (14). Empty: since the last reflection, else the last 14 days"`
	About string `json:"about,omitempty" jsonschema:"skills, agents or rules (rules and guardrails): only the evidence about them. Empty: everything"`
}

// HarnessEvidenceOutput is what it returns.
type HarnessEvidenceOutput struct {
	Since    string `json:"since,omitempty"`
	Evidence string `json:"evidence,omitempty"`
	Error    string `json:"error,omitempty"`
}

// errNoEvidence means the engine gave the tool no reader.
var errNoEvidence = errors.New("the evidence isn't available here")

// NewHarnessEvidenceTool is harness_evidence: what went wrong in this
// workspace's sessions, from its audit log, for the agents that improve
// its harness. It reads; it changes nothing.
func NewHarnessEvidenceTool(r *Registry) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{Name: "harness_evidence", Description: "What went wrong in this workspace's sessions: failed tools and commands, denied actions, guardrail blocks, rules that sent work back, the user's steers and corrections, undos, stopped turns and limits, and the skills and agents in turns that went wrong. Each item has a count and up to three occurrences (session, turn, time, an excerpt); [pattern] marks one seen at least twice, or once with a cost. Read-only."},
		func(ctx agent.Context, in HarnessEvidenceInput) (HarnessEvidenceOutput, error) {
			read := r.evidenceReader()
			if read == nil {
				return HarnessEvidenceOutput{Error: errNoEvidence.Error()}, nil
			}
			since, err := parseSince(in.Since, time.Now())
			if err != nil {
				return HarnessEvidenceOutput{Error: err.Error()}, nil
			}
			about := strings.ToLower(strings.TrimSpace(in.About))
			switch about {
			case "", "skills", "agents", "rules":
			default:
				return HarnessEvidenceOutput{Error: fmt.Sprintf("about is skills, agents, rules or empty, not %q", in.About)}, nil
			}
			text, from := read(ctx, since, about)
			return HarnessEvidenceOutput{Since: from.Format(time.DateOnly), Evidence: text}, nil
		})
}

// parseSince reads a date or a number of days before now ("" the zero
// time: the reader's default).
func parseSince(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	if n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSuffix(s, " days"), "d")); err == nil {
		if n <= 0 {
			return time.Time{}, fmt.Errorf("since is a number of days above 0, not %d", n)
		}
		return now.AddDate(0, 0, -n), nil
	}
	t, err := time.ParseInLocation(time.DateOnly, s, now.Location())
	if err != nil {
		return time.Time{}, fmt.Errorf("since is a date (2026-10-01) or a number of days (14), not %q", s)
	}
	return t, nil
}
