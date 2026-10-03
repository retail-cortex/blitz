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
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/config/configtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

const personSchema = `{"type": "object", "properties": {"name": {"type": "string"}, "age": {"type": "integer"}}, "required": ["name", "age"]}`

func TestExtractJSON(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{`{"a": 1}`, `{"a": 1}`},
		{"Here you go:\n```json\n{\"a\": 1}\n```\nDone.", `{"a": 1}`},
		{`The answer is {"a": [1, 2]} I think`, `{"a": [1, 2]}`},
		{`["x"]`, `["x"]`},
		{"no json here", ""},
	} {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, string(extractJSON(tt.in)))
		})
	}
}

func TestLoadSchema(t *testing.T) {
	s, err := loadSchema(personSchema)
	require.NoError(t, err)
	assert.Contains(t, s.instruction(), `"required":["name","age"]`)
	file := filepath.Join(t.TempDir(), "s.json")
	require.NoError(t, os.WriteFile(file, []byte(personSchema), 0o644))
	_, err = loadSchema(file)
	require.NoError(t, err)
	_, err = loadSchema(filepath.Join(t.TempDir(), "missing.json"))
	assert.Error(t, err)
	_, err = loadSchema(`{"type": 7}`)
	assert.Error(t, err)

	for _, tt := range []struct{ answer, wantErr string }{
		{answer: `{"name": "Ada", "age": 36}`},
		{answer: `{"name": "Ada"}`, wantErr: "age"},
		{answer: `Ada, 36`, wantErr: "no JSON value"},
	} {
		t.Run(tt.answer, func(t *testing.T) {
			_, err := s.check(tt.answer)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestOneShotJSONSchema(t *testing.T) {
	schema, err := loadSchema(personSchema)
	require.NoError(t, err)
	text := func(s string) *genai.Content { return genai.NewContentFromText(s, genai.RoleModel) }
	tests := []struct {
		name     string
		replies  []*genai.Content
		want     string
		exitCode int
	}{
		{name: "right at once", replies: []*genai.Content{text(`{"name": "Ada", "age": 36}`)}, want: `{"name": "Ada", "age": 36}`},
		{name: "right the second time", replies: []*genai.Content{text(`{"name": "Ada"}`), text("```json\n{\"name\": \"Ada\", \"age\": 36}\n```")}, want: `{"name": "Ada", "age": 36}`},
		{name: "wrong twice", replies: []*genai.Content{text("Ada"), text("Still Ada")}, exitCode: exitFailure},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := testEnv(t, tt.replies...)
			sess, _ := e.Storage().CreateSession("", "t", "blitz")
			var out bytes.Buffer
			err := runOneShot(context.Background(), e, oneShotOptions{prompt: "who?", sessionID: sess.ID, format: formatJSON, stdout: &out, schema: schema})
			assert.Equal(t, tt.exitCode, exitCodeFor(err))
			var res runResult
			require.NoError(t, json.Unmarshal(out.Bytes(), &res), out.String())
			if tt.want != "" {
				assert.JSONEq(t, tt.want, string(res.Structured))
			} else {
				assert.Nil(t, res.Structured)
				assert.Contains(t, res.Error, "isn't valid against --json-schema")
			}
		})
	}
}

// lineReader reads the run's JSON lines as they come.
type lineReader struct {
	t     *testing.T
	lines chan string
}

func newLineReader(t *testing.T, r io.Reader) lineReader {
	lines := make(chan string, 100)
	go func() {
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	return lineReader{t, lines}
}

// next is the next line of type typ, skipping others.
func (r lineReader) next(typ string) map[string]any {
	r.t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case l, ok := <-r.lines:
			if !ok {
				r.t.Fatalf("the output ended before a %s line", typ)
			}
			var m map[string]any
			require.NoError(r.t, json.Unmarshal([]byte(l), &m), l)
			if m["type"] == typ {
				return m
			}
		case <-timeout:
			r.t.Fatalf("no %s line in time", typ)
		}
	}
}

// Another program drives a session: prompts, an approval and a question
// over stdin, events and results on stdout.
func TestStreamJSONInput(t *testing.T) {
	e := testEnvWith(t, func(c *config.Config) { c.Blitz.AutoApprove = false },
		toolCall("create_file", map[string]any{"path": "a.txt", "content": "x"}), genai.NewContentFromText("made it", genai.RoleModel),
		toolCall("ask_user_question", map[string]any{"question": "Which colour?", "options": []any{"red", "blue"}}),
		genai.NewContentFromText("blue it is", genai.RoleModel))
	sess, _ := e.Storage().CreateSession("", "t", "blitz")
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- runStreamInput(context.Background(), e, oneShotOptions{sessionID: sess.ID, format: formatStreamJSON, stdout: outW}, inR)
		outW.Close()
	}()
	lines := newLineReader(t, outR)
	send := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(inW, "%s\n", b)
	}

	send(map[string]any{"type": "nonsense"})
	assert.Contains(t, lines.next("error")["error"], "unknown message type")
	send(map[string]any{"type": "user", "text": "make a file"})
	req := lines.next("approval_request")
	assert.Equal(t, "create_file", req["tool"])
	send(map[string]any{"type": "approval", "id": req["id"], "decision": "once"})
	res := lines.next("result")
	assert.Equal(t, "made it", res["result"])
	_, err := os.Stat(filepath.Join(e.Dir(), "a.txt"))
	assert.NoError(t, err)

	send(map[string]any{"type": "user", "text": "pick a colour"})
	q := lines.next("question")
	assert.Contains(t, q["question"], "Which colour?")
	send(map[string]any{"type": "answer", "id": q["id"], "answer": "blue"})
	assert.Equal(t, "blue it is", lines.next("result")["result"])

	inW.Close()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("the run didn't end with its input")
	}
}

func TestHeadlessFlags(t *testing.T) {
	isolate(t)
	for name, args := range map[string][]string{
		"stream input, text output":   {"--input-format", "stream-json"},
		"bad input format":            {"--input-format", "xml", "hi"},
		"bad schema":                  {"--json-schema", `{"type": 7}`, "hi"},
		"schema without a prompt":     {"--json-schema", personSchema, "-i"},
		"no persistence and continue": {"--no-session-persistence", "--continue", "hi"},
		"a missing prompt file":       {"--append-system-prompt-file", "/no/such/file", "hi"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := runCLI(t, args...)
			assert.Equal(t, exitUsage, exitCodeFor(err), "%v", err)
			assert.True(t, strings.Contains(err.Error(), "--") || err != nil)
		})
	}
}

// The stream-json protocol's own errors, each decision an approval can
// carry, and what an approval or question gets once the input ends or the
// run is cancelled.
func TestStreamInputProtocol(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	s := newStreamInput(inR, json.NewEncoder(outW))
	lines := newLineReader(t, outR)
	send := func(line string) { fmt.Fprintln(inW, line) }

	send("")
	send("not json")
	assert.Contains(t, lines.next("error")["error"], "not a JSON line")
	send(`{"type": "user", "text": "  "}`)
	assert.Contains(t, lines.next("error")["error"], "a user message needs text")
	send(`{"type": "answer", "id": "question-99", "answer": "x"}`)
	assert.Contains(t, lines.next("error")["error"], `nothing is waiting for "question-99"`)

	ctx := context.Background()
	for decision, want := range map[string]api.Decision{"yes": api.DecisionOnce, "session": api.DecisionSession, "always": api.DecisionAlways, "no": api.DecisionDeny} {
		t.Run(decision, func(t *testing.T) {
			got := make(chan api.Decision, 1)
			go func() {
				d, _ := s.approve(ctx, api.ApprovalRequest{Tool: "run_shell_command"})
				got <- d
			}()
			req := lines.next("approval_request")
			send(fmt.Sprintf(`{"type": "approval", "id": %q, "decision": %q}`, req["id"], decision))
			assert.Equal(t, want, <-got)
		})
	}

	// Cancelled while waiting: denied.
	cctx, cancel := context.WithCancel(ctx)
	got := make(chan api.Decision, 1)
	go func() {
		d, _ := s.approve(cctx, api.ApprovalRequest{Tool: "x"})
		got <- d
	}()
	lines.next("approval_request")
	cancel()
	assert.Equal(t, api.DecisionDeny, <-got)

	// The input ends while a question waits, and after.
	answered := make(chan error, 1)
	go func() {
		_, err := s.question(ctx, "Which?", []string{"a"})
		answered <- err
	}()
	lines.next("question")
	inW.Close()
	assert.ErrorContains(t, <-answered, "the input ended")
	_, err := s.question(ctx, "Again?", nil)
	assert.ErrorContains(t, err, "the input ended")
	d, err := s.approve(ctx, api.ApprovalRequest{})
	assert.NoError(t, err)
	assert.Equal(t, api.DecisionDeny, d)
	outW.Close()
}

// A prompt given with stream-json input runs first; a failing turn is the
// run's error; cancelling ends the stream.
func TestStreamInputPromptAndCancel(t *testing.T) {
	e := testEnv(t, loopReplies(3)...)
	sess, _ := e.Storage().CreateSession("", "t", "blitz")
	inR, inW := io.Pipe()
	var out bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runStreamInput(ctx, e, oneShotOptions{prompt: "first", sessionID: sess.ID, format: formatStreamJSON, stdout: &out, maxTurns: 1}, inR)
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		assert.ErrorIs(t, err, api.ErrMaxTurns, "the failed first turn")
	case <-time.After(10 * time.Second):
		t.Fatal("cancelling didn't end the stream")
	}
	inW.Close()
}

// In text mode a schema's retry prints like the first answer, with the
// usage line.
func TestOneShotJSONSchemaText(t *testing.T) {
	schema, err := loadSchema(personSchema)
	require.NoError(t, err)
	text := func(s string) *genai.Content { return genai.NewContentFromText(s, genai.RoleModel) }
	e := testEnv(t, text(`{"name": "Ada"}`), text(`{"name": "Ada", "age": 36}`))
	sess, _ := e.Storage().CreateSession("", "t", "blitz")
	var out bytes.Buffer
	err = runOneShot(context.Background(), e, oneShotOptions{prompt: "who?", sessionID: sess.ID, format: formatText, stdout: &out, schema: schema, usageLines: true})
	require.NoError(t, err)
	assert.Contains(t, out.String(), `"age": 36`)

}

// A run cancelled from outside exits as interrupted.
func TestOneShotInterrupted(t *testing.T) {
	e := testEnvWith(t, configtest.RunTools, toolCall("run_shell_command", map[string]any{"command": "sleep 5"}))
	sess, _ := e.Storage().CreateSession("", "t", "blitz")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := runOneShot(ctx, e, oneShotOptions{prompt: "wait", sessionID: sess.ID, format: formatJSON, stdout: &bytes.Buffer{}})
	assert.Equal(t, exitInterrupted, exitCodeFor(err), "%v", err)
}

// A schema whose reference goes nowhere doesn't load.
func TestLoadSchemaBadReference(t *testing.T) {
	_, err := loadSchema(`{"$ref": "#/definitions/nowhere"}`)
	assert.Error(t, err)
}

// Input that can't be read to its end (a line over the 4 MiB limit) is an
// error line and fails the run, rather than passing for the input's end.
func TestStreamInputTooLongALine(t *testing.T) {
	e := testEnv(t)
	sess, _ := e.Storage().CreateSession("", "t", "blitz")
	in := strings.NewReader(`{"type": "user", "text": "` + strings.Repeat("x", 5<<20) + `"}` + "\n")
	var out bytes.Buffer
	err := runStreamInput(context.Background(), e, oneShotOptions{sessionID: sess.ID, format: formatStreamJSON, stdout: &out}, in)
	require.ErrorIs(t, err, bufio.ErrTooLong)
	assert.Contains(t, out.String(), "reading the input")
}

// turnRecorder is a backend that notes each turn it runs.
type turnRecorder struct {
	api.Backend
	turns []api.Turn
}

func (r *turnRecorder) Run(ctx context.Context, sessionID string, t api.Turn, on func(api.Event)) (api.TurnResult, error) {
	r.turns = append(r.turns, t)
	return r.Backend.Run(ctx, sessionID, t, on)
}

// The schema's retry turn runs under the run's own limits and mode.
func TestOneShotJSONSchemaRetryKeepsTheLimits(t *testing.T) {
	schema, err := loadSchema(personSchema)
	require.NoError(t, err)
	text := func(s string) *genai.Content { return genai.NewContentFromText(s, genai.RoleModel) }
	e := testEnv(t, text(`{"name": "Ada"}`), text(`{"name": "Ada", "age": 36}`))
	sess, _ := e.Storage().CreateSession("", "t", "blitz")
	r := &turnRecorder{Backend: e}
	o := oneShotOptions{prompt: "who?", sessionID: sess.ID, format: formatJSON, stdout: &bytes.Buffer{}, schema: schema,
		plan: true, maxTurns: 7, maxCostUSD: 1.5, timeout: time.Minute}
	require.NoError(t, runOneShot(context.Background(), r, o))
	require.Len(t, r.turns, 2, "the answer and its retry")
	retry := r.turns[1]
	assert.True(t, retry.Plan, "plan mode")
	assert.Equal(t, 7, retry.MaxTurns)
	assert.Equal(t, 1.5, retry.MaxCostUSD)
	assert.Equal(t, time.Minute, retry.Timeout)
}
