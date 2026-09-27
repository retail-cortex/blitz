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
	"slices"
	"strings"
	"testing"
)

func TestPlanModeRefusesChangesButAllowsReading(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{},
		toolCall("create_file", map[string]any{"path": "new.txt", "content": "x"}),
		toolCall("read_file", map[string]any{"path": "notes.txt"}),
		textContent("1. Do the thing"))
	os.WriteFile(filepath.Join(f.cfg.Tools.WorkspaceDir, "notes.txt"), []byte("existing notes"), 0o600)

	got, err := functionResponses(t, f.eng, "s", PlanPrompt("add a file"), WithPlanOnly())
	if err != nil {
		t.Fatal(err)
	}
	if e, _ := got["create_file"]["error"].(string); !strings.Contains(e, "plan mode") {
		t.Fatalf("create_file not refused: %v", got["create_file"])
	}
	if _, err := os.Stat(filepath.Join(f.cfg.Tools.WorkspaceDir, "new.txt")); err == nil {
		t.Fatal("file created in plan mode")
	}
	if c, _ := got["read_file"]["content"].(string); !strings.Contains(c, "existing notes") {
		t.Fatalf("read_file blocked in plan mode: %v", got["read_file"])
	}
}

func TestPlanModeCoversSubagents(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{},
		toolCall("invoke_agent", map[string]any{"agent_name": "qa", "prompt": "write a test"}),
		toolCall("create_file", map[string]any{"path": "sub.txt", "content": "x"}), // the sub-agent tries to write
		textContent("sub-agent done"),
		textContent("plan ready"))
	if _, err := functionResponses(t, f.eng, "s", PlanPrompt("tests"), WithPlanOnly()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.cfg.Tools.WorkspaceDir, "sub.txt")); err == nil {
		t.Fatal("sub-agent wrote a file in plan mode")
	}
	// The sub-agent really ran and was refused (not skipped): its second
	// model call carries the refusal of its create_file.
	refused := false
	for _, req := range f.llm.Requests {
		for _, c := range req.Contents {
			for _, p := range c.Parts {
				if r := p.FunctionResponse; r != nil && r.Name == "create_file" {
					e, _ := r.Response["error"].(string)
					refused = refused || strings.Contains(e, "plan mode")
				}
			}
		}
	}
	if !refused {
		t.Fatal("sub-agent's create_file was never attempted and refused")
	}
}

func TestWithoutPlanModeToolsRunNormally(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{},
		toolCall("create_file", map[string]any{"path": "new.txt", "content": "x"}),
		textContent("done"))
	if _, err := functionResponses(t, f.eng, "s", "add a file"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.cfg.Tools.WorkspaceDir, "new.txt")); err != nil {
		t.Fatal("create_file did not run outside plan mode")
	}
}

// Every primary agent has the workflow tools, whatever its tool list;
// sub-agents don't.
func TestPrimaryAgentsHaveWorkflowTools(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{})
	if err := f.eng.SetActiveAgent(context.Background(), "qa"); err != nil {
		t.Fatal(err)
	}
	if _, err := collect(t, f.eng, "s", "hi"); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range f.llm.Requests[0].Config.Tools {
		for _, d := range tl.FunctionDeclarations {
			names = append(names, d.Name)
		}
	}
	for _, want := range []string{"todo", "exit_plan_mode", "enter_plan_mode", "read_file"} {
		if !slices.Contains(names, want) {
			t.Errorf("qa lacks %s: %v", want, names)
		}
	}
	if slices.Contains(names, "apply_patch") {
		t.Error("qa got a tool its list doesn't have")
	}
}

// A turn whose agent entered plan mode refuses tools that change things.
func TestPlanGateRefusesWrites(t *testing.T) {
	if r := planRefusal(&runState{}, true, "create_file"); r == nil {
		t.Error("create_file allowed while planning")
	}
	if r := planRefusal(&runState{}, true, "todo"); r != nil {
		t.Error("todo refused while planning")
	}
	if r := planRefusal(&runState{}, false, "create_file"); r != nil {
		t.Error("create_file refused outside plan mode")
	}
}
