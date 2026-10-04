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
	"path/filepath"
	"sort"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/memory"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"github.com/retail-cortex/blitz/pkg/i18n"
)

// ReviewProject is what the project files of cfg's workspace
// (cfg.Tools.WorkspaceDir, loaded with config.LoadWorkspace) say, and the
// trust decision for them, without opening the workspace: the CLI asks
// before it opens one.
func ReviewProject(cfg *config.Config) (api.ProjectSettings, error) {
	sk, err := skills.NewProvider()
	if err != nil {
		return api.ProjectSettings{}, err
	}
	if cfg.Skills.Enabled {
		_ = sk.DiscoverExternal(cfg.SkillSearchPaths(cfg.Tools.WorkspaceDir))
	}
	return reviewProject(cfg, sk), nil
}

// TrustProject records the decision for cfg's workspace's project
// settings, whose hash the person was shown (api.ErrProjectChanged if
// they have changed since).
func TrustProject(cfg *config.Config, hash string, trusted bool) error {
	fresh, err := config.LoadWorkspace(configDirArg(cfg), cfg.Tools.WorkspaceDir)
	if err != nil {
		return err
	}
	fresh.Tools.WorkspaceDir = cfg.Tools.WorkspaceDir
	now, err := ReviewProject(fresh)
	if err != nil {
		return err
	}
	if now.Hash == "" || now.Hash != hash {
		return api.ErrProjectChanged
	}
	dir, err := config.CanonicalWorkspace(cfg.Tools.WorkspaceDir)
	if err != nil {
		return err
	}
	return trustStore(cfg).Set(dir, hash, trusted)
}

// ForgetProjectTrust removes the decision for cfg's workspace, so it's
// asked again.
func ForgetProjectTrust(cfg *config.Config) error {
	dir, err := config.CanonicalWorkspace(cfg.Tools.WorkspaceDir)
	if err != nil {
		return err
	}
	return trustStore(cfg).Forget(dir)
}

// reviewProject is ReviewProject with the workspace's skills loaded: the
// project's skills with scripts need trust too, and their content is
// part of what is trusted.
func reviewProject(cfg *config.Config, sk *skills.Provider) api.ProjectSettings {
	p := cfg.Project
	if p == nil {
		return api.ProjectSettings{State: api.TrustNone}
	}
	out := api.ProjectSettings{
		Files: p.Files, Problems: p.Problems,
		Applied: items(p.Applied), Pending: items(p.Pending), Ignored: items(p.Ignored),
	}
	dir, err := config.CanonicalWorkspace(cfg.Tools.WorkspaceDir)
	if err != nil {
		out.State = api.TrustNone
		return out
	}
	var extra []string
	for _, s := range projectSkillsWithScripts(sk, dir) {
		h, err := s.ContentHash()
		if err != nil {
			h = "unreadable"
		}
		extra = append(extra, "skill:"+s.Name+":"+h)
		rel, _ := filepath.Rel(dir, s.HostDir())
		out.Pending = append(out.Pending, api.ProjectItem{File: filepath.ToSlash(rel), Kind: "skill_scripts", Key: s.Name, Value: fmt.Sprint(len(s.Scripts))})
	}
	out.Hash = p.Hash(extra...)
	out.State = trustStore(cfg).State(dir, out.Hash)
	return out
}

// projectSkillsWithScripts are the skills in the workspace that ship
// scripts, by name.
func projectSkillsWithScripts(sk *skills.Provider, dir string) []*skills.Skill {
	var out []*skills.Skill
	for _, s := range sk.List() {
		if len(s.Scripts) == 0 || s.HostDir() == "" {
			continue
		}
		host := s.HostDir()
		if real, err := filepath.EvalSymlinks(host); err == nil {
			host = real
		}
		if rel, err := filepath.Rel(dir, host); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// loadProject decides, as the workspace opens, whether its project
// settings that need trust apply: trusted as they are now, or for this
// run (--trust-project, or the deprecated trust_workspace). Otherwise its
// skills' scripts don't run. It reports what the files ask that a
// project may not.
func (w *Workspace) loadProject(cfg *config.Config, forRun bool, warn func(string)) {
	w.project = reviewProject(cfg, w.skills)
	p := cfg.Project
	if p == nil {
		return
	}
	for _, it := range p.Ignored {
		switch it.Reason {
		case config.ReasonNever:
			warn(i18n.T("project.ignored_never", "file", it.File, "key", it.Key))
		case config.ReasonUnknown:
			warn(i18n.T("project.ignored_unknown", "file", it.File, "key", it.Key))
		}
	}
	for _, prob := range p.Problems {
		warn(i18n.T("project.unreadable", "problem", prob))
	}
	if local := filepath.Join(cfg.Tools.WorkspaceDir, config.ProjectFiles[1]); memory.TrackedByGit(local) {
		warn(i18n.T("project.local_tracked", "path", local))
	}
	switch {
	case w.project.State == api.TrustNone:
	case w.project.State == api.TrustTrusted || forRun:
		p.ApplyTrusted(cfg)
		w.project.Loaded = true
		w.project.Ignored = items(p.Ignored)
	default:
		cfg.Skills.Policy.UntrustedRoots = append(cfg.Skills.Policy.UntrustedRoots, cfg.Tools.WorkspaceDir)
		if dir, err := config.CanonicalWorkspace(cfg.Tools.WorkspaceDir); err == nil && dir != cfg.Tools.WorkspaceDir {
			cfg.Skills.Policy.UntrustedRoots = append(cfg.Skills.Policy.UntrustedRoots, dir)
		}
	}
}

// ProjectSettings are the workspace's project settings, as they were when
// it opened, with the trust decision as it is now.
func (w *Workspace) ProjectSettings() api.ProjectSettings {
	out := w.project
	if dir, err := config.CanonicalWorkspace(w.Dir()); err == nil && out.Hash != "" {
		out.State = trustStore(w.cfg).State(dir, out.Hash)
	}
	return out
}

// TrustProject records trusting or declining the workspace's project
// settings as they are now (their hash): they load when it next opens.
func (w *Workspace) TrustProject(hash string, trusted bool) error {
	return TrustProject(w.cfg, hash, trusted)
}

// ForgetProjectTrust forgets the decision about the workspace's project
// settings: they wait for trust again.
func (w *Workspace) ForgetProjectTrust() error { return ForgetProjectTrust(w.cfg) }

func trustStore(cfg *config.Config) *config.TrustStore {
	dir := cfg.Dir
	if dir == "" {
		dir = config.ConfigDir("")
	}
	return config.OpenTrustStore(dir)
}

// configDirArg is the settings directory to load cfg's again from.
func configDirArg(cfg *config.Config) string { return cfg.Dir }

func items(in []config.ProjectItem) []api.ProjectItem {
	out := make([]api.ProjectItem, len(in))
	for i, it := range in {
		out[i] = api.ProjectItem{File: it.File, Kind: it.Kind, Key: it.Key, Value: it.Value, Reason: it.Reason}
	}
	return out
}
