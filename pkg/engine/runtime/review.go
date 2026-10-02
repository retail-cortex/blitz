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

package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/textutil"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// The auto permission mode's reviewer (spec_parity_027 PAR-PERM-20, -21):
// what would ask the user goes to a model instead, with the request, the
// recent conversation and the environment the user described, and it
// allows or denies with a reason. Its tokens count for the session.

// reviewInstruction tells the reviewer its job.
const reviewInstruction = `You review actions an AI coding agent wants to take on the user's computer, in place of the user, who chose to let you decide.
Allow an action when it follows from what the user asked and stays within the environment below. Deny it when it could do harm the user didn't ask for: deleting or overwriting work outside the task, reading or sending secrets, reaching hosts the task doesn't need, installing or running untrusted code, pushing, deploying or spending money, or anything you can't tell is safe.
Answer with JSON only: {"decision": "allow" or "deny", "reason": "one short sentence"}.`

// maxReviewDiff bounds how much of a change the reviewer sees.
const maxReviewDiff = 8000

// WithReviewModel makes llm the auto mode's reviewer
// ([permissions.auto] model); without it the session's model reviews.
func WithReviewModel(llm model.LLM) Option { return func(e *Engine) { e.reviewModel = llm } }

// review is the Hooks' reviewer.
func (e *Engine) review(ctx context.Context, req api.ApprovalRequest) (bool, string, error) {
	e.mu.RLock()
	llm := e.reviewModel
	if llm == nil {
		llm = e.llm
	}
	e.mu.RUnlock()
	if llm == nil {
		return false, "", errors.New("no model to review with")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Environment (the user's description):\n%s\n\n", orNothing(e.cfg.Permissions.Auto.Environment))
	fmt.Fprintf(&b, "Workspace: %s\n\n", e.toolReg.Workspace().Dir())
	if recent := e.recentConversation(ctx); recent != "" {
		fmt.Fprintf(&b, "Recent conversation:\n%s\n\n", recent)
	}
	fmt.Fprintf(&b, "The action:\n- tool: %s\n- kind: %s\n- what: %s\n", req.Tool, req.Kind, req.Detail)
	if len(req.Targets) > 0 {
		fmt.Fprintf(&b, "- targets: %s\n", strings.Join(req.Targets, ", "))
	}
	if req.Diff != "" {
		fmt.Fprintf(&b, "- the change:\n%s\n", textutil.Ellipsize(req.Diff, maxReviewDiff))
	}

	text, err := e.askModel(ctx, llm, reviewInstruction, b.String())
	if err != nil {
		return false, "", err
	}
	return parseVerdict(text)
}

// askModel has llm answer prompt under instruction, and counts its tokens
// for the session.
func (e *Engine) askModel(ctx context.Context, llm model.LLM, instruction, prompt string) (string, error) {
	text, served, usage, err := Ask(ctx, llm, instruction, prompt)
	if err != nil {
		return "", err
	}
	if usage != nil { // priced in the session's /cost
		id := "default"
		if st := stateFrom(ctx); st != nil && st.sessionID != "" {
			id = st.sessionID
		}
		e.restoreUsage(id)
		e.usage.RecordWrites(id, served, usage, 0)
		e.saveUsage(ctx, id)
	}
	return text, nil
}

// Ask has llm answer prompt under instruction, outside any session (no
// tools, no history): its text without thoughts, the model that served
// it, and the tokens it took.
func Ask(ctx context.Context, llm model.LLM, instruction, prompt string) (text, served string, usage *genai.GenerateContentResponseUsageMetadata, err error) {
	llmReq := &model.LLMRequest{
		Contents: []*genai.Content{genai.NewContentFromText(prompt, genai.RoleUser)},
		Config:   &genai.GenerateContentConfig{SystemInstruction: genai.NewContentFromText(instruction, genai.RoleUser)},
	}
	var b strings.Builder
	served = llm.Name()
	for resp, err := range llm.GenerateContent(ctx, llmReq, false) {
		if err != nil {
			return "", "", nil, err
		}
		if resp.UsageMetadata != nil {
			usage = resp.UsageMetadata
		}
		if resp.ModelVersion != "" {
			served = resp.ModelVersion
		}
		if resp.Content != nil {
			for _, p := range resp.Content.Parts {
				if !p.Thought {
					b.WriteString(p.Text)
				}
			}
		}
	}
	return b.String(), served, usage, nil
}

// hookInstruction tells a prompt hook's model its job.
const hookInstruction = `You are a hook in an AI coding agent: you check an event (a tool call, a prompt, the end of a turn) against the user's criteria below, and decide.
Answer with JSON only: {"decision": "allow", "deny", "ask" or "block", "reason": "one short sentence", "additional_context": "optional text for the agent"}.
Use "block" or "deny" to stop what the criteria forbid, "ask" to have the user decide, and "allow" when the criteria allow it. When the criteria say nothing about the event, answer {"decision": ""}.`

// WithHookModel makes llm the model of prompt hooks naming ref.
func WithHookModel(ref string, llm model.LLM) Option {
	return func(e *Engine) {
		if e.hookModels == nil {
			e.hookModels = map[string]model.LLM{}
		}
		e.hookModels[ref] = llm
	}
}

// judgeHook is the prompt hooks' judge: the hook's model (else the auto
// mode's reviewer, else the session's model) answers with a hook reply.
func (e *Engine) judgeHook(ctx context.Context, ref, criteria string, event []byte) (string, error) {
	e.mu.RLock()
	llm := e.hookModels[ref]
	if llm == nil {
		llm = e.reviewModel
	}
	if llm == nil {
		llm = e.llm
	}
	e.mu.RUnlock()
	if llm == nil {
		return "", errors.New("no model to judge the hook with")
	}
	text, err := e.askModel(ctx, llm, hookInstruction, fmt.Sprintf("The user's criteria:\n%s\n\nThe event (JSON):\n%s", criteria, textutil.Ellipsize(string(event), 16000)))
	if err != nil {
		return "", err
	}
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return "", fmt.Errorf("the hook's model didn't answer JSON: %q", textutil.Ellipsize(text, 200))
	}
	return text[start : end+1], nil
}

// parseVerdict reads the reviewer's JSON answer, which may come in a code
// fence or with words around it.
func parseVerdict(s string) (bool, string, error) {
	start, end := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if start < 0 || end < start {
		return false, "", fmt.Errorf("the reviewer's answer isn't JSON: %q", textutil.Ellipsize(s, 200))
	}
	var v struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(s[start:end+1]), &v); err != nil {
		return false, "", fmt.Errorf("the reviewer's answer isn't JSON: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(v.Decision)) {
	case "allow":
		return true, v.Reason, nil
	case "deny":
		return false, v.Reason, nil
	}
	return false, "", fmt.Errorf("the reviewer answered %q, not allow or deny", v.Decision)
}

// recentConversation is the end of the run's session, in words: the last
// few prompts and answers, for the reviewer to see what was asked.
func (e *Engine) recentConversation(ctx context.Context) string {
	return e.recentConversationN(ctx, 6)
}

// recentConversationN is the last n prompts and answers of the run's
// session.
func (e *Engine) recentConversationN(ctx context.Context, n int) string {
	st := stateFrom(ctx)
	if st == nil || st.sessionID == "" {
		return ""
	}
	got, err := e.sessions.Get(ctx, &session.GetRequest{AppName: appName, UserID: "user", SessionID: st.sessionID})
	if err != nil {
		return ""
	}
	var lines []string
	for _, ev := range finalEvents(got.Session) {
		if ev.Content == nil {
			continue
		}
		for _, p := range ev.Content.Parts {
			if p.Text == "" || p.Thought {
				continue
			}
			who := "agent"
			if ev.Content.Role == genai.RoleUser {
				who = "user"
			}
			lines = append(lines, who+": "+textutil.Ellipsize(strings.TrimSpace(p.Text), 600))
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func orNothing(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none given)"
	}
	return s
}

// goalInstruction tells the goal's judge its job.
const goalInstruction = `You judge whether an AI coding agent has reached a goal the user set, from the end of its conversation with the user.
Answer with JSON only: {"verdict": "met", "unmet" or "impossible", "reason": "one or two sentences", "next": "when unmet: what the agent should do next"}.
Say "met" only when the conversation shows the goal holds (tests the goal names passed, the change is made); "impossible" when the agent can't reach it (it needs something only the user can give, or it contradicts itself); otherwise "unmet".`

// GoalVerdict is the judge's answer about a goal.
type GoalVerdict struct {
	Met, Impossible bool
	Reason, Next    string
}

// JudgeGoal has a model (the auto reviewer's, else the session's) decide
// whether the session's conversation shows condition holds (spec_parity_027
// PAR-SES-30).
func (e *Engine) JudgeGoal(ctx context.Context, sessionID, condition string) (GoalVerdict, error) {
	e.mu.RLock()
	llm := e.reviewModel
	if llm == nil {
		llm = e.llm
	}
	e.mu.RUnlock()
	if llm == nil {
		return GoalVerdict{}, errors.New("no model to judge the goal with")
	}
	if st := stateFrom(ctx); st == nil || st.sessionID != sessionID { // for the conversation, and /cost
		ctx = context.WithValue(ctx, runStateKey{}, &runState{sessionID: sessionID})
	}
	prompt := fmt.Sprintf("The goal:\n%s\n\nThe end of the conversation:\n%s\n", condition, orNothing(e.recentConversationN(ctx, 10)))
	text, err := e.askModel(ctx, llm, goalInstruction, prompt)
	if err != nil {
		return GoalVerdict{}, err
	}
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return GoalVerdict{}, fmt.Errorf("the judge's answer isn't JSON: %q", textutil.Ellipsize(text, 200))
	}
	var v struct {
		Verdict string `json:"verdict"`
		Reason  string `json:"reason"`
		Next    string `json:"next"`
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &v); err != nil {
		return GoalVerdict{}, fmt.Errorf("the judge's answer isn't JSON: %w", err)
	}
	out := GoalVerdict{Reason: strings.TrimSpace(v.Reason), Next: strings.TrimSpace(v.Next)}
	switch strings.ToLower(strings.TrimSpace(v.Verdict)) {
	case "met":
		out.Met = true
	case "impossible":
		out.Impossible = true
	case "unmet":
	default:
		return GoalVerdict{}, fmt.Errorf("the judge answered %q, not met, unmet or impossible", v.Verdict)
	}
	return out, nil
}
