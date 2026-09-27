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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/config"
	"google.golang.org/genai"
)

func writeCommand(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
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
	if byName["hello"].Source != "project" || byName["db:migrate"].Description != "Migrate" || byName["review"].Source != "bundled" {
		t.Fatalf("commands %+v", byName)
	}
	if _, ok := byName["broken"]; ok {
		t.Error("a broken command file was listed")
	}
	if byName["code-review"].Source != "skill" { // a built-in skill, runnable by name
		t.Errorf("skills as commands: %+v", byName["code-review"])
	}

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
	if _, err := run("/hello world"); err != nil {
		t.Fatal(err)
	}
	if got := userTextAt(llm.Requests[0].Contents); got != "Project hello world." {
		t.Errorf("expanded %q", got)
	}
	// allowed-tools limits the turn: create_file is refused, even in accept-edits.
	result, _ := run("/db:migrate users")
	if msg, _ := result["error"].(string); !strings.Contains(msg, "isn't among the tools this command allows") {
		t.Errorf("allowed-tools not applied: %v", result)
	}
	if _, err := os.Stat(filepath.Join(w.Dir(), "x.txt")); err == nil {
		t.Error("the command's turn created a file it wasn't allowed to")
	}
	// A bundled plan-mode command: read-only, and the prompt is the review
	// instructions as written (not a request for a plan).
	result, _ = run("/review")
	if got := userTextAt(llm.Requests[3].Contents); strings.Contains(got, "plan-only mode") || !strings.Contains(got, "Review code changes") {
		t.Errorf("review prompt %q", got)
	}
	if msg, _ := result["error"].(string); !strings.Contains(msg, "/review is read-only") {
		t.Errorf("/review could write: %v", result)
	}
	// The transcript records the command as typed.
	msgs := w.storage.Active().Messages
	if msgs[0].Content != "/hello world" {
		t.Errorf("transcript %q", msgs[0].Content)
	}
	if _, err := run("/nope"); !errors.Is(err, api.ErrUnknownCommand) {
		t.Errorf("unknown command: %v", err)
	}
}
