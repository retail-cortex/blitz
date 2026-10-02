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

package agents

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"gopkg.in/yaml.v3"
)

// AgentMetadata holds frontmatter metadata defined in the markdown header.
type AgentMetadata struct {
	Name         string   `yaml:"name"`
	DisplayName  string   `yaml:"display_name,omitempty"`
	Description  string   `yaml:"description"`
	Tools        []string `yaml:"tools"`
	DefaultModel string   `yaml:"default_model,omitempty"`
	AgencyLevel  string   `yaml:"agency_level,omitempty"`
	// PermissionMode is the mode it runs in through invoke_agent: default,
	// plan, accept-edits, dont-ask or bypass ("": the workspace's). A
	// project's agent may only tighten until the project is trusted, and
	// bypass needs the OS sandbox.
	PermissionMode string `yaml:"permission_mode,omitempty"`
	// MaxTurns caps its model calls through invoke_agent (0: the caller's
	// budget, or a background task's).
	MaxTurns int `yaml:"max_turns,omitempty"`
	// Background makes invoke_agent run it in the background unless the
	// call says otherwise.
	Background bool `yaml:"background,omitempty"`
	// Isolation "worktree" runs it, in the background, in its own git
	// worktree and branch.
	Isolation string `yaml:"isolation,omitempty"`

	// The model settings it runs with, over the model's own
	// ([model_settings]) and the session's effort; unset leaves those.
	Temperature    *float64 `yaml:"temperature,omitempty"`
	TopP           *float64 `yaml:"top_p,omitempty"`
	MaxTokens      *int     `yaml:"max_tokens,omitempty"`
	Effort         string   `yaml:"effort,omitempty"`
	ThinkingBudget *int     `yaml:"thinking_budget,omitempty"`
}

// ModelSettings returns the model settings the agent sets, and whether it
// sets any.
func (m AgentMetadata) ModelSettings() (config.ModelSettings, bool) {
	s := config.ModelSettings{Temperature: m.Temperature, TopP: m.TopP, MaxTokens: m.MaxTokens, ThinkingBudget: m.ThinkingBudget}
	if m.Effort != "" {
		e := m.Effort
		s.ReasoningEffort = &e
	}
	return s, s != config.ModelSettings{}
}

// Check returns what keeps the metadata from loading, or nil.
func (m AgentMetadata) Check() error {
	if m.Name == "" {
		return fmt.Errorf("agent spec must declare a 'name' in frontmatter")
	}
	if m.PermissionMode != "" {
		if _, err := api.ParsePermissionMode(m.PermissionMode); err != nil {
			return fmt.Errorf("permission_mode: %w", err)
		}
	}
	if m.MaxTurns < 0 {
		return fmt.Errorf("max_turns must not be negative")
	}
	if m.Isolation != "" && m.Isolation != "worktree" {
		return fmt.Errorf("isolation: %q (only worktree)", m.Isolation)
	}
	if m.Temperature != nil && (*m.Temperature < 0 || *m.Temperature > 2) {
		return fmt.Errorf("temperature: %v (0 to 2)", *m.Temperature)
	}
	if m.TopP != nil && (*m.TopP <= 0 || *m.TopP > 1) {
		return fmt.Errorf("top_p: %v (above 0, at most 1)", *m.TopP)
	}
	if m.MaxTokens != nil && *m.MaxTokens < 1 {
		return fmt.Errorf("max_tokens: %d (at least 1)", *m.MaxTokens)
	}
	if m.ThinkingBudget != nil && *m.ThinkingBudget < 0 {
		return fmt.Errorf("thinking_budget: %d (0 or more)", *m.ThinkingBudget)
	}
	if m.Effort != "" {
		if _, err := config.ParseEffort(m.Effort); err != nil {
			return fmt.Errorf("effort: %w", err)
		}
	}
	return nil
}

// AgencyLevels are the agency levels an agent may declare.
var AgencyLevels = []string{"low", "medium", "high", "extreme"}

// AgentSpec represents a parsed agent specification with frontmatter and markdown body.
type AgentSpec struct {
	AgentMetadata
	SystemPrompt string
	// Path is the file it came from ("" for a built-in agent).
	Path string
}

// ParseMarkdownSpec parses a markdown file containing YAML frontmatter.
func ParseMarkdownSpec(content []byte) (*AgentSpec, error) {
	trimmed := bytes.TrimSpace(content)
	if !bytes.HasPrefix(trimmed, []byte("---")) {
		return nil, fmt.Errorf("markdown spec missing starting frontmatter delimiter '---'")
	}

	// Find closing delimiter
	rest := trimmed[3:]
	idx := bytes.Index(rest, []byte("---"))
	if idx == -1 {
		return nil, fmt.Errorf("markdown spec missing closing frontmatter delimiter '---'")
	}

	frontmatterBytes := rest[:idx]
	promptBytes := bytes.TrimSpace(rest[idx+3:])

	var meta AgentMetadata
	if err := yaml.Unmarshal(frontmatterBytes, &meta); err != nil {
		return nil, fmt.Errorf("failed to parse YAML frontmatter: %w", err)
	}

	if err := meta.Check(); err != nil {
		return nil, err
	}
	if meta.Effort != "" {
		meta.Effort, _ = config.ParseEffort(meta.Effort)
	}

	return &AgentSpec{
		AgentMetadata: meta,
		SystemPrompt:  string(promptBytes),
	}, nil
}

// Marshal writes spec as an agent file: its frontmatter, then its prompt.
func (spec *AgentSpec) Marshal() ([]byte, error) {
	meta := spec.AgentMetadata
	if meta.Tools == nil {
		meta.Tools = []string{}
	}
	front, err := yaml.Marshal(meta)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString("---\n")
	b.Write(front)
	b.WriteString("---\n")
	if p := strings.TrimSpace(spec.SystemPrompt); p != "" {
		b.WriteString(p)
		b.WriteString("\n")
	}
	return b.Bytes(), nil
}

// InterpolatePrompt fills in the agency rules for agencyLevel ("" is high).
func (spec *AgentSpec) InterpolatePrompt(agencyLevel string) string {
	if agencyLevel == "" {
		agencyLevel = "high"
	}
	agencyInstructions := getAgencyInstructions(agencyLevel)
	prompt := strings.ReplaceAll(spec.SystemPrompt, "{agency_instructions}", agencyInstructions)
	return strings.ReplaceAll(prompt, "{{agency_instructions}}", agencyInstructions)
}

func getAgencyInstructions(level string) string {
	switch strings.ToLower(level) {
	case "low":
		return "- You are at LOW agency: work one step at a time. After each meaningful unit of work, stop, summarize what you did, and ask before continuing.\n- Never take consequential or irreversible actions without explicit approval."
	case "medium":
		return "- You are at MEDIUM agency: complete routine, clearly-requested work without asking, but pause and check in at major milestones or before consequential changes."
	case "extreme":
		return "- You are at EXTREME agency: complete the requested task autonomously with maximal persistence.\n- If a background process gates completion, do not stop and force the user to reprompt you. Check progress when work remains. Be as agentic as possible."
	case "high":
		fallthrough
	default:
		return "- Complete the requested task autonomously. Do not ask for routine permission to continue work the user already requested. Ask only when blocked by missing requirements or irreversible actions requiring approval.\n- Continue autonomously unless user input is definitively required."
	}
}
