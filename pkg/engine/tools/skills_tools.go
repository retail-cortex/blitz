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

package tools

import (
	"fmt"
	"strings"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"github.com/retail-cortex/blitz/pkg/textutil"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// SkillSummary represents a skill in search results.
type SkillSummary struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Version     string   `json:"version"`
}

// ListSkillsInput defines arguments for listing or searching skills.
type ListSkillsInput struct {
	Query string `json:"query,omitempty" jsonschema:"Optional keyword query to filter skills by name, description, or tags"`
}

// ListSkillsOutput holds discovered skills.
type ListSkillsOutput struct {
	Skills []SkillSummary `json:"skills"`
	Count  int            `json:"count"`
	Error  string         `json:"error,omitempty"`
}

// NewListSkillsTool creates an ADK tool for searching skills.
func NewListSkillsTool(provider *skills.Provider) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        "list_or_search_skills",
			Description: "List or search available Agent Skills by keyword",
		},
		func(ctx agent.Context, input ListSkillsInput) (ListSkillsOutput, error) {
			if provider == nil {
				return ListSkillsOutput{Error: "skills provider not initialized"}, nil
			}

			var matched []*skills.Skill
			if input.Query == "" {
				matched = provider.List()
			} else {
				matched = provider.Search(input.Query)
			}

			summaries := make([]SkillSummary, 0, len(matched))
			for _, s := range matched {
				if !offered(s, callerAgent(ctx)) {
					continue // offered to other agents (agents:)
				}
				summaries = append(summaries, SkillSummary{
					Name:        s.Name,
					Description: s.Description,
					Tags:        s.Tags,
					Version:     s.Version,
				})
			}

			return ListSkillsOutput{
				Skills: summaries,
				Count:  len(summaries),
			}, nil
		},
	)
}

// ActivateSkillInput defines arguments for activating a skill.
type ActivateSkillInput struct {
	SkillName string `json:"skill_name" jsonschema:"Name of the skill to activate"`
}

// ActivateSkillOutput holds skill instructions.
type ActivateSkillOutput struct {
	SkillName    string   `json:"skill_name"`
	Instructions string   `json:"instructions"`
	Resources    []string `json:"resources"`
	// References are the files under references/, read with
	// read_skill_file.
	References []string `json:"references,omitempty"`
	// Members are the skills a meta skill combines, each activated with
	// it, and theirs in turn, once each (SK-99).
	Members []SkillMember `json:"members,omitempty"`
	// Scripts can be run with run_skill_script, when allowed.
	Scripts []SkillScriptInfo `json:"scripts,omitempty"`
	// Assets are the skill's declared resources (Castor's): what each is,
	// where it lives, and text it was turned into.
	Assets []SkillAsset `json:"assets,omitempty"`
	// WritesWorkspace means the scripts change the workspace's files, in a
	// copy; the user approves what they changed before it's kept.
	WritesWorkspace bool   `json:"writes_workspace,omitempty"`
	Error           string `json:"error,omitempty"`
}

// SkillAsset is one of a skill's declared resources, for the model.
type SkillAsset struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Category    string `json:"category,omitempty"`
	MimeType    string `json:"mime_type,omitempty"`
	// URL is where to fetch it (web_fetch), when it has one.
	URL       string `json:"url,omitempty"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
	// Content is its inline text; Summary and Text what it was interpreted
	// as (Text only when the skill asks it to go with the instructions).
	Content string `json:"content,omitempty"`
	Summary string `json:"summary,omitempty"`
	Text    string `json:"text,omitempty"`
}

// assetTextLimit bounds a resource's text in activate_skill's answer.
const assetTextLimit = 16 << 10

// skillAssets lists a skill's declared resources.
func skillAssets(skill *skills.Skill) []SkillAsset {
	var out []SkillAsset
	for _, r := range skill.ResourceRequirements {
		a := SkillAsset{ID: r.ID, Name: r.Name, Description: r.Description, Category: r.CategoryName(), MimeType: r.MimeType,
			Content: textutil.Ellipsize(r.InlineContent, assetTextLimit)}
		if st := r.Storage; st != nil {
			a.SizeBytes = st.SizeBytes
			a.MimeType = cmpOr(a.MimeType, st.MimeType)
			a.URL = st.PublicURL
			if a.URL == "" && st.GCSURI != "" {
				a.URL, _ = skills.StorageURL(st.GCSURI)
			}
		}
		if in := r.Interpretation; in != nil {
			a.Summary = textutil.Ellipsize(in.Summary, assetTextLimit)
			if r.AutoInjectContext {
				a.Text = textutil.Ellipsize(in.InterpretedText, assetTextLimit)
			}
		}
		out = append(out, a)
	}
	return out
}

// SkillScriptInfo describes one of a skill's scripts for the model.
type SkillScriptInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Allowed     bool   `json:"allowed"`
	// Why the host's skills policy doesn't allow it.
	Blocked string `json:"blocked,omitempty"`
}

// NewActivateSkillTool creates an ADK tool for activating a skill.
// policy (nil: the defaults) decides which of its scripts may run.
func NewActivateSkillTool(provider *skills.Provider, policy *config.SkillPolicy) (tool.Tool, error) {
	if policy == nil {
		p := config.DefaultConfig().Skills.Policy
		policy = &p
	}
	return functiontool.New(
		functiontool.Config{
			Name:        "activate_skill",
			Description: "Load and activate full SKILL.md instructions and resources for a skill",
		},
		func(ctx agent.Context, input ActivateSkillInput) (ActivateSkillOutput, error) {
			if provider == nil {
				return ActivateSkillOutput{Error: "skills provider not initialized"}, nil
			}

			return activate(provider, *policy, input.SkillName, callerAgent(ctx), map[string]bool{}), nil
		},
	)
}

// callerAgent is the name of the agent calling a tool, "" when the
// context can't say (no agent behind it).
func callerAgent(ctx agent.Context) (name string) {
	defer func() {
		if recover() != nil {
			name = ""
		}
	}()
	return ctx.AgentName()
}

// offered reports whether s is offered to the agent named (agents:); an
// agent that can't be told is offered every skill.
func offered(s *skills.Skill, agent string) bool { return agent == "" || s.OfferedTo(agent) }

// SkillMember is one of the skills a meta skill combines, activated with
// it.
type SkillMember struct {
	SkillName    string            `json:"skill_name"`
	Instructions string            `json:"instructions,omitempty"`
	References   []string          `json:"references,omitempty"`
	Scripts      []SkillScriptInfo `json:"scripts,omitempty"`
	Error        string            `json:"error,omitempty"`
}

// maxMembers bounds how many skills one activation brings in.
const maxMembers = 16

// activate is a skill's instructions, files and scripts for agent, with
// a meta skill's members' (SK-99), theirs in turn, each once: seen holds
// those already activated, so a loop ends.
func activate(provider *skills.Provider, policy config.SkillPolicy, name, agent string, seen map[string]bool) ActivateSkillOutput {
	skill, ok := provider.Get(name)
	if !ok || skill == nil {
		return ActivateSkillOutput{SkillName: name, Error: fmt.Sprintf("skill '%s' not found; use list_or_search_skills to see available skills", name)}
	}
	if !offered(skill, agent) {
		return ActivateSkillOutput{SkillName: name, Error: fmt.Sprintf("skill '%s' is offered to %s, not to this agent", name, strings.Join(skill.Agents, ", "))}
	}
	seen[skill.Name] = true
	out := ActivateSkillOutput{
		SkillName:    skill.Name,
		Instructions: skill.Content,
		Resources:    skill.Resources,
		References:   skill.References,
	}
	out.WritesWorkspace = skill.ExecutionHints != nil && skill.ExecutionHints.WritesWorkspace
	out.Assets = skillAssets(skill)
	if len(skill.Scripts) > 0 {
		ev := skills.Evaluate(skill, policy)
		for i, sc := range skill.Scripts {
			info := SkillScriptInfo{Name: sc.Name, Description: sc.Description, Allowed: ev.Scripts[i].Allowed}
			if !info.Allowed {
				info.Blocked = strings.Join(ev.Scripts[i].Reasons, "; ")
			}
			out.Scripts = append(out.Scripts, info)
		}
	}
	for _, m := range skill.Members {
		switch {
		case seen[m]:
			continue // activated already, here or above: a loop, or named twice
		case len(seen) > maxMembers:
			out.Members = append(out.Members, SkillMember{SkillName: m, Error: "too many skills combined; activate it on its own"})
		default:
			sub := activate(provider, policy, m, agent, seen)
			out.Members = append(out.Members, SkillMember{SkillName: sub.SkillName, Instructions: sub.Instructions, References: sub.References, Scripts: sub.Scripts, Error: sub.Error})
			out.Members = append(out.Members, sub.Members...)
		}
	}
	return out
}

// ReadSkillFileInput names one of a skill's files.
type ReadSkillFileInput struct {
	SkillName string `json:"skill_name" jsonschema:"Name of the skill"`
	Path      string `json:"path" jsonschema:"The file, relative to the skill's folder, as activate_skill lists it (references/guide.md)"`
}

// ReadSkillFileOutput is the file's text.
type ReadSkillFileOutput struct {
	Content   string `json:"content,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Error     string `json:"error,omitempty"`
}

// skillFileLimit bounds what read_skill_file returns.
const skillFileLimit = 64 << 10

// NewReadSkillFileTool creates read_skill_file: one of a skill's files
// (its references, SK-98), confined to the skill's folder.
func NewReadSkillFileTool(provider *skills.Provider) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        "read_skill_file",
			Description: "Read one of a skill's files, such as a reference activate_skill lists (references/...)",
		},
		func(ctx agent.Context, in ReadSkillFileInput) (ReadSkillFileOutput, error) {
			if provider == nil {
				return ReadSkillFileOutput{Error: "skills provider not initialized"}, nil
			}
			skill, ok := provider.Get(in.SkillName)
			if !ok || !offered(skill, callerAgent(ctx)) {
				return ReadSkillFileOutput{Error: fmt.Sprintf("skill '%s' not found", in.SkillName)}, nil
			}
			data, err := skill.ReadScript(in.Path)
			if err != nil {
				return ReadSkillFileOutput{Error: err.Error()}, nil
			}
			out := ReadSkillFileOutput{Content: string(data)}
			if len(data) > skillFileLimit {
				out.Content, out.Truncated = textutil.Ellipsize(string(data), skillFileLimit), true
			}
			return out, nil
		},
	)
}
