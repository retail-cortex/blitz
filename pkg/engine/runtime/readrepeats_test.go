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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// readCall is a model turn calling read_file on path, with no call ID as
// Gemini sends it.
func readCall(path string) *genai.Content {
	return &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "read_file", Args: map[string]any{"path": path}}}}}
}

// readResult is the read_file result among a turn's events.
func readResult(t *testing.T, evs []*session.Event) map[string]any {
	t.Helper()
	for _, ev := range evs {
		if ev.Content == nil {
			continue
		}
		for _, p := range ev.Content.Parts {
			if p.FunctionResponse != nil && p.FunctionResponse.Name == "read_file" {
				return p.FunctionResponse.Response
			}
		}
	}
	require.Fail(t, "no read_file result in the turn")
	return nil
}

// TestReadFileRepeatsThroughTheEngine runs read_file through the engine's
// before-model hook (FS-83): the second read of an unchanged file answers
// unchanged, a summary that drops the first read brings the content back,
// and so does a change to the file.
func TestReadFileRepeatsThroughTheEngine(t *testing.T) {
	f := newEngine(t,
		readCall("notes.txt"), genai.NewContentFromText("read it", genai.RoleModel),
		readCall("notes.txt"), genai.NewContentFromText("still have it", genai.RoleModel),
		genai.NewContentFromText("The agent read notes.txt.", genai.RoleModel), // the summary
		readCall("notes.txt"), genai.NewContentFromText("read it again", genai.RoleModel),
		readCall("notes.txt"), genai.NewContentFromText("unchanged", genai.RoleModel),
	)
	path := filepath.Join(f.cfg.Tools.WorkspaceDir, "notes.txt")
	require.NoError(t, os.WriteFile(path, []byte("alpha\nbeta\n"), 0o644))
	ctx := context.Background()

	turn := func(prompt string) map[string]any {
		t.Helper()
		evs, err := collect(t, f.eng, "s1", prompt)
		require.NoError(t, err)
		return readResult(t, evs)
	}

	first := turn("read notes.txt")
	assert.Contains(t, first["content"], "alpha")
	assert.Nil(t, first["unchanged"])

	second := turn("read it again")
	assert.Equal(t, true, second["unchanged"], "a repeat of an unchanged file the model was shown: %v", second)
	assert.Empty(t, second["content"])

	_, err := f.eng.Compact(ctx, "s1", "", 1)
	require.NoError(t, err)
	third := turn("and once more")
	assert.Contains(t, third["content"], "alpha", "the summary dropped the first read, so the content comes again: %v", third)

	require.NoError(t, os.WriteFile(path, []byte("gamma\n"), 0o644))
	fourth := turn("it changed")
	assert.Contains(t, fourth["content"], "gamma", "a changed file gives its content: %v", fourth)
}

// TestReadFileRepeatsAcrossAHandoff runs a real transfer_to_agent (FS-83):
// the agent handed to sees blitz's read as ADK's "For context:" text, the
// result whole, so its read of the unchanged file answers unchanged.
func TestReadFileRepeatsAcrossAHandoff(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{},
		readCall("notes.txt"),
		toolCall("transfer_to_agent", map[string]any{"agent_name": "qa"}),
		readCall("notes.txt"), textContent("qa has it"))
	require.NoError(t, os.WriteFile(filepath.Join(f.cfg.Tools.WorkspaceDir, "notes.txt"), []byte("alpha\nbeta\n"), 0o644))

	var reads []map[string]any
	var authors []string
	require.NoError(t, f.eng.Execute(context.Background(), "s", "read notes.txt, then let qa check it", func(ev *session.Event) error {
		if ev.Content == nil {
			return nil
		}
		for _, p := range ev.Content.Parts {
			if p.FunctionResponse != nil && p.FunctionResponse.Name == "read_file" {
				reads = append(reads, p.FunctionResponse.Response)
				authors = append(authors, ev.Author)
			}
		}
		return nil
	}))
	require.Len(t, reads, 2, "authors %v", authors)
	assert.Equal(t, []string{"blitz", "qa"}, authors)
	assert.Contains(t, reads[0]["content"], "alpha")
	assert.Equal(t, true, reads[1]["unchanged"], "qa was handed blitz's read: %v", reads[1])
}
