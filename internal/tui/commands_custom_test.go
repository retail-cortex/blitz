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
