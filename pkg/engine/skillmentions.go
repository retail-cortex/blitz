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
	"fmt"
	"regexp"
	"strings"

	"github.com/retail-cortex/blitz/pkg/engine/skills"
)

// skillMentionRE is \skill-name where a word starts (spec_skills_013
// SK-104): a backslash, then a skill's name.
var skillMentionRE = regexp.MustCompile(`(?:^|[\s(\[{"'])\\([a-z0-9]+(?:-[a-z0-9]+)*)`)

// skillMentions are the skills a prompt names as \skill-name, in order,
// once each: those the workspace has and offers to the active agent.
func (w *Workspace) skillMentions(text string) []*skills.Skill {
	var out []*skills.Skill
	seen := map[string]bool{}
	agent := w.engine.ActiveAgent()
	for _, m := range skillMentionRE.FindAllStringSubmatch(text, -1) {
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		if s, ok := w.skills.Get(name); ok && s.OfferedTo(agent) {
			out = append(out, s)
		}
	}
	return out
}

// skillMentionNote is what goes to the model after a prompt that names
// skills as \skill-name: each one's instructions, as /skill-name sends
// them, and a meta skill's members to activate. The transcript keeps the
// prompt as typed. "" when it names none.
func (w *Workspace) skillMentionNote(text string) string {
	return skillNote(w.skillMentions(text))
}

// skillNote gives the agent each skill in list (its instructions, its
// references and a meta skill's members), as \skill-name sends them.
func skillNote(list []*skills.Skill) string {
	if len(list) == 0 {
		return ""
	}
	var b strings.Builder
	for _, s := range list {
		fmt.Fprintf(&b, "\n\n---\nThe user asked for the skill %q (\\%s). Use it for this request. Its instructions:\n\n%s", s.Name, s.Name, s.Content)
		if len(s.References) > 0 {
			fmt.Fprintf(&b, "\n\nIts references, read with read_skill_file: %s", strings.Join(s.References, ", "))
		}
		if len(s.Members) > 0 {
			fmt.Fprintf(&b, "\n\nIt combines these skills: activate each with activate_skill: %s", strings.Join(s.Members, ", "))
		}
	}
	return b.String()
}
