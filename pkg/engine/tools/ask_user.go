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
	"fmt"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// AskUserQuestionInput defines arguments for asking the user: one
// question, or several at once.
type AskUserQuestionInput struct {
	Question    string   `json:"question,omitempty" jsonschema:"The question or clarification needed from the user"`
	Options     []string `json:"options,omitempty" jsonschema:"Optional list of multiple choice options"`
	MultiSelect bool     `json:"multi_select,omitempty" jsonschema:"The user may choose several of the options"`
	// Choices are options explained, in recommended order (APR-40b).
	Choices []ChoiceItem `json:"choices,omitempty" jsonschema:"Instead of options, when they name technical things (frameworks, libraries, services, approaches): each explained for someone who doesn't know them, in recommended order, best first"`
	Order   string       `json:"order,omitempty" jsonschema:"With choices: what the order rests on, in a sentence (easiest to maintain with AI, the largest community, what the project's ADRs or conventions require)"`
	// Questions asks several at once, answered in turn.
	Questions []QuestionItem `json:"questions,omitempty" jsonschema:"Several questions to ask together, instead of question and options"`
}

// QuestionItem is one of several questions.
type QuestionItem struct {
	Question    string       `json:"question" jsonschema:"The question"`
	Options     []string     `json:"options,omitempty" jsonschema:"Suggested answers"`
	MultiSelect bool         `json:"multi_select,omitempty" jsonschema:"The user may choose several options"`
	Choices     []ChoiceItem `json:"choices,omitempty" jsonschema:"Instead of options, when they're technical: each explained, in recommended order, best first"`
	Order       string       `json:"order,omitempty" jsonschema:"With choices: what the order rests on, in a sentence"`
}

// ChoiceItem is an option explained for someone who doesn't know the
// tools it names (APR-40b).
type ChoiceItem struct {
	Label   string `json:"label" jsonschema:"What the button says: short and plain (Vite + React + Tailwind)"`
	Summary string `json:"summary,omitempty" jsonschema:"One plain sentence: what it is and what it's good for (its tooltip)"`
	Details string `json:"details,omitempty" jsonschema:"A few sentences for someone non-technical: what choosing it means for them, its trade-offs"`
	Why     string `json:"why,omitempty" jsonschema:"Why it's ranked here (easiest to maintain with AI, most contributors, meets the ADR)"`
}

// choices are q's choices as the prompter takes them, and q with its
// options their labels (choices win over options).
func (q QuestionItem) choices() (QuestionItem, []api.Choice, string) {
	if len(q.Choices) == 0 {
		return q, nil, ""
	}
	out := make([]api.Choice, 0, len(q.Choices))
	q.Options = nil
	for _, c := range q.Choices {
		label := strings.TrimSpace(c.Label)
		if label == "" {
			return q, nil, "every choice needs a label"
		}
		q.Options = append(q.Options, label)
		out = append(out, api.Choice{Label: label, Summary: strings.TrimSpace(c.Summary), Details: strings.TrimSpace(c.Details), Why: strings.TrimSpace(c.Why)})
	}
	return q, out, strings.TrimSpace(q.Order)
}

// AskUserQuestionOutput holds the user's answer (or answers).
type AskUserQuestionOutput struct {
	Answer string `json:"answer,omitempty"`
	// Selected are the options chosen, for a multi-select question.
	Selected []string         `json:"selected,omitempty"`
	Answers  []QuestionAnswer `json:"answers,omitempty"`
	Error    string           `json:"error,omitempty"`
}

// QuestionAnswer is the answer to one of several questions.
type QuestionAnswer struct {
	Question string   `json:"question"`
	Answer   string   `json:"answer"`
	Selected []string `json:"selected,omitempty"`
}

// maxQuestions bounds the questions of one call.
const maxQuestions = 8

// NewAskUserQuestionTool creates an ADK tool for asking user questions. The
// prompt is delegated to hooks so it shares the host's stdin reader instead of
// competing with it for buffered input.
func NewAskUserQuestionTool(hooks *Hooks) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name: "ask_user_question",
			Description: "Ask the user a question or clarification to resolve ambiguity or solicit guidance, when you can't go on without their answer. Offer options when you can; multi_select lets the user choose several. Several related questions can go in one call (questions). " +
				"When the options name technical things (frameworks, libraries, services, architectures), give choices instead of options: not every user is technical, so each has a short plain label, a one-sentence summary, details on what it means for them, and why it's ranked there; list them best first, and say in order what the ranking rests on (easiest to maintain with AI, the largest community, the project's ADRs). " +
				"Not for conversation: reply to a greeting, small talk or a thank-you, and ask what they'd like to do, in plain text, without this tool.",
		},
		func(ctx agent.Context, input AskUserQuestionInput) (AskUserQuestionOutput, error) {
			items := input.Questions
			if len(items) == 0 {
				if input.Question == "" {
					return AskUserQuestionOutput{Error: "question cannot be empty"}, nil
				}
				items = []QuestionItem{{Question: input.Question, Options: input.Options, MultiSelect: input.MultiSelect, Choices: input.Choices, Order: input.Order}}
			}
			if len(items) > maxQuestions {
				return AskUserQuestionOutput{Error: fmt.Sprintf("at most %d questions at once", maxQuestions)}, nil
			}
			var answers []QuestionAnswer
			for _, q := range items {
				if strings.TrimSpace(q.Question) == "" {
					return AskUserQuestionOutput{Error: "question cannot be empty"}, nil
				}
				q, choices, why := q.choices()
				if len(q.Choices) > 0 && choices == nil {
					return AskUserQuestionOutput{Error: why}, nil
				}
				if q.MultiSelect && len(q.Options) == 0 {
					return AskUserQuestionOutput{Error: "multi_select needs options"}, nil
				}
				answer, errMsg := askOne(api.WithChoices(ctx, choices, why), hooks, q)
				if errMsg != "" {
					return AskUserQuestionOutput{Answers: answers, Error: errMsg}, nil
				}
				a := QuestionAnswer{Question: q.Question, Answer: answer}
				if q.MultiSelect {
					a.Selected = splitSelected(answer)
					a.Answer = strings.Join(a.Selected, ", ")
				}
				answers = append(answers, a)
			}
			if len(input.Questions) == 0 {
				return AskUserQuestionOutput{Answer: answers[0].Answer, Selected: answers[0].Selected}, nil
			}
			return AskUserQuestionOutput{Answers: answers}, nil
		},
	)
}

// askOne asks one question, through a background task's asker or the
// user's prompter; the error text says why there's no answer.
func askOne(ctx context.Context, hooks *Hooks, q QuestionItem) (string, string) {
	if q.MultiSelect {
		ctx = api.WithMultiSelect(ctx)
	}
	if a, ok := taskAskerFrom(ctx); ok && a.Ask != nil { // a background task's
		hooks.Notify(ctx, "question", q.Question)
		answer, err := a.Ask(ctx, q.Question, q.Options)
		if err != nil {
			return "", fmt.Sprintf("no answer: %v; decide yourself and say what you assumed", err)
		}
		return answer, ""
	}
	if isUnattended(ctx) {
		return "", "this run is unattended: no one can answer; decide yourself and say what you assumed"
	}
	prompter := hooks.userPrompter()
	if prompter == nil {
		return "", "interactive input is not available; proceed with your best judgement"
	}
	hooks.Notify(ctx, "question", q.Question)
	answer, err := prompter(ctx, q.Question, q.Options)
	if err != nil {
		return "", fmt.Sprintf("prompt failed: %v", err)
	}
	return answer, ""
}

// splitSelected reads a multi-select answer: the options, one per line.
func splitSelected(answer string) []string {
	var out []string
	for line := range strings.Lines(answer) {
		if l := strings.TrimSpace(line); l != "" {
			out = append(out, l)
		}
	}
	return out
}
