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
	"fmt"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// AskUserQuestionInput defines arguments for asking the user a question.
type AskUserQuestionInput struct {
	Question string   `json:"question" jsonschema:"The question or clarification needed from the user"`
	Options  []string `json:"options,omitempty" jsonschema:"Optional list of multiple choice options"`
}

// AskUserQuestionOutput holds the user's answer.
type AskUserQuestionOutput struct {
	Answer string `json:"answer"`
	Error  string `json:"error,omitempty"`
}

// NewAskUserQuestionTool creates an ADK tool for asking user questions. The
// prompt is delegated to hooks so it shares the host's stdin reader instead of
// competing with it for buffered input.
func NewAskUserQuestionTool(hooks *Hooks) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        "ask_user_question",
			Description: "Ask the user a question or clarification to resolve ambiguity or solicit guidance",
		},
		func(ctx agent.Context, input AskUserQuestionInput) (AskUserQuestionOutput, error) {
			if input.Question == "" {
				return AskUserQuestionOutput{Error: "question cannot be empty"}, nil
			}
			if a, ok := taskAskerFrom(ctx); ok && a.Ask != nil { // a background task's
				hooks.Notify(ctx, "question", input.Question)
				answer, err := a.Ask(ctx, input.Question, input.Options)
				if err != nil {
					return AskUserQuestionOutput{Error: fmt.Sprintf("no answer: %v; decide yourself and say what you assumed", err)}, nil
				}
				return AskUserQuestionOutput{Answer: answer}, nil
			}
			if isUnattended(ctx) {
				return AskUserQuestionOutput{Error: "this run is unattended: no one can answer; decide yourself and say what you assumed"}, nil
			}
			prompter := hooks.userPrompter()
			if prompter == nil {
				return AskUserQuestionOutput{Error: "interactive input is not available; proceed with your best judgement"}, nil
			}
			hooks.Notify(ctx, "question", input.Question)
			answer, err := prompter(ctx, input.Question, input.Options)
			if err != nil {
				return AskUserQuestionOutput{Error: fmt.Sprintf("prompt failed: %v", err)}, nil
			}
			return AskUserQuestionOutput{Answer: answer}, nil
		},
	)
}
