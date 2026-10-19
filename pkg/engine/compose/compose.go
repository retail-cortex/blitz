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

// Package compose is the composer (spec_composer_045): a guided
// conversation that makes a workspace's component. It asks what it needs,
// writes the component in its file format, and checks it with the kind's
// own rules, sending the problems back to be fixed, before handing it on
// as a draft. A kind is a Recipe; the conversation is the same for all.
package compose

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Recipe is one kind of component: what the model is told about it, and
// how a draft of it is checked.
type Recipe struct {
	Kind string
	// Brief says what the kind is and its file format, for the model.
	Brief string
	// Check returns what doesn't hold in a draft (nothing: it loads).
	Check func(name, file string) []string
}

// Turn is one message of the conversation.
type Turn struct {
	// Role is "user" or "composer".
	Role string `json:"role"`
	Text string `json:"text"`
	// Options are a question's suggested answers.
	Options []string `json:"options,omitempty"`
}

// Draft is the component as the composer wrote it.
type Draft struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	// File is the component in its file format.
	File     string   `json:"file"`
	Problems []string `json:"problems,omitempty"`
}

// Session is one conversation.
type Session struct {
	ID        string
	Kind      string
	Workspace string
	// Current is the component being changed, as its file ("" for a new one).
	Current string
	Turns   []Turn
	// Draft is the latest draft (nil until there's one).
	Draft   *Draft
	CostUSD float64
	Updated time.Time
	// mu is held while a turn runs: one turn at a time.
	mu sync.Mutex
}

// Lock holds s for a turn.
func (s *Session) Lock() { s.mu.Lock() }

// Unlock lets s go after a turn.
func (s *Session) Unlock() { s.mu.Unlock() }

// Snapshot is a copy of s, to hand out.
func (s *Session) Snapshot() Session {
	c := Session{ID: s.ID, Kind: s.Kind, Workspace: s.Workspace, Current: s.Current, CostUSD: s.CostUSD, Updated: s.Updated}
	c.Turns = append([]Turn(nil), s.Turns...)
	if s.Draft != nil {
		d := *s.Draft
		d.Problems = append([]string(nil), s.Draft.Problems...)
		c.Draft = &d
	}
	return c
}

// Asking reports whether the composer's last turn is a question waiting
// for an answer.
func (s *Session) Asking() bool {
	return len(s.Turns) > 0 && s.Turns[len(s.Turns)-1].Role == "composer" && s.Draft == nil
}

// Asker has a model answer prompt under instruction, and says what it cost.
type Asker func(ctx context.Context, instruction, prompt string) (reply string, costUSD float64, err error)

// MaxQuestions is how many questions the composer may ask before it must
// draft.
const MaxQuestions = 5

// fixes is how many times a draft with problems goes back to be fixed.
const fixes = 2

// Instruction tells the model its job.
const Instruction = `You are the composer of an AI agent workspace: you make one component for the user (described below) through a short conversation.
Ask only what you must to make a good component, one question at a time, in plain words, with up to six short suggested answers when there are natural choices. When the request already says enough, draft at once.
When you draft, write the component's whole file in its format, using only names the workspace has (listed below) unless the user asked for something new, and nothing that isn't needed.
Answer in one of two ways.
A question: JSON only, {"question": "…", "options": ["…"]}
A draft: a JSON line, {"draft": {"name": "lowercase-name-with-hyphens", "summary": "one sentence: what it does"}}, then the file's whole text in one fenced block:
` + "```" + `file
…
` + "```"

// Prompt is what the model gets for s's next turn: the recipe's brief, the
// workspace's context, the component being changed, the conversation, and
// what's asked of this turn.
func Prompt(r Recipe, context string, s *Session, problems []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The component: a %s.\n\n%s\n\nThe workspace:\n%s\n", r.Kind, r.Brief, context)
	if s.Current != "" {
		fmt.Fprintf(&b, "\nThe user is changing this %s; draft the whole file again with the change:\n```\n%s\n```\n", r.Kind, s.Current)
	}
	b.WriteString("\nThe conversation:\n")
	for _, t := range s.Turns {
		fmt.Fprintf(&b, "%s: %s\n", t.Role, t.Text)
	}
	asked := s.questions()
	switch {
	case len(problems) > 0:
		fmt.Fprintf(&b, "\nYour last answer had problems. Fix these and answer again:\n- %s\n", strings.Join(problems, "\n- "))
	case asked >= MaxQuestions:
		b.WriteString("\nYou've asked enough: draft it now.\n")
	default:
		fmt.Fprintf(&b, "\nYou may ask %d more question(s) before drafting.\n", MaxQuestions-asked)
	}
	return b.String()
}

func (s *Session) questions() int {
	n := 0
	for _, t := range s.Turns {
		if t.Role == "composer" {
			n++
		}
	}
	return n
}

// reply is the model's answer.
type reply struct {
	Question string   `json:"question"`
	Options  []string `json:"options"`
	Draft    *Draft   `json:"draft"`
}

// ErrNoReply means the model's answer is neither a question nor a draft.
var ErrNoReply = errors.New("the composer's answer is neither a question nor a draft")

// fence finds the last fenced block in s: its text, and s without it.
func fence(s string) (body, rest string, ok bool) {
	end := strings.LastIndex(s, "```")
	if end < 0 {
		return "", s, false
	}
	start := strings.LastIndex(s[:end], "```")
	if start < 0 {
		return "", s, false
	}
	inner := s[start+3 : end]
	if nl := strings.IndexByte(inner, '\n'); nl >= 0 {
		inner = inner[nl+1:] // the fence's info string (file, yaml)
	} else {
		return "", s, false
	}
	return inner, s[:start] + s[end+3:], true
}

// firstObject is the first balanced JSON object in s ("" if none),
// skipping braces inside strings.
func firstObject(s string) string {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return ""
	}
	depth, inStr, esc := 0, false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case inStr && c == '\\':
			esc = true
		case c == '"':
			inStr = !inStr
		case inStr:
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

func parseReply(text string) (reply, error) {
	s := strings.TrimSpace(text)
	// A draft's file comes in its own fenced block, after the JSON line.
	file, rest, fenced := fence(s)
	obj := firstObject(rest)
	if obj == "" && fenced && strings.HasPrefix(strings.TrimSpace(file), "{") {
		obj, fenced = strings.TrimSpace(file), false // the JSON itself fenced
	}
	if obj == "" {
		return reply{}, fmt.Errorf("%w: no JSON", ErrNoReply)
	}
	var r reply
	if err := json.Unmarshal([]byte(obj), &r); err != nil {
		return reply{}, fmt.Errorf("%w: %v", ErrNoReply, err)
	}
	if r.Draft != nil && fenced && strings.TrimSpace(r.Draft.File) == "" {
		r.Draft.File = file
	}
	if r.Draft != nil {
		r.Draft.File = strings.TrimSpace(r.Draft.File)
		if r.Draft.File == "" {
			return reply{}, fmt.Errorf("%w: an empty draft", ErrNoReply)
		}
		return r, nil
	}
	if strings.TrimSpace(r.Question) == "" {
		return reply{}, ErrNoReply
	}
	if len(r.Options) > 6 {
		r.Options = r.Options[:6]
	}
	return r, nil
}

// Step runs the composer's turn on s: a question, or a draft checked with
// the recipe (and sent back with its problems, up to twice, to be fixed).
// context describes the workspace for the model.
func Step(ctx context.Context, r Recipe, context string, s *Session, ask Asker) error {
	var problems []string
	var last *Draft
	retried := false
	for try := 0; try <= fixes; try++ {
		text, cost, err := ask(ctx, Instruction, Prompt(r, context, s, problems))
		s.CostUSD += cost
		if err != nil {
			return err
		}
		rep, err := parseReply(text)
		if err != nil && last == nil && !retried {
			// Once: the answer's format, asked again.
			retried = true
			problems = []string{"your answer couldn't be read (" + err.Error() + "): answer in one of the two formats exactly"}
			try--
			continue
		}
		if err != nil {
			if last != nil {
				break // a fix that didn't come: the draft as it was
			}
			return err
		}
		if rep.Draft == nil {
			if last != nil {
				break // asked instead of fixing: the draft as it was
			}
			s.Turns = append(s.Turns, Turn{Role: "composer", Text: strings.TrimSpace(rep.Question), Options: rep.Options})
			s.Updated = time.Now()
			return nil
		}
		d := rep.Draft
		d.Name = strings.TrimSpace(d.Name)
		d.Problems = r.Check(d.Name, d.File)
		last = d
		if len(d.Problems) == 0 {
			break
		}
		problems = d.Problems
	}
	s.Draft = last
	s.Turns = append(s.Turns, Turn{Role: "composer", Text: last.Summary})
	s.Updated = time.Now()
	return nil
}

// Answer records the user's answer to s's question (or a change asked of
// its draft, which starts a new draft).
func Answer(s *Session, text string) {
	s.Turns = append(s.Turns, Turn{Role: "user", Text: strings.TrimSpace(text)})
	s.Draft = nil
}

// Keep is how long a session lasts after its last turn.
const Keep = time.Hour

// Store keeps the sessions going, in memory.
type Store struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

// New starts a session of kind in workspace, with the user's request.
func (st *Store) New(kind, workspace, current, request string) *Session {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	s := &Session{ID: "compose-" + hex.EncodeToString(b), Kind: kind, Workspace: workspace, Current: current, Updated: time.Now()}
	if strings.TrimSpace(request) != "" {
		s.Turns = append(s.Turns, Turn{Role: "user", Text: strings.TrimSpace(request)})
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.sweepLocked()
	if st.sessions == nil {
		st.sessions = map[string]*Session{}
	}
	st.sessions[s.ID] = s
	return s
}

// ErrNoSession means there's no such session (it ended, or it's another
// workspace's).
var ErrNoSession = errors.New("no such composer session")

// Get is workspace's session id.
func (st *Store) Get(workspace, id string) (*Session, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.sweepLocked()
	s, ok := st.sessions[id]
	if !ok || s.Workspace != workspace {
		return nil, ErrNoSession
	}
	return s, nil
}

// Drop forgets workspace's session id.
func (st *Store) Drop(workspace, id string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if s, ok := st.sessions[id]; ok && s.Workspace == workspace {
		delete(st.sessions, id)
	}
}

func (st *Store) sweepLocked() {
	cut := time.Now().Add(-Keep)
	for id, s := range st.sessions {
		if s.Updated.Before(cut) {
			delete(st.sessions, id)
		}
	}
}
