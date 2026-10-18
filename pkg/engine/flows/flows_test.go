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

package flows

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const brief = `description: A one-page brief from the notes
nodes:
  - id: notes
    type: read_file
    title: Read the notes
    position: {x: 0, y: 0}
    path: notes/topic.md
  - id: web
    type: browser
    title: Look it up
    position: {x: 0, y: 120}
    instructions: Find the latest figures.
  - id: both
    type: accumulate
    position: {x: 240, y: 60}
  - id: draft
    type: agent
    title: Draft
    position: {x: 480, y: 60}
    agent: writer
    skills: [tone]
    rules: [cites-sources]
    instructions: Write a one-page brief.
  - id: save
    type: save_file
    position: {x: 720, y: 60}
    path: briefs/topic.md
edges:
  - {from: notes, to: both}
  - {from: web, to: both}
  - {from: both, to: draft}
  - {from: draft, to: save}
`

func TestParseAndOrder(t *testing.T) {
	f := Parse("brief", "/x/brief.flow.yaml", []byte(brief))
	require.Empty(t, f.Problem)
	assert.Equal(t, "A one-page brief from the notes", f.Description)
	require.Len(t, f.Nodes, 5)
	assert.Equal(t, Node{ID: "draft", Type: Agent, Title: "Draft", Position: Position{480, 60}, Agent: "writer", Skills: []string{"tone"}, Rules: []string{"cites-sources"}, Instructions: "Write a one-page brief."}, f.Nodes[3])
	order, err := Order(f)
	require.NoError(t, err)
	var ids []string
	for _, n := range order {
		ids = append(ids, n.ID)
	}
	assert.Equal(t, []string{"notes", "web", "both", "draft", "save"}, ids)
	assert.Equal(t, []string{"notes", "web"}, Inputs(f, "both"))
	assert.Empty(t, Inputs(f, "notes"))
}

func TestCheck(t *testing.T) {
	tests := []struct{ name, in, problem string }{
		{"not yaml", "nodes: [", "isn't a workflow"},
		{"no steps", "description: x\n", "it has no steps"},
		{"bad type", "nodes: [{id: a, type: dance}]", `type "dance"`},
		{"coming soon", "nodes: [{id: a, type: email}]", "email steps don't run yet"},
		{"agent without instructions", "nodes: [{id: a, type: agent}]", "say what it's to do"},
		{"script without one", "nodes: [{id: a, type: script, skill: x}]", "name the skill and its script"},
		{"file without a path", "nodes: [{id: a, type: read_file}]", "name the file"},
		{"no ID", "nodes: [{type: accumulate}]", "it needs an ID"},
		{"same ID", "nodes: [{id: a, type: accumulate}, {id: a, type: accumulate}]", "another step has that ID"},
		{"bad link", "nodes: [{id: a, type: accumulate}]\nedges: [{from: a, to: b}]", "no such step"},
		{"self link", "nodes: [{id: a, type: accumulate}]\nedges: [{from: a, to: a}]", "can't feed itself"},
		{"loop", "nodes: [{id: a, type: accumulate}, {id: b, type: accumulate}]\nedges: [{from: a, to: b}, {from: b, to: a}]", "the steps loop"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Contains(t, Parse("x", "", []byte(tt.in)).Problem, tt.problem)
		})
	}
	assert.Contains(t, Parse("Bad", "", []byte("nodes: [{id: a, type: accumulate}]")).Problem, "name")
}

func TestSaveLoadDelete(t *testing.T) {
	dir := t.TempDir()
	f := Parse("brief", "", []byte(brief))
	p, probs, err := Save(dir, f, "")
	require.NoError(t, err)
	require.Empty(t, probs)
	assert.Equal(t, filepath.Join(dir, "brief.flow.yaml"), p)
	back := Load(dir)
	require.Len(t, back, 1)
	assert.Equal(t, f.Nodes, back[0].Nodes)
	assert.Equal(t, f.Edges, back[0].Edges)
	assert.Empty(t, back[0].Problem)

	// A draft (a step not filled in) is saved, and is its problem.
	draft := Flow{Name: "draft", Nodes: []Node{{ID: "a", Type: Agent}, {ID: "b", Type: Email}}}
	_, probs, err = Save(dir, draft, "")
	require.NoError(t, err)
	assert.Empty(t, probs)
	assert.Contains(t, Load(dir)[1].Problem, "say what it's to do")

	_, probs, _ = Save(dir, Flow{Name: "draft", Nodes: []Node{{ID: "a", Type: Accumulate}}}, "")
	assert.Equal(t, []string{`name "draft": there's a workflow by that name`}, probs)
	_, probs, _ = Save(dir, Flow{Name: "x", Nodes: []Node{{ID: "a", Type: "dance"}}}, "")
	assert.NotEmpty(t, probs, "a bad type isn't saved")
	_, _, err = Save(dir, Flow{Name: "y"}, "gone")
	assert.ErrorIs(t, err, ErrUnknown)

	f.Name = "renamed"
	_, probs, err = Save(dir, f, "brief")
	require.NoError(t, err)
	require.Empty(t, probs)
	assert.NoFileExists(t, filepath.Join(dir, "brief.flow.yaml"))
	require.NoError(t, Delete(dir, "renamed"))
	assert.ErrorIs(t, Delete(dir, "renamed"), ErrUnknown)
	assert.ErrorIs(t, Delete(dir, "../x"), ErrUnknown)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.md"), []byte("a command"), 0o644))
	assert.Len(t, Load(dir), 1, "the .md commands aren't workflows")
	assert.True(t, IsFlow("x.FLOW.yaml"))
}
