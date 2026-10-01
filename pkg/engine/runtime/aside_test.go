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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cpsession "github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func requestText(req *model.LLMRequest) string {
	var b strings.Builder
	for _, c := range req.Contents {
		for _, p := range c.Parts {
			b.WriteString(p.Text + "\n")
			if p.FunctionResponse != nil {
				fmt.Fprintf(&b, "%s: %v\n", p.FunctionResponse.Name, p.FunctionResponse.Response)
			}
		}
	}
	return b.String()
}

// A side question sees the session's history, but leaves no trace in it:
// not in the next turn's request, not in the saved event log.
func TestAsideSeesHistoryAndLeavesNoTrace(t *testing.T) {
	dir := t.TempDir()
	svc, err := cpsession.NewPersistentService(dir)
	require.NoError(t, err)
	f := newEngineWith(t, fixtureOpts{opts: []Option{WithSessionService(svc)}},
		textContent("noted: pineapple"),
		toolCall("create_file", map[string]any{"path": "x.txt", "content": "x"}),
		textContent("the word was pineapple"),
		textContent("next answer"))
	f.llm.Usage = &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 10, CandidatesTokenCount: 2}
	ctx := context.Background()
	_, turnErr := collect(t, f.eng, "s", "remember pineapple")
	require.NoError(t, turnErr)
	logPath := filepath.Join(dir, "s.events.jsonl")
	before, _ := os.ReadFile(logPath)
	usage := f.eng.Usage("s").Calls

	var answer strings.Builder
	err = f.eng.Aside(ctx, "s", "btw, what was the word?", func(ev *session.Event) error {
		if ev.Content != nil && !ev.Partial {
			for _, p := range ev.Content.Parts {
				answer.WriteString(p.Text)
			}
		}
		return nil
	})
	require.NoError(t, err)
	require.Contains(t, answer.String(), "the word was pineapple", "answer %q", answer.String())
	f.llm.mu.Lock()
	asideFirst, asideSecond := requestText(f.llm.Requests[1]), requestText(f.llm.Requests[2])
	f.llm.mu.Unlock()
	require.Contains(t, asideFirst, "remember pineapple", "aside request lacks history or question:\n%s", asideFirst)
	require.Contains(t, asideFirst, "btw, what was the word?", "aside request lacks history or question:\n%s", asideFirst)
	require.Contains(t, asideSecond, "btw is read-only", "create_file wasn't refused as read-only:\n%s", asideSecond)
	_, err = os.Stat(filepath.Join(f.cfg.Tools.WorkspaceDir, "x.txt"))
	require.Error(t, err, "the side question wrote a file")
	after, _ := os.ReadFile(logPath)
	require.Equal(t, string(before), string(after), "the saved event log changed:\n%s", after)
	got := f.eng.Usage("s").Calls
	require.Equal(t, usage+2, got, "usage calls %d, want %d", got, usage+2)

	_, err = collect(t, f.eng, "s", "carry on")
	require.NoError(t, err)
	f.llm.mu.Lock()
	next := requestText(f.llm.Requests[3])
	f.llm.mu.Unlock()
	require.Contains(t, next, "remember pineapple", "the next turn saw the side question:\n%s", next)
	require.NotContains(t, next, "btw", "the next turn saw the side question:\n%s", next)
	require.NotContains(t, next, "the word was", "the next turn saw the side question:\n%s", next)
}

// With no turns yet, a side question still works (on an empty history).
func TestAsideOnANewSession(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{}, textContent("hello"))
	require.NoError(t, f.eng.Aside(context.Background(), "fresh", "hi?", func(*session.Event) error { return nil }))
	require.Equal(t, 1, f.llm.Calls(), "%d calls", f.llm.Calls())
}

// A side question streams like a turn when the engine streams, and works
// on a session with no turns yet.
func TestAsideStreamsOnANewSession(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{opts: []Option{WithStreaming(true)}}, textContent("an aside"))
	var got strings.Builder
	err := f.eng.Aside(context.Background(), "new", "btw?", func(ev *session.Event) error {
		if ev.Content != nil && !ev.Partial {
			for _, p := range ev.Content.Parts {
				got.WriteString(p.Text)
			}
		}
		return nil
	})
	require.NoError(t, err)
	require.Contains(t, got.String(), "an aside")
}

// copySession fails when the copy's ID is taken.
func TestCopySessionTakenID(t *testing.T) {
	ctx := context.Background()
	dst := session.InMemoryService()
	_, err := dst.Create(ctx, &session.CreateRequest{AppName: appName, UserID: "user", SessionID: "taken"})
	require.NoError(t, err)
	require.Error(t, copySession(ctx, session.InMemoryService(), dst, "s", "taken"))
}
