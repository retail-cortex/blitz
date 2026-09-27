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
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/agents"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func TestEngineExecution(t *testing.T) {
	ctx := context.Background()
	cfg := config.DefaultConfig()

	agentReg, err := agents.NewRegistry()
	if err != nil {
		t.Fatalf("failed to create agent registry: %v", err)
	}

	skillProv, err := skills.NewProvider()
	if err != nil {
		t.Fatalf("failed to create skill provider: %v", err)
	}

	toolReg, err := tools.NewRegistry(cfg, agentReg, skillProv)
	if err != nil {
		t.Fatalf("failed to create tool registry: %v", err)
	}

	mockModel := NewMockLLM("mock-model",
		genai.NewContentFromText("Wrote the code.", genai.RoleModel),
	)

	eng, err := NewEngine(ctx, cfg, agentReg, skillProv, toolReg, mockModel)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	if eng.ActiveAgent() != "blitz" {
		t.Errorf("expected active agent 'blitz', got '%s'", eng.ActiveAgent())
	}

	var observedTexts []string
	err = eng.Execute(ctx, "session-1", "Write a hello world program", func(ev *session.Event) error {
		if ev.Content != nil {
			for _, part := range ev.Content.Parts {
				if part.Text != "" {
					observedTexts = append(observedTexts, part.Text)
				}
			}
		}
		return nil
	})

	if err != nil {
		t.Fatalf("engine execution failed: %v", err)
	}

	joined := strings.Join(observedTexts, " ")
	if !strings.Contains(joined, "Wrote the code.") {
		t.Errorf("expected output to contain 'Wrote the code.', got: %s", joined)
	}

	// Test switching agent to helios
	err = eng.SetActiveAgent(ctx, "helios")
	if err != nil {
		t.Fatalf("failed to switch agent to helios: %v", err)
	}
	if eng.ActiveAgent() != "helios" {
		t.Errorf("expected active agent 'helios', got '%s'", eng.ActiveAgent())
	}
}
