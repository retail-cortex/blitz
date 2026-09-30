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

	"github.com/retail-cortex/blitz/pkg/config"
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
	} {
		t.Run(name, func(t *testing.T) {
			_, err := runCLI(t, args...)
			assert.Equal(t, exitUsage, exitCodeFor(err), "%v", err)
			assert.True(t, strings.Contains(err.Error(), "--") || err != nil)
		})
	}
}
