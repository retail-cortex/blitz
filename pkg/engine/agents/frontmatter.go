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

	"gopkg.in/yaml.v3"
)

// AgentMetadata holds frontmatter metadata defined in the markdown header.
type AgentMetadata struct {
	Name         string   `yaml:"name"`
	DisplayName  string   `yaml:"display_name"`
	Description  string   `yaml:"description"`
	Tools        []string `yaml:"tools"`
	DefaultModel string   `yaml:"default_model,omitempty"`
	AgencyLevel  string   `yaml:"agency_level,omitempty"`
}

// AgentSpec represents a parsed agent specification with frontmatter and markdown body.
type AgentSpec struct {
	AgentMetadata
	SystemPrompt string
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

	if meta.Name == "" {
		return nil, fmt.Errorf("agent spec must declare a 'name' in frontmatter")
	}

	return &AgentSpec{
		AgentMetadata: meta,
		SystemPrompt:  string(promptBytes),
	}, nil
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
