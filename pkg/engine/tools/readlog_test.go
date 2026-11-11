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
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// namedAgent is an agent with only a name.
type namedAgent struct {
	agent.Agent
	name string
}

func (a namedAgent) Name() string { return a.name }

// conversationIC is an invocation of one agent in one conversation.
type conversationIC struct {
	*mockIC
	sess  session.Session
	agent agent.Agent
}

func (c conversationIC) Session() session.Session { return c.sess }
func (c conversationIC) Agent() agent.Agent       { return c.agent }
func (c conversationIC) Branch() string           { return "" }

// reader runs read_file as one agent of conversation s, and plays the
// before-model hook: shown tells the read log what the next request
// carries.
type reader struct {
	t     *testing.T
	ws    *Workspace
	rt    runnerTool
	ic    conversationIC
	calls int
	sent  []*genai.Content // the read_file results this agent has been sent
}

func newReader(t *testing.T, ws *Workspace, rt runnerTool, sess, name string) *reader {
	ic := conversationIC{mockIC: &mockIC{ctx: context.Background()}, sess: stubSession{id: sess}, agent: namedAgent{name: name}}
	return &reader{t: t, ws: ws, rt: rt, ic: ic}
}

// read calls read_file and keeps its result for the next request.
func (r *reader) read(args map[string]any) map[string]any {
	r.t.Helper()
	r.calls++
	id := fmt.Sprintf("call-%d", r.calls)
	out, err := r.rt.Run(agent.NewToolContext(r.ic, id, &session.EventActions{}, nil), args)
	require.NoError(r.t, err)
	r.sent = append(r.sent, &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: id, Name: "read_file", Response: out}}}})
	return out
}

// request sends the agent the results it has so far.
func (r *reader) request() {
	r.ws.Reads().Shown(agent.NewCallbackContext(r.ic, &session.EventActions{}), r.sent)
}

// forget drops what the agent was sent, as a summary or a rewind does.
func (r *reader) forget() { r.sent = nil }

// handTo hands the conversation to agent to: it's sent this agent's
// results as text, as ADK shows another agent's events ("For context:").
func (r *reader) handTo(to *reader) {
	for _, c := range r.sent {
		for _, p := range c.Parts {
			resp, err := json.Marshal(p.FunctionResponse.Response)
			require.NoError(r.t, err)
			text := fmt.Sprintf("[%s] `%s` tool returned result: %s", r.ic.agent.Name(), p.FunctionResponse.Name, resp)
			to.sent = append(to.sent, &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{Text: "For context:"}, {Text: text}}})
		}
	}
}

func TestReadFileRepeats(t *testing.T) {
	var long strings.Builder
	for i := 1; i <= wholeFileLines+50; i++ {
		fmt.Fprintf(&long, "line %d\n", i)
	}
	tests := []struct {
		name string
		// steps reads the file as agent a (b is another agent of the same
		// conversation) and returns the last read's result.
		steps         func(t *testing.T, a, b *reader, dir string) map[string]any
		wantUnchanged bool
		wantContent   string
	}{
		{name: "an unchanged file already shown answers unchanged", wantUnchanged: true,
			steps: func(t *testing.T, a, _ *reader, _ string) map[string]any {
				a.read(map[string]any{"path": "f.txt"})
				a.request()
				return a.read(map[string]any{"path": "./f.txt"})
			}},
		{name: "a range inside a read already shown answers unchanged", wantUnchanged: true,
			steps: func(t *testing.T, a, _ *reader, _ string) map[string]any {
				a.read(map[string]any{"path": "long.txt"})
				a.request()
				return a.read(map[string]any{"path": "long.txt", "start_line": 100, "end_line": 120})
			}},
		{name: "a read not yet sent to the model gives the content", wantContent: "   1: one\n",
			steps: func(t *testing.T, a, _ *reader, _ string) map[string]any {
				a.read(map[string]any{"path": "f.txt"})
				return a.read(map[string]any{"path": "f.txt"})
			}},
		{name: "a changed file gives the content", wantContent: "   1: ONE\n",
			steps: func(t *testing.T, a, _ *reader, dir string) map[string]any {
				a.read(map[string]any{"path": "f.txt"})
				a.request()
				writeFile(t, filepath.Join(dir, "f.txt"), "ONE\ntwo\n")
				return a.read(map[string]any{"path": "f.txt"})
			}},
		{name: "after a summary or a rewind the content comes again", wantContent: "   1: one\n",
			steps: func(t *testing.T, a, _ *reader, _ string) map[string]any {
				a.read(map[string]any{"path": "f.txt"})
				a.request()
				a.forget()
				a.request()
				return a.read(map[string]any{"path": "f.txt"})
			}},
		{name: "another agent's read not handed over doesn't count", wantContent: "   1: one\n",
			steps: func(t *testing.T, a, b *reader, _ string) map[string]any {
				a.read(map[string]any{"path": "f.txt"})
				a.request()
				return b.read(map[string]any{"path": "f.txt"})
			}},
		{name: "a read handed over answers unchanged", wantUnchanged: true,
			steps: func(t *testing.T, a, b *reader, _ string) map[string]any {
				a.read(map[string]any{"path": "f.txt"})
				a.request()
				a.handTo(b)
				b.request()
				return b.read(map[string]any{"path": "f.txt"})
			}},
		{name: "a read handed over and summarized away gives the content", wantContent: "   1: one\n",
			steps: func(t *testing.T, a, b *reader, _ string) map[string]any {
				a.read(map[string]any{"path": "f.txt"})
				a.handTo(b)
				b.request()
				b.forget()
				b.request()
				return b.read(map[string]any{"path": "f.txt"})
			}},
		{name: "force gives the content", wantContent: "   1: one\n",
			steps: func(t *testing.T, a, _ *reader, _ string) map[string]any {
				a.read(map[string]any{"path": "f.txt"})
				a.request()
				return a.read(map[string]any{"path": "f.txt", "force": true})
			}},
		{name: "lines outside what was shown give the content", wantContent: "  60: line 60\n",
			steps: func(t *testing.T, a, _ *reader, _ string) map[string]any {
				a.read(map[string]any{"path": "long.txt", "start_line": 1, "end_line": 50})
				a.request()
				return a.read(map[string]any{"path": "long.txt", "start_line": 60, "end_line": 80})
			}},
		{name: "a read stays shown while later requests carry it", wantUnchanged: true,
			steps: func(t *testing.T, a, _ *reader, _ string) map[string]any {
				a.read(map[string]any{"path": "f.txt"})
				a.request()
				a.read(map[string]any{"path": "long.txt", "start_line": 1, "end_line": 5})
				a.request()
				return a.read(map[string]any{"path": "f.txt"})
			}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ws, dir := newTestWorkspace(t)
			writeFile(t, filepath.Join(dir, "f.txt"), "one\ntwo\n")
			writeFile(t, filepath.Join(dir, "long.txt"), long.String())
			rt := toolOf(t)(NewReadFileTool(ws))
			a := newReader(t, ws, rt, "s/1", "coder")
			b := newReader(t, ws, rt, "s/1", "reviewer")
			out := tc.steps(t, a, b, dir)
			require.Empty(t, errOf(out))
			if tc.wantUnchanged {
				assert.Equal(t, true, out["unchanged"])
				assert.Empty(t, out["content"])
				assert.Contains(t, out["note"], "Unchanged since your earlier read")
				return
			}
			assert.Nil(t, out["unchanged"])
			assert.True(t, strings.HasPrefix(out["content"].(string), tc.wantContent), "content %q", out["content"])
		})
	}
}

func TestReadKey(t *testing.T) {
	_, ok := ReadKey(createTestToolContext())
	assert.False(t, ok, "a call outside a conversation has no key")
	_, ok = ReadKey(nil)
	assert.False(t, ok)
	ic := conversationIC{mockIC: &mockIC{ctx: context.Background()}, sess: stubSession{id: "s/1"}, agent: namedAgent{name: "coder"}}
	key, ok := ReadKey(agent.NewToolContext(ic, "c", &session.EventActions{}, nil))
	assert.True(t, ok)
	assert.Equal(t, "s/1\x00\x00coder", key)
}

func TestReadLogForgetsTheOldest(t *testing.T) {
	var l ReadLog
	result := []*genai.Content{{Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{Name: "read_file", Response: map[string]any{"digest": "d", "lines": "1-2"}}}}}}
	ctxOf := func(sess string) agent.Context {
		return agent.NewCallbackContext(conversationIC{mockIC: &mockIC{ctx: context.Background()}, sess: stubSession{id: sess}, agent: namedAgent{name: "coder"}}, &session.EventActions{})
	}
	for i := range maxReadLogAgents + 1 {
		l.Shown(ctxOf(fmt.Sprint(i)), result)
	}
	assert.Len(t, l.agents, maxReadLogAgents)
	assert.NotContains(t, l.agents, "0\x00\x00coder")
	l.Shown(ctxOf("x"), nil) // nothing read: nothing kept
	assert.NotContains(t, l.agents, "x\x00\x00coder")
	l.Shown(ctxOf("1"), nil) // the summary took them: none shown
	assert.Empty(t, l.agents["1\x00\x00coder"])

	var none *ReadLog
	none.Shown(nil, nil)
	_, ok := none.covering("k", "d", 1, 1)
	assert.False(t, ok)
}

func TestShownRead(t *testing.T) {
	tests := []struct {
		name string
		resp map[string]any
		want fileRead
		ok   bool
	}{
		{"a read", map[string]any{"digest": "d", "lines": "3-9"}, fileRead{digest: "d", start: 3, end: 9}, true},
		{"an unchanged answer", map[string]any{"unchanged": true, "note": "Unchanged"}, fileRead{}, false},
		{"no lines", map[string]any{"digest": "d"}, fileRead{}, false},
		{"no digest", map[string]any{"lines": "1-2"}, fileRead{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := shownRead(tc.resp)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestHandedRead(t *testing.T) {
	tests := []struct {
		name, text string
		want       map[string]any
	}{
		{"a read handed over", "[coder] `read_file` tool returned result: {\"digest\":\"d\",\"lines\":\"1-2\"}", map[string]any{"digest": "d", "lines": "1-2"}},
		{"another tool", "[coder] `list_files` tool returned result: {\"digest\":\"d\"}", nil},
		{"words that only quote it", "I saw `read_file` tool returned result: {}", nil},
		{"a result that isn't JSON", "[coder] `read_file` tool returned result: {cut", nil},
		{"plain text", "For context:", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, handedRead(tc.text))
		})
	}
}

func TestReadFileSuggestsPaths(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	writeFile(t, filepath.Join(dir, "frontend", "src", "views", "CatalogView.tsx"), "x")
	writeFile(t, filepath.Join(dir, "frontend", "src", "views", "CatalogViewTest.tsx"), "x")
	writeFile(t, filepath.Join(dir, "node_modules", "CatalogView.tsx"), "x")
	writeFile(t, filepath.Join(dir, ".cache", "CatalogView.tsx"), "x")
	rt := toolOf(t)(NewReadFileTool(ws))
	tests := []struct {
		path, want string
	}{
		{"src/CatalogView.tsx", "did you mean frontend/src/views/CatalogView.tsx, frontend/src/views/CatalogViewTest.tsx?"},
		{"Nothing.go", "no such file"},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			err := errOf(runTool(t, rt, map[string]any{"path": tc.path}))
			assert.Contains(t, err, tc.want)
			assert.NotContains(t, err, "node_modules")
			assert.NotContains(t, err, ".cache")
		})
	}
}
