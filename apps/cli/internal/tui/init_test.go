package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/genai"
)

// /init sends the instructions-writing prompt, records "/init", and loads
// the BLITZ.md the agent wrote before the next prompt.
func TestInitWritesAndLoadsBlitzMD(t *testing.T) {
	write := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{
		Name: "create_file", Args: map[string]any{"path": "BLITZ.md", "content": "Run tests with `make check`."}}}}}
	app, llm := newCommandApp(t, "/init\nhello\n/exit\n", write, genai.NewContentFromText("wrote BLITZ.md", genai.RoleModel), genai.NewContentFromText("hi", genai.RoleModel))
	out := captureStdout(t, func() { RunREPL(context.Background(), app) })

	if first := requestTextAt(llm, 0); !strings.Contains(first, "write BLITZ.md at the workspace root") {
		t.Fatalf("the init prompt wasn't sent:\n%s", first)
	}
	if !strings.Contains(out, filepath.Join(local(app).Dir(), "BLITZ.md")) {
		t.Errorf("memory wasn't reloaded after /init:\n%s", out)
	}
	// Memory is part of the system instruction, not the conversation.
	var system strings.Builder
	if cfg := llm.Requests[2].Config; cfg != nil && cfg.SystemInstruction != nil {
		for _, p := range cfg.SystemInstruction.Parts {
			system.WriteString(p.Text)
		}
	}
	if !strings.Contains(system.String(), "Run tests with `make check`.") {
		t.Errorf("the next prompt's instructions don't carry the new BLITZ.md")
	}
	if strings.Contains(requestTextAt(llm, 0), "Run tests with") {
		t.Error("BLITZ.md was loaded before /init wrote it")
	}
	var recorded []string
	for _, m := range local(app).Storage().Active().Messages {
		recorded = append(recorded, m.Content)
	}
	if len(recorded) == 0 || recorded[0] != "/init" {
		t.Errorf("transcript %q", recorded)
	}
}
