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

package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/genai"
)

// A command file is a slash command: /help lists it, typing it runs its
// prompt, and a built-in command of the same name still wins.
func TestCustomCommandInTheREPL(t *testing.T) {
	app, llm := newCommandApp(t, "/help\n/greet Ada\n/nope\n/exit\n", genai.NewContentFromText("hi Ada", genai.RoleModel))
	dir := filepath.Join(local(app).Dir(), ".blitz", "commands")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "greet.md"), []byte("---\ndescription: Greet someone\nargument-hint: <name>\n---\nSay hello to $1."), 0o644)
	os.WriteFile(filepath.Join(dir, "help.md"), []byte("I shadow /help."), 0o644)
	out := captureStdout(t, func() { RunREPL(context.Background(), app) })

	if !strings.Contains(out, "Custom commands") || !strings.Contains(out, "/greet <name>") || !strings.Contains(out, "Greet someone") {
		t.Errorf("/help doesn't list the command:\n%s", out)
	}
	if llm.Calls() != 1 || !strings.Contains(requestTextAt(llm, 0), "Say hello to Ada.") {
		t.Fatalf("the command's prompt wasn't sent (%d calls)", llm.Calls())
	}
	if !strings.Contains(out, "Unknown command") {
		t.Errorf("/nope wasn't reported unknown:\n%s", out)
	}
}
