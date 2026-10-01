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
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
)

// A background task's questions go to its asker; an unattended run has
// nobody to ask.
func TestAskOneOutsideTheUsersPrompt(t *testing.T) {
	hooks := NewHooks(Policy{})
	hooks.SetUserPrompter(func(context.Context, string, []string) (string, error) {
		t.Error("the user's prompter was asked")
		return "", nil
	})
	q := QuestionItem{Question: "Which?", Options: []string{"a", "b"}}
	asker := func(answer string, err error) context.Context {
		return WithTaskAsker(context.Background(), TaskAsker{Ask: func(_ context.Context, question string, options []string) (string, error) {
			assert.Equal(t, "Which?", question)
			assert.Equal(t, []string{"a", "b"}, options)
			return answer, err
		}})
	}
	tests := []struct {
		name       string
		ctx        context.Context
		wantAnswer string
		wantErr    string
	}{
		{"the task's asker answers", asker("b", nil), "b", ""},
		{"the task's asker gets no answer", asker("", errors.New("session closed")), "", "no answer: session closed"},
		{"unattended", Unattended(context.Background(), func(context.Context, api.ApprovalRequest) (api.Decision, error) {
			return api.DecisionDeny, nil
		}), "", "this run is unattended"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			answer, errMsg := askOne(tt.ctx, hooks, q)
			assert.Equal(t, tt.wantAnswer, answer)
			assert.Contains(t, errMsg, tt.wantErr)
		})
	}
}
