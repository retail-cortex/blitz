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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func writeCommand(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	os.MkdirAll(filepath.Dir(p), 0o755)
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
}

// userTextAt is the latest user text in a request's contents.
func userTextAt(contents []*genai.Content) string {
	for i := len(contents) - 1; i >= 0; i-- {
		if c := contents[i]; c.Role == genai.RoleUser {
			var sb strings.Builder
			for _, p := range c.Parts {
				sb.WriteString(p.Text)
			}
			return sb.String()
		}
	}
	return ""
}

func TestCustomCommands(t *testing.T) {
	create := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "create_file", Args: map[string]any{"path": "x.txt", "content": "x"}}}}}
	w, llm := openTestWith(t, func(c *config.Config) { c.Blitz.PermissionMode = "accept-edits" }, text("done"), create, text("ok"), create, text("reviewed"))
	writeCommand(t, config.ExpandHome("~/.blitz/commands"), "hello.md", "User hello $1.")
	writeCommand(t, w.Dir(), ".claude/commands/hello.md", "Project hello $ARGUMENTS.") // project wins
	writeCommand(t, w.Dir(), ".blitz/commands/db/migrate.md", "---\ndescription: Migrate\nallowed-tools: Read, Grep\n---\nMigrate $1.")
	writeCommand(t, w.Dir(), ".agents/workflows/broken.md", "---\nmode: fast\n---\nx")

	byName := map[string]api.CommandInfo{}
	for _, c := range w.ListCommands() {
		byName[c.Name] = c
	}
	require.Equal(t, "project", byName["hello"].Source, "commands %+v", byName)
	require.Equal(t, "Migrate", byName["db:migrate"].Description, "commands %+v", byName)
	require.Equal(t, "bundled", byName["review"].Source, "commands %+v", byName)
	_, ok := byName["broken"]
	assert.False(t, ok, "a broken command file was listed")
	assert.Equal(t, "skill", byName["code-review"].Source, "skills as commands: %+v", byName["code-review"])

	s, _ := w.NewSession()
	run := func(line string) (map[string]any, error) {
		var result map[string]any
		_, err := w.Run(context.Background(), s.ID, api.Turn{Text: line, Command: true}, func(e api.Event) {
			if e.ToolResult != nil {
				result = e.ToolResult.Result
			}
		})
		return result, err
	}
	_, err := run("/hello world")
	require.NoError(t, err)
	got := userTextAt(llm.Requests[0].Contents)
	assert.Equal(t, "Project hello world.", got, "expanded %q", got)
	// allowed-tools limits the turn: create_file is refused, even in accept-edits.
	result, _ := run("/db:migrate users")
	msg, _ := result["error"].(string)
	assert.Contains(t, msg, "isn't among the tools this command allows", "allowed-tools not applied: %v", result)
	_, err = os.Stat(filepath.Join(w.Dir(), "x.txt"))
	assert.Error(t, err, "the command's turn created a file it wasn't allowed to")
	// A bundled plan-mode command: read-only, and the prompt is the review
	// instructions as written (not a request for a plan).
	result, _ = run("/review")
	got = userTextAt(llm.Requests[3].Contents)
	assert.NotContains(t, got, "plan-only mode", "review prompt %q", got)
	assert.Contains(t, got, "Review code changes", "review prompt %q", got)
	msg, _ = result["error"].(string)
	assert.Contains(t, msg, "/review is read-only", "/review could write: %v", result)
	// The transcript records the command as typed.
	msgs := w.storage.Active().Messages
	assert.Equal(t, "/hello world", msgs[0].Content, "transcript %q", msgs[0].Content)
	_, err = run("/nope")
	assert.ErrorIs(t, err, api.ErrUnknownCommand, "unknown command: %v", err)
}

// A command's frontmatter picks the agent and model of its turn; an
// unknown agent or a model that can't be built is an error naming it.
func TestCommandAgentAndModel(t *testing.T) {
	w := openTest(t)
	writeCommand(t, w.Dir(), ".blitz/commands/qa.md", "---\nagent: qa\nmodel: gemini-3.8-pro\n---\nTest $1.")
	writeCommand(t, w.Dir(), ".blitz/commands/ghost.md", "---\nagent: ghost\n---\nBoo.")
	writeCommand(t, w.Dir(), ".blitz/commands/broken.md", "---\nmodel: broken\n---\nx")
	ctx := context.Background()

	turn := &api.Turn{Text: "/qa cart"}
	opts, err := w.expandCommand(ctx, turn)
	require.NoError(t, err)
	assert.Equal(t, "Test cart.", turn.Prompt)
	assert.Len(t, opts, 2, "the agent and the model")

	_, err = w.expandCommand(ctx, &api.Turn{Text: "/ghost"})
	var unknown *api.UnknownAgentError
	assert.ErrorAs(t, err, &unknown)
	_, err = w.expandCommand(ctx, &api.Turn{Text: "/broken"})
	assert.ErrorContains(t, err, `/broken: model "broken": no such model`)
}

// A skill whose name isn't a valid command, or that a bundled command
// already has, isn't a command.
func TestSkillsAsCommandsSkipClashes(t *testing.T) {
	w := openTest(t)
	dir := t.TempDir()
	for name, desc := range map[string]string{"review": "a skill named like a bundled command", "Bad_Name!": "invalid"} {
		writeCommand(t, dir, name+"/SKILL.md", "---\nname: "+name+"\ndescription: "+desc+"\n---\nDo it.")
	}
	require.NoError(t, w.Skills().DiscoverExternal([]string{dir}))
	byName, err := w.commands()
	require.NoError(t, err)
	assert.Equal(t, "bundled", byName["review"].Source)
	_, ok := byName["bad_name!"]
	assert.False(t, ok, "an invalid name became a command")
}
