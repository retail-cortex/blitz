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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
)

// Studio's skills (spec_skills_013 SK-100–SK-103): the workspace's own,
// in .agents/skills (or another skills folder inside the workspace),
// read, written, renamed, copied and deleted, with their references and
// scripts; any skill the workspace sees may be copied in.

// ProjectSkillsDir is where Studio writes a new skill of the workspace's:
// .agents/skills.
func (w *Workspace) ProjectSkillsDir() string { return filepath.Join(w.Dir(), ".agents", "skills") }

// ownsSkill reports whether s is the workspace's own: its folder is in
// the workspace.
func (w *Workspace) ownsSkill(s *skills.Skill) bool {
	dir := s.HostDir()
	if dir == "" {
		return false
	}
	ws := w.Dir()
	// Both through their symbolic links (/var is /private/var on macOS).
	if real, err := filepath.EvalSymlinks(ws); err == nil {
		ws = real
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	rel, err := filepath.Rel(ws, dir)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

// ownSkill is the workspace's own skill name, with the folder holding it.
func (w *Workspace) ownSkill(name string) (*skills.Skill, string, error) {
	s, ok := w.skills.Get(name)
	if !ok || !w.ownsSkill(s) {
		return nil, "", fmt.Errorf("%w: %q isn't one of the workspace's skills", skills.ErrUnknownSkill, name)
	}
	return s, filepath.Dir(s.HostDir()), nil
}

// reloadSkills reads the skills again after a change, so the next turn,
// listing or activation sees it.
func (w *Workspace) reloadSkills() {
	if err := w.skills.Reload(); err != nil {
		w.warn(err.Error())
	}
}

// GetSkillFile returns a skill as Studio edits it: any skill the
// workspace sees, editable when it's the workspace's own.
func (w *Workspace) GetSkillFile(name string) (api.SkillFile, error) {
	s, ok := w.skills.Get(name)
	if !ok {
		return api.SkillFile{}, fmt.Errorf("%w: %q", skills.ErrUnknownSkill, name)
	}
	f := api.SkillFile{
		Info: w.skillInfo(s), Path: s.Path, Editable: w.ownsSkill(s),
		Spec: api.SkillSpec{Name: s.Name, Description: s.Description, Tools: s.AllowedTools, Agents: s.Agents, Members: s.Members, Instructions: s.Content, Triggers: s.TriggerPhrases},
	}
	if f.Editable {
		data, err := os.ReadFile(s.Path)
		if err != nil {
			return api.SkillFile{}, err
		}
		f.Hash = skills.FileHash(data)
	}
	return f, nil
}

// SaveSkill writes a skill of the workspace's from spec: a new one in
// .agents/skills (previous ""), or the one named previous, renamed when
// the name differs, if its SKILL.md still has hash. Problems are returned,
// with nothing written, when it can't be saved.
func (w *Workspace) SaveSkill(spec api.SkillSpec, previous, hash string) (api.SkillFile, []string, error) {
	dir := w.ProjectSkillsDir()
	if previous != "" {
		var err error
		if _, dir, err = w.ownSkill(previous); err != nil {
			return api.SkillFile{}, nil, err
		}
	} else if err := os.MkdirAll(dir, 0o755); err != nil {
		return api.SkillFile{}, nil, err
	}
	_, problems, err := skills.Save(dir, skills.Spec{
		Name: spec.Name, Description: spec.Description, Tools: spec.Tools, Agents: spec.Agents, Members: spec.Members, Instructions: spec.Instructions, Triggers: spec.Triggers,
	}, previous, hash, w.skills.IsBuiltin)
	if err != nil || len(problems) > 0 {
		return api.SkillFile{}, problems, err
	}
	w.reloadSkills()
	f, err := w.GetSkillFile(strings.TrimSpace(spec.Name))
	return f, nil, err
}

// DeleteSkill deletes a skill of the workspace's: its folder.
func (w *Workspace) DeleteSkill(name string) error {
	_, dir, err := w.ownSkill(name)
	if err != nil {
		return err
	}
	if err := skills.Delete(dir, name); err != nil {
		return err
	}
	w.reloadSkills()
	return nil
}

// ReadSkillFile returns one of a skill's files (references/…, scripts/…,
// or one beside SKILL.md), for any skill the workspace sees.
func (w *Workspace) ReadSkillFile(name, rel string) ([]byte, error) {
	s, ok := w.skills.Get(name)
	if !ok {
		return nil, fmt.Errorf("%w: %q", skills.ErrUnknownSkill, name)
	}
	return s.ReadScript(rel)
}

// WriteSkillFile writes one of a workspace skill's files, under
// references/ or scripts/, and returns the skill as it is then.
func (w *Workspace) WriteSkillFile(name, rel string, data []byte) (api.SkillFile, error) {
	_, dir, err := w.ownSkill(name)
	if err != nil {
		return api.SkillFile{}, err
	}
	if err := skills.WriteFile(dir, name, rel, data); err != nil {
		return api.SkillFile{}, err
	}
	w.reloadSkills()
	return w.GetSkillFile(name)
}

// DeleteSkillFile removes one of a workspace skill's files.
func (w *Workspace) DeleteSkillFile(name, rel string) (api.SkillFile, error) {
	_, dir, err := w.ownSkill(name)
	if err != nil {
		return api.SkillFile{}, err
	}
	if err := skills.DeleteFile(dir, name, rel); err != nil {
		return api.SkillFile{}, err
	}
	w.reloadSkills()
	return w.GetSkillFile(name)
}

// CopySkill copies any skill the workspace sees (a built-in, the user's,
// a registry's, another of the workspace's) into .agents/skills as name,
// its files and all, to edit as the workspace's own.
func (w *Workspace) CopySkill(from, name string) (api.SkillFile, []string, error) {
	src, ok := w.skills.Get(from)
	if !ok {
		return api.SkillFile{}, nil, fmt.Errorf("%w: %q", skills.ErrUnknownSkill, from)
	}
	name = strings.TrimSpace(name)
	spec := skills.Spec{Name: name, Description: src.Description, Tools: src.AllowedTools, Agents: src.Agents, Members: src.Members, Instructions: src.Content, Triggers: src.TriggerPhrases}
	if probs := skills.Check(spec, w.skills.IsBuiltin); len(probs) > 0 {
		return api.SkillFile{}, probs, nil
	}
	if _, taken := w.skills.Get(name); taken {
		return api.SkillFile{}, []string{fmt.Sprintf("name %q: the workspace has a skill by that name", name)}, nil
	}
	dir := w.ProjectSkillsDir()
	target := filepath.Join(dir, name)
	if _, err := os.Stat(target); err == nil {
		return api.SkillFile{}, []string{fmt.Sprintf("name %q: %s exists", name, target)}, nil
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return api.SkillFile{}, nil, err
	}
	if err := src.CopyTo(target); err != nil {
		os.RemoveAll(target)
		return api.SkillFile{}, nil, err
	}
	// The copy's own name, the rest of its front matter as it was.
	old, err := os.ReadFile(filepath.Join(target, "SKILL.md"))
	if err == nil {
		var data []byte
		if data, err = skills.Render(old, spec); err == nil {
			err = os.WriteFile(filepath.Join(target, "SKILL.md"), data, 0o644)
		}
	}
	if err != nil {
		os.RemoveAll(target)
		return api.SkillFile{}, nil, err
	}
	w.reloadSkills()
	f, err := w.GetSkillFile(name)
	return f, nil, err
}

// RunSkillScript runs one of a skill's scripts now, with args, as
// run_skill_script would (the policy, its sandbox and the permission
// rules' denials all apply): Studio's Test run, which the user starts,
// so its approvals are given.
func (w *Workspace) RunSkillScript(ctx context.Context, name, script string, args []string) api.ScriptRun {
	runner := w.tools.SkillScripts()
	if runner == nil {
		return api.ScriptRun{Error: "skills' scripts can't run here (skills are off)"}
	}
	approve := func(context.Context, api.ApprovalRequest) (api.Decision, error) { return api.DecisionOnce, nil }
	out := runner.Run(tools.Unattended(ctx, approve), tools.RunSkillScriptInput{Skill: name, Script: script, Args: args})
	return api.ScriptRun{ExitCode: out.ExitCode, Stdout: out.Stdout, Stderr: out.Stderr, TimedOut: out.TimedOut, Sandbox: out.Sandbox, Files: out.Files, Error: out.Error}
}
