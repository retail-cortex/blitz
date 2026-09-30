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

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/retail-cortex/blitz/pkg/api"
)

// Headless runs (spec_parity_027 §5.3): a JSON schema for the answer
// (--json-schema), a stream of prompts and answers on stdin
// (--input-format stream-json), and runs that leave no session behind
// (--no-session-persistence).

// Input formats.
const (
	inputText       = "text"
	inputStreamJSON = "stream-json"
)

// answerSchema is --json-schema, resolved.
type answerSchema struct {
	text     string
	resolved *jsonschema.Resolved
}

// loadSchema reads --json-schema: inline JSON, or a file's.
func loadSchema(arg string) (*answerSchema, error) {
	data := []byte(strings.TrimSpace(arg))
	if len(data) == 0 || (data[0] != '{' && data[0] != '[') {
		var err error
		if data, err = os.ReadFile(arg); err != nil {
			return nil, err
		}
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("not a JSON schema: %w", err)
	}
	r, err := s.Resolve(nil)
	if err != nil {
		return nil, err
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		return nil, err
	}
	return &answerSchema{text: compact.String(), resolved: r}, nil
}

// instruction asks the agent for the answer the schema describes.
func (s *answerSchema) instruction() string {
	return "\n\nWhen you're done, give your final answer as a single JSON value valid against this JSON schema, and nothing else (no prose, no code fence):\n" + s.text
}

// check reads the answer's JSON (in a code fence or with words around it,
// if need be) and validates it.
func (s *answerSchema) check(output string) (json.RawMessage, error) {
	raw := extractJSON(output)
	if raw == nil {
		return nil, errors.New("the answer has no JSON value")
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("the answer's JSON doesn't parse: %w", err)
	}
	if err := s.resolved.Validate(v); err != nil {
		return nil, err
	}
	return raw, nil
}

// extractJSON is the JSON value in text: all of it, a fenced block, or
// from the first { or [ to the last } or ].
func extractJSON(text string) json.RawMessage {
	t := strings.TrimSpace(text)
	if json.Valid([]byte(t)) {
		return json.RawMessage(t)
	}
	if i := strings.Index(t, "```"); i >= 0 {
		rest := t[i+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:]
		}
		if j := strings.Index(rest, "```"); j >= 0 {
			if block := strings.TrimSpace(rest[:j]); json.Valid([]byte(block)) {
				return json.RawMessage(block)
			}
		}
	}
	for _, pair := range [][2]string{{"{", "}"}, {"[", "]"}} {
		i, j := strings.Index(t, pair[0]), strings.LastIndex(t, pair[1])
		if i >= 0 && j > i && json.Valid([]byte(t[i:j+1])) {
			return json.RawMessage(t[i : j+1])
		}
	}
	return nil
}

// lockedWriter keeps JSON lines from different goroutines whole (a JSON
// encoder writes each value in one Write).
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// inMessage is a line of --input-format stream-json.
type inMessage struct {
	Type string `json:"type"` // user, approval, answer
	// user
	Text string `json:"text"`
	// approval and answer
	ID       string `json:"id"`
	Decision string `json:"decision"` // once, session, always, deny
	Answer   string `json:"answer"`
}

// streamInput reads stdin's JSON lines: prompts in order on prompts, and
// answers to the approval requests and questions it sends out.
type streamInput struct {
	out     *json.Encoder
	prompts chan string

	mu      sync.Mutex
	seq     int
	waiting map[string]chan inMessage
	closed  bool
}

func newStreamInput(in io.Reader, out *json.Encoder) *streamInput {
	s := &streamInput{out: out, prompts: make(chan string, 64), waiting: map[string]chan inMessage{}}
	go s.read(in)
	return s
}

func (s *streamInput) read(in io.Reader) {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var m inMessage
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			s.out.Encode(map[string]any{"type": "error", "error": "not a JSON line: " + err.Error()})
			continue
		}
		switch m.Type {
		case "user":
			if strings.TrimSpace(m.Text) == "" {
				s.out.Encode(map[string]any{"type": "error", "error": "a user message needs text"})
				continue
			}
			s.prompts <- m.Text
		case "approval", "answer":
			s.mu.Lock()
			ch, ok := s.waiting[m.ID]
			delete(s.waiting, m.ID)
			s.mu.Unlock()
			if !ok {
				s.out.Encode(map[string]any{"type": "error", "error": fmt.Sprintf("nothing is waiting for %q", m.ID)})
				continue
			}
			ch <- m
		default:
			s.out.Encode(map[string]any{"type": "error", "error": fmt.Sprintf("unknown message type %q (user, approval or answer)", m.Type)})
		}
	}
	s.mu.Lock()
	s.closed = true
	for id, ch := range s.waiting {
		close(ch)
		delete(s.waiting, id)
	}
	s.mu.Unlock()
	close(s.prompts)
}

// ask sends a request line and waits for its answer (nothing: stdin
// ended, or ctx did).
func (s *streamInput) ask(ctx context.Context, prefix string, line map[string]any) (inMessage, bool) {
	ch := make(chan inMessage, 1)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return inMessage{}, false
	}
	s.seq++
	id := fmt.Sprintf("%s-%d", prefix, s.seq)
	s.waiting[id] = ch
	s.mu.Unlock()
	line["id"] = id
	s.out.Encode(line)
	select {
	case m, ok := <-ch:
		return m, ok
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.waiting, id)
		s.mu.Unlock()
		return inMessage{}, false
	}
}

// approve answers approval requests from stdin (deny when it ends).
func (s *streamInput) approve(ctx context.Context, req api.ApprovalRequest) (api.Decision, error) {
	m, ok := s.ask(ctx, "approval", map[string]any{
		"type": "approval_request", "tool": req.Tool, "kind": req.Kind, "detail": req.Detail, "diff": req.Diff,
		"targets": req.Targets, "key_label": req.KeyLabel, "must_ask": req.MustAsk,
	})
	if !ok {
		return api.DecisionDeny, nil
	}
	switch m.Decision {
	case "once", "yes", "allow":
		return api.DecisionOnce, nil
	case "session":
		return api.DecisionSession, nil
	case "always":
		return api.DecisionAlways, nil
	}
	return api.DecisionDeny, nil
}

// question answers the agent's questions from stdin.
func (s *streamInput) question(ctx context.Context, question string, options []string) (string, error) {
	m, ok := s.ask(ctx, "question", map[string]any{"type": "question", "question": question, "options": options})
	if !ok {
		return "", errors.New("no answer: the input ended")
	}
	return m.Answer, nil
}

// runStreamInput runs a turn for each user message on stdin (after the
// prompt given, if any), in order, until stdin ends.
func runStreamInput(ctx context.Context, w api.Backend, o oneShotOptions, in io.Reader) error {
	out := &lockedWriter{w: o.stdout}
	s := newStreamInput(in, json.NewEncoder(out))
	w.SetUI(s.approve, s.question)
	o.stdout, o.keepGoing = out, true
	var last error
	run := func(prompt string) {
		o.prompt = prompt
		if err := runOneShot(ctx, w, o); err != nil {
			last = err
		}
		o.images, o.sendPrompt = nil, "" // the first prompt's only
	}
	if o.prompt != "" {
		run(o.prompt)
	}
	for ctx.Err() == nil {
		select {
		case p, ok := <-s.prompts:
			if !ok {
				o.keepGoing = false
				return finishStream(ctx, w, o, last)
			}
			run(p)
		case <-ctx.Done():
		}
	}
	return finishStream(ctx, w, o, last)
}

// finishStream stops what the runs left running, as a one-shot run does.
func finishStream(ctx context.Context, w api.Backend, o oneShotOptions, last error) error {
	o.keepGoing = false
	endRun(ctx, w, o)
	return last
}
