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

	"google.golang.org/genai"
)

// Plan mode plans every prompt: tools that change anything are refused
// and the transcript records the text as typed (not "/plan …").
func TestPlanModeAppliesToEveryPrompt(t *testing.T) {
	create := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "create_file", Args: map[string]any{"path": "x.txt", "content": "x"}}}}}
	w, llm := openTestWith(t, nil, create, text("here is the plan"), text("done"))
	if _, err := w.SetPermissionMode("plan"); err != nil {
		t.Fatal(err)
	}
	if got := w.Settings().PermissionMode; got != "plan" {
		t.Fatalf("mode %q", got)
	}
	s, _ := w.NewSession()
	var result map[string]any
	res, err := w.Run(context.Background(), s.ID, api.Turn{Text: "add x.txt"}, func(e api.Event) {
		if e.ToolResult != nil {
			result = e.ToolResult.Result
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg, _ := result["error"].(string); !strings.Contains(msg, "plan mode") {
		t.Errorf("create_file wasn't refused: %v", result)
	}
	if _, err := os.Stat(filepath.Join(w.Dir(), "x.txt")); err == nil {
		t.Error("the file was created in plan mode")
	}
	if first := llm.Requests[0].Contents; !strings.Contains(first[len(first)-1].Parts[0].Text, "plan-only mode") {
		t.Error("the prompt wasn't wrapped as a plan")
	}
	if got := w.storage.Active().Messages[0].Content; got != "add x.txt" {
		t.Errorf("transcript recorded %q", got)
	}
	if res.Output != "here is the plan" {
		t.Errorf("output %q", res.Output)
	}

	// Back to default: the next prompt runs normally.
	w.SetPermissionMode("default")
	w.Run(context.Background(), s.ID, api.Turn{Text: "go"}, func(api.Event) {})
	if last := llm.Requests[2].Contents; strings.Contains(last[len(last)-1].Parts[0].Text, "plan-only mode") {
		t.Error("default mode still planned")
	}
}

func TestSetPermissionModeErrors(t *testing.T) {
	w, _ := openTestWith(t, nil)
	if _, err := w.SetPermissionMode("yolo"); !errors.Is(err, api.ErrUnknownMode) {
		t.Errorf("unknown mode: %v", err)
	}
	if m, err := w.SetPermissionMode("acceptEdits"); err != nil || m != "accept-edits" {
		t.Errorf("accept-edits: %q %v", m, err)
	}
}
