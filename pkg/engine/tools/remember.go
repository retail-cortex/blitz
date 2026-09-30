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

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// NoteSaver keeps a note the agent remembers across sessions and returns
// its name (spec_parity_027 PAR-MEM-10); the engine supplies it.
type NoteSaver func(ctx context.Context, kind, text string) (string, error)

// SetNoteSaver supplies the remember tool's saver; nil turns it off.
func (r *Registry) SetNoteSaver(s NoteSaver) {
	r.notesMu.Lock()
	defer r.notesMu.Unlock()
	r.notes = s
}

func (r *Registry) noteSaver() NoteSaver {
	r.notesMu.Lock()
	defer r.notesMu.Unlock()
	return r.notes
}

// RememberInput is what the remember tool takes.
type RememberInput struct {
	Note string `json:"note" jsonschema:"The note: one short, self-contained fact, preference or correction worth knowing in later sessions of this workspace"`
	Kind string `json:"kind,omitempty" jsonschema:"fact (the default), preference (how the user likes things done) or correction (a mistake not to repeat)"`
}

// RememberOutput is what it returns.
type RememberOutput struct {
	Saved string `json:"saved,omitempty"`
	Error string `json:"error,omitempty"`
}

// errMemoryOff: notes are turned off (memory.auto = false).
var errMemoryOff = errors.New("notes are turned off in this workspace (memory.auto = false)")

// NewRememberTool is remember: the agent keeps a note for later sessions.
// Notes never hold secrets (they're redacted) and never grant permission.
func NewRememberTool(r *Registry) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{Name: "remember", Description: "Save a short note for later sessions of this workspace: a fact about the project, a preference of the user's, or a correction to a mistake. Save only what is durable and not already in the project's files or instructions; never secrets."},
		func(ctx agent.Context, in RememberInput) (RememberOutput, error) {
			save := r.noteSaver()
			if save == nil {
				return RememberOutput{Error: errMemoryOff.Error()}, nil
			}
			kind := in.Kind
			if kind == "" {
				kind = "fact"
			}
			name, err := save(ctx, kind, in.Note)
			if err != nil {
				return RememberOutput{Error: err.Error()}, nil
			}
			return RememberOutput{Saved: name}, nil
		})
}
