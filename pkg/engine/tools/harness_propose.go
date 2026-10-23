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
	"strings"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// ProposalSaver keeps a scheduled reflection's proposed changes for the
// user to review with /reflect (spec_harness_learning_048 HE-06); the
// engine supplies it.
type ProposalSaver func(ctx context.Context, changes []string) error

// SetProposalSaver supplies the harness_propose tool's saver; nil turns it
// off.
func (r *Registry) SetProposalSaver(save ProposalSaver) {
	r.evidenceMu.Lock()
	defer r.evidenceMu.Unlock()
	r.propose = save
}

func (r *Registry) proposalSaver() ProposalSaver {
	r.evidenceMu.Lock()
	defer r.evidenceMu.Unlock()
	return r.propose
}

// maxProposals is how many changes a reflection proposes at most.
const maxProposals = 5

// HarnessProposeInput is what the harness_propose tool takes.
type HarnessProposeInput struct {
	Changes []string `json:"changes" jsonschema:"The changes you would offer the user, at most five, most frequent first, each: add|update|retire <where>: <the change> — <evidence>"`
}

// HarnessProposeOutput is what it returns.
type HarnessProposeOutput struct {
	Saved int    `json:"saved,omitempty"`
	Error string `json:"error,omitempty"`
}

// errProposeAttended refuses harness_propose in a conversation, where the
// user is asked instead.
var errProposeAttended = errors.New("only a scheduled reflection keeps its proposals: ask the user with ask_user_question instead")

// NewHarnessProposeTool is harness_propose: a scheduled reflection keeps
// the changes it would have offered, for the user to choose from with
// /reflect. It writes nothing in the workspace, and works only unattended.
func NewHarnessProposeTool(r *Registry) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{Name: "harness_propose", Description: "On a scheduled reflection only: keep the changes you would offer the user (at most five), replacing any kept before, for them to review with /reflect. It changes nothing in the workspace."},
		func(ctx agent.Context, in HarnessProposeInput) (HarnessProposeOutput, error) {
			save := r.proposalSaver()
			switch {
			case save == nil:
				return HarnessProposeOutput{Error: errNoEvidence.Error()}, nil
			case !isUnattended(ctx):
				return HarnessProposeOutput{Error: errProposeAttended.Error()}, nil
			}
			var changes []string
			for _, c := range in.Changes {
				if c = strings.TrimSpace(c); c != "" && len(changes) < maxProposals {
					changes = append(changes, c)
				}
			}
			if err := save(ctx, changes); err != nil {
				return HarnessProposeOutput{Error: err.Error()}, nil
			}
			return HarnessProposeOutput{Saved: len(changes)}, nil
		})
}

// GlobalExperienceSaver adds a "How to work" entry to the user's global
// instructions file and returns its path (spec_harness_learning_048
// HE-21); the engine supplies it.
type GlobalExperienceSaver func(ctx context.Context, entry string) (string, error)

// SetGlobalExperienceSaver supplies the save_global_experience tool's
// saver; nil turns it off.
func (r *Registry) SetGlobalExperienceSaver(save GlobalExperienceSaver) {
	r.evidenceMu.Lock()
	defer r.evidenceMu.Unlock()
	r.global = save
}

func (r *Registry) globalExperienceSaver() GlobalExperienceSaver {
	r.evidenceMu.Lock()
	defer r.evidenceMu.Unlock()
	return r.global
}

// SaveGlobalExperienceInput is what the save_global_experience tool takes.
type SaveGlobalExperienceInput struct {
	Entry string `json:"entry" jsonschema:"The \"How to work\" entry the user chose to keep for every workspace, as one sentence with its evidence in brackets"`
}

// SaveGlobalExperienceOutput is what it returns.
type SaveGlobalExperienceOutput struct {
	Path  string `json:"path,omitempty"`
	Error string `json:"error,omitempty"`
}

// errGlobalUnattended refuses save_global_experience where nobody chose.
var errGlobalUnattended = errors.New("only the user can choose an entry for every workspace: nobody can answer here")

// NewSaveGlobalExperienceTool is save_global_experience: a "How to work"
// entry that held in several workspaces goes to the user's global
// instructions file, once the user chose it.
func NewSaveGlobalExperienceTool(r *Registry) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{Name: "save_global_experience", Description: "Add a \"How to work\" entry to the user's global instructions file, read in every workspace. Only an entry the user chose in your question, that held in two or more workspaces; never a fact about one project."},
		func(ctx agent.Context, in SaveGlobalExperienceInput) (SaveGlobalExperienceOutput, error) {
			save := r.globalExperienceSaver()
			entry := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(in.Entry), "- "))
			switch {
			case save == nil:
				return SaveGlobalExperienceOutput{Error: errNoEvidence.Error()}, nil
			case isUnattended(ctx):
				return SaveGlobalExperienceOutput{Error: errGlobalUnattended.Error()}, nil
			case entry == "" || strings.Contains(entry, "\n"):
				return SaveGlobalExperienceOutput{Error: "the entry is one line"}, nil
			}
			path, err := save(ctx, entry)
			if err != nil {
				return SaveGlobalExperienceOutput{Error: err.Error()}, nil
			}
			return SaveGlobalExperienceOutput{Path: path}, nil
		})
}
