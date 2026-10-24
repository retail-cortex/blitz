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

package engine

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/compose"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
)

func composeDraft(name, file string) string {
	data, _ := json.Marshal(map[string]any{"draft": map[string]any{"name": name, "summary": "Reads the notes and saves a brief.", "file": file}})
	return string(data)
}

const goodFlow = "description: A brief from the notes\nnodes:\n  - {id: notes, type: read_file, path: notes.md, position: {x: 0, y: 80}}\n  - {id: draft, type: agent, instructions: Write a brief., position: {x: 280, y: 80}}\nedges:\n  - {from: notes, to: draft}\n"

// The composer makes a workflow: a question, an answer, a draft checked by
// the workflow's own rules (a broken one sent back and fixed), from a
// prompt that names the workspace's agents and workflows.
func TestComposeWorkflow(t *testing.T) {
	w, _ := openTestWith(t, nil)
	writeFlow(t, w, "existing", "description: Already here\nnodes:\n  - {id: a, type: accumulate}\n")
	llm := runtime.NewMockLLM("composer",
		text(`{"question": "Where are the notes?", "options": ["notes.md", "docs/"]}`),
		text(composeDraft("brief", "nodes:\n  - {id: a, type: dance}\n")),
		text(composeDraft("brief", goodFlow)))
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return llm, nil }

	s, err := w.StartCompose(context.Background(), "workflow", "a brief from my notes, every morning", "")
	require.NoError(t, err)
	assert.True(t, s.Asking())
	assert.Equal(t, []string{"notes.md", "docs/"}, s.Turns[1].Options)
	sent := llm.Requests[0].Contents[0].Parts[0].Text
	assert.Contains(t, sent, "existing: Already here", "the workspace's workflows")
	assert.Contains(t, sent, "Agents:")
	assert.Contains(t, sent, "a brief from my notes")

	s, err = w.AnswerCompose(context.Background(), s.ID, "notes.md")
	require.NoError(t, err)
	require.NotNil(t, s.Draft)
	assert.Empty(t, s.Draft.Problems, "fixed after the first draft's problem went back")
	assert.Equal(t, "brief", s.Draft.Name)
	assert.Contains(t, llm.Requests[2].Contents[0].Parts[0].Text, `type "dance"`)

	_, err = w.AnswerCompose(context.Background(), "compose-nope", "x")
	assert.ErrorIs(t, err, compose.ErrNoSession)
	_, err = w.StartCompose(context.Background(), "spaceship", "x", "")
	assert.ErrorIs(t, err, ErrNoRecipe)
	w.DropCompose(s.ID)
	_, err = w.AnswerCompose(context.Background(), s.ID, "again")
	assert.ErrorIs(t, err, compose.ErrNoSession)
	assert.Equal(t, []string{"workflow", "job"}, ComposeKinds())
}

// A job composed in a conversation (WK-74): a draft that doesn't load goes
// back with its problems, and the one that does is the job.
func TestComposeJob(t *testing.T) {
	w, _ := openTestWith(t, nil)
	good := "---\ndescription: AI news every morning\nschedule: \"0 8 * * *\"\npermissions: [\"web:news.google.com\", \"write:reports/ai-news/**\"]\n---\nFetch the AI news and write reports/ai-news/MM-DD-YYYY.html.\n"
	llm := runtime.NewMockLLM("composer",
		text(`{"question": "Which time zone?", "options": ["America/Chicago", "The service's"]}`),
		text(composeDraft("ai-news", "---\nschedule: someday\n---\nFetch.\n")),
		text(composeDraft("ai-news", good)))
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return llm, nil }
	s, err := w.StartCompose(context.Background(), "job", "every day at 8 fetch the AI news into reports", "")
	require.NoError(t, err)
	assert.True(t, s.Asking())
	assert.Contains(t, llm.Requests[0].Contents[0].Parts[0].Text, "WORKER.md", "the job's brief")
	s, err = w.AnswerCompose(context.Background(), s.ID, "America/Chicago")
	require.NoError(t, err)
	require.NotNil(t, s.Draft)
	assert.Empty(t, s.Draft.Problems, "fixed after the unreadable schedule went back")
	assert.Equal(t, "ai-news", s.Draft.Name)
	assert.Contains(t, llm.Requests[2].Contents[0].Parts[0].Text, "someday")
}

func TestCheckJobDraft(t *testing.T) {
	w, _ := openTestWith(t, nil)
	good := "---\nschedule: \"@daily\"\n---\nDo it.\n"
	assert.Empty(t, w.checkJobDraft("daily", good))
	assert.Contains(t, w.checkJobDraft("Bad Name", good)[0], "name")
	assert.NotEmpty(t, w.checkJobDraft("daily", "---\nschedule: whenever\n---\nDo it.\n"), "an unreadable schedule")
	assert.NotEmpty(t, w.checkJobDraft("daily", "---\nschedule: \"@daily\"\nagent: nobody\n---\nDo it.\n"), "an unknown agent")
	assert.NotEmpty(t, w.checkJobDraft("daily", "---\nschedule: [\n---\nx\n"), "frontmatter that isn't YAML")
}

func TestCheckWorkflowDraft(t *testing.T) {
	assert.Empty(t, checkWorkflowDraft("brief", goodFlow))
	assert.Contains(t, checkWorkflowDraft("Bad Name", goodFlow)[0], "name")
	assert.Contains(t, checkWorkflowDraft("brief", "nodes: [")[0], "isn't a workflow")
	assert.NotEmpty(t, checkWorkflowDraft("brief", "description: none\n"))
}

// A model that can't be built, and a first turn that fails, leave no
// session behind.
func TestComposeModelErrors(t *testing.T) {
	w, _ := openTestWith(t, nil)
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return nil, os.ErrPermission }
	_, err := w.StartCompose(context.Background(), "workflow", "x", "")
	assert.Error(t, err)
	llm := runtime.NewMockLLM("composer", text("not json"))
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return llm, nil }
	_, err = w.StartCompose(context.Background(), "workflow", "x", "")
	assert.ErrorIs(t, err, compose.ErrNoReply)
}
