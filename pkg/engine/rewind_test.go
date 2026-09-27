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
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"google.golang.org/genai"
)

// rewindSession runs two prompts: the first creates notes.txt, the second
// edits it. It returns the workspace and its config.
func rewindSession(t *testing.T, extra ...*genai.Content) (*Workspace, *config.Config, *runtime.MockLLM, string) {
	t.Helper()
	var cfg *config.Config
	replies := []*genai.Content{
		toolCall("create_file", map[string]any{"path": "notes.txt", "content": "v1\n"}), text("created"),
		toolCall("replace_in_file", map[string]any{"path": "notes.txt", "target_content": "v1", "replacement_content": "v2"}), text("edited"),
	}
	w, llm := openTestWith(t, func(c *config.Config) { cfg = c }, append(replies, extra...)...)
	w.SetUI(func(context.Context, api.ApprovalRequest) (api.Decision, error) { return api.DecisionOnce, nil }, nil)
	s, _, err := w.OpenSession("", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"make notes", "second draft"} {
		if _, err := w.Run(context.Background(), s.ID, api.Turn{Text: p}, func(api.Event) {}); err != nil {
			t.Fatal(err)
		}
	}
	return w, cfg, llm, s.ID
}

func notes(t *testing.T, cfg *config.Config) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(cfg.Tools.WorkspaceDir, "notes.txt"))
	if err != nil {
		return "<none>"
	}
	return string(b)
}

func TestRewindPoints(t *testing.T) {
	w, _, _, id := rewindSession(t)
	if err := w.Steer(context.Background(), id, "a steer message"); err != nil {
		t.Fatal(err)
	}
	points, err := w.RewindPoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 2 || points[0].Text != "make notes" || points[1].Text != "second draft" || points[0].Index != 0 || points[1].Index != 2 {
		t.Fatalf("points %+v", points)
	}
	for _, p := range points {
		if !p.Conversation || len(p.Files) != 1 || p.Files[0] != "notes.txt" {
			t.Errorf("point %+v", p)
		}
	}
}

func TestRewindCodeAndConversation(t *testing.T) {
	w, cfg, llm, id := rewindSession(t, text("redone"))
	res, err := w.Rewind(context.Background(), 2, api.RewindBoth, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Prompt != "second draft" || len(res.Restored) != 1 || notes(t, cfg) != "v1\n" {
		t.Fatalf("rewind %+v, notes %q", res, notes(t, cfg))
	}
	a, _ := w.ActiveSession()
	if a.MessageCount != 2 {
		t.Errorf("transcript has %d messages, want 2", a.MessageCount)
	}
	if _, err := w.Run(context.Background(), id, api.Turn{Text: "third try"}, func(api.Event) {}); err != nil {
		t.Fatal(err)
	}
	var sent strings.Builder
	for _, c := range llm.Requests[len(llm.Requests)-1].Contents {
		for _, p := range c.Parts {
			sent.WriteString(p.Text + "|")
		}
	}
	if strings.Contains(sent.String(), "second draft") || !strings.Contains(sent.String(), "make notes") {
		t.Errorf("the model still sees the rewound prompt: %s", sent.String())
	}
	if points, _ := w.RewindPoints(); len(points) != 2 || points[1].Text != "third try" {
		t.Errorf("points after rewinding %+v", points)
	}
}

func TestRewindConversationOnlyKeepsFiles(t *testing.T) {
	w, cfg, _, id := rewindSession(t, text("nothing to change"))
	if _, err := w.Rewind(context.Background(), 2, api.RewindConversation, false); err != nil {
		t.Fatal(err)
	}
	if notes(t, cfg) != "v2\n" {
		t.Errorf("files changed: %q", notes(t, cfg))
	}
	// A new prompt takes the rewound one's place; rewinding its code leaves
	// the kept change, which came before it.
	if _, err := w.Run(context.Background(), id, api.Turn{Text: "look around"}, func(api.Event) {}); err != nil {
		t.Fatal(err)
	}
	if res, err := w.Rewind(context.Background(), 2, api.RewindCode, false); err != nil || len(res.Restored) != 0 || notes(t, cfg) != "v2\n" {
		t.Fatalf("rewinding the new prompt: %+v %v, notes %q", res, err, notes(t, cfg))
	}
	// The kept change now belongs before the next prompt: rewinding the
	// first prompt's code still restores through it.
	if _, err := w.Rewind(context.Background(), 0, api.RewindCode, false); err != nil {
		t.Fatal(err)
	}
	if notes(t, cfg) != "<none>" {
		t.Errorf("notes after rewinding the first prompt: %q", notes(t, cfg))
	}
}

func TestRewindCodeOnlyKeepsTheConversation(t *testing.T) {
	w, cfg, _, _ := rewindSession(t)
	if _, err := w.Rewind(context.Background(), 2, api.RewindCode, false); err != nil {
		t.Fatal(err)
	}
	if a, _ := w.ActiveSession(); a.MessageCount != 4 || notes(t, cfg) != "v1\n" {
		t.Errorf("messages %d, notes %q", a.MessageCount, notes(t, cfg))
	}
}

func TestRewindRefusals(t *testing.T) {
	w, cfg, _, id := rewindSession(t)
	ctx := context.Background()
	for _, c := range []struct {
		index int
		mode  api.RewindMode
		want  error
	}{
		{1, api.RewindBoth, api.ErrNotRewindPoint}, // a reply
		{9, api.RewindBoth, api.ErrNotRewindPoint},
		{0, "sideways", api.ErrUnknownRewindMode},
	} {
		if _, err := w.Rewind(ctx, c.index, c.mode, false); !errors.Is(err, c.want) {
			t.Errorf("%d %s: %v", c.index, c.mode, err)
		}
	}
	// A file changed since: nothing happens, not even to the conversation.
	os.WriteFile(filepath.Join(cfg.Tools.WorkspaceDir, "notes.txt"), []byte("mine\n"), 0o644)
	if _, err := w.Rewind(ctx, 2, api.RewindBoth, false); !errors.Is(err, api.ErrUndoConflict) {
		t.Fatalf("conflict: %v", err)
	}
	if a, _ := w.ActiveSession(); a.MessageCount != 4 {
		t.Error("a refused rewind changed the conversation")
	}
	w.turnStarted(id)
	if _, err := w.Rewind(ctx, 2, api.RewindCode, true); !errors.Is(err, api.ErrSessionBusy) {
		t.Errorf("during a turn: %v", err)
	}
	w.turnEnded(id)
	if _, err := w.Rewind(ctx, 2, api.RewindBoth, true); err != nil || notes(t, cfg) != "v1\n" {
		t.Errorf("forced: %v %q", err, notes(t, cfg))
	}
}

// The conversation can be rewound after the workspace was closed and the
// session resumed.
func TestRewindAfterResume(t *testing.T) {
	w, cfg, _, id := rewindSession(t)
	w.Close()
	w2, err := Open(context.Background(), cfg, Options{Model: runtime.NewMockLLM("gemini-3.8-flash"), NewModel: mockModels})
	if err != nil {
		t.Fatal(err)
	}
	defer w2.Close()
	if _, _, err := w2.OpenSession(id, false); err != nil {
		t.Fatal(err)
	}
	res, err := w2.Rewind(context.Background(), 2, api.RewindBoth, false)
	if err != nil || notes(t, cfg) != "v1\n" || res.Prompt != "second draft" {
		t.Fatalf("%+v %v %q", res, err, notes(t, cfg))
	}
	w2.Close()
	w3, err := Open(context.Background(), cfg, Options{Model: runtime.NewMockLLM("gemini-3.8-flash"), NewModel: mockModels})
	if err != nil {
		t.Fatal(err)
	}
	defer w3.Close()
	s, _, err := w3.OpenSession(id, false)
	if err != nil || s.MessageCount != 2 {
		t.Fatalf("reopened: %d messages, %v", s.MessageCount, err)
	}
}
