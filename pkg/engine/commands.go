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
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/commands"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
)

// commandDirs are where command files live, with their source: the user's
// own, then the workspace's (other agents' directories too), which win.
func (w *Workspace) commandDirs() []struct{ dir, source string } {
	out := []struct{ dir, source string }{
		{config.ExpandHome("~/.blitz/commands"), "user"},
	}
	for _, d := range []string{".blitz/commands", ".claude/commands", ".agents/workflows"} {
		out = append(out, struct{ dir, source string }{filepath.Join(w.Dir(), filepath.FromSlash(d)), "project"})
	}
	return out
}

// commands returns every command by name: bundled, then skills (as
// /skill-name), then the user's and the project's, a later one winning.
// Files are read each time, so a new command works without restarting.
func (w *Workspace) commands() (map[string]commands.Command, error) {
	byName := map[string]commands.Command{}
	for _, c := range commands.Bundled() {
		byName[c.Name] = c
	}
	for _, s := range w.skills.List() {
		name := strings.ToLower(s.Name)
		if _, taken := byName[name]; taken || !commands.ValidName(name) {
			continue
		}
		byName[name] = commands.Command{
			Name: name, Description: s.Description, Source: "skill",
			Body: fmt.Sprintf("Use the skill %q for this request: $ARGUMENTS\n\nThe skill's instructions:\n\n%s", s.Name, s.Content),
		}
	}
	var errs []error
	for _, d := range w.commandDirs() {
		list, err := commands.Load(d.dir, d.source)
		if err != nil {
			errs = append(errs, err)
		}
		for _, c := range list {
			byName[c.Name] = c
		}
	}
	return byName, errors.Join(errs...)
}

// ListCommands returns the custom slash commands, by name. Files that
// don't parse are reported through the workspace's warnings.
func (w *Workspace) ListCommands() []api.CommandInfo {
	byName, err := w.commands()
	if err != nil {
		w.warnOnce(err.Error())
	}
	out := make([]api.CommandInfo, 0, len(byName))
	for _, c := range byName {
		out = append(out, api.CommandInfo{Name: c.Name, Description: c.Description, ArgumentHint: c.ArgumentHint, Source: c.Source})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// expandCommand turns a turn whose Text is "/name args" into the
// command's prompt and the options its frontmatter asks for.
func (w *Workspace) expandCommand(ctx context.Context, t *api.Turn) ([]runtime.ExecOption, error) {
	line := strings.TrimPrefix(strings.TrimSpace(t.Text), "/")
	name, args, _ := strings.Cut(line, " ")
	name = strings.ToLower(name)
	byName, _ := w.commands()
	c, ok := byName[name]
	if !ok {
		return nil, fmt.Errorf("%w: /%s", api.ErrUnknownCommand, name)
	}
	t.Prompt = c.Expand(args)
	if c.Plan { // read-only, with the command's own instructions
		t.ReadOnly = "/" + name
	}
	var opts []runtime.ExecOption
	if c.Agent != "" {
		if _, ok := w.agents.Get(c.Agent); !ok {
			return nil, &api.UnknownAgentError{Name: c.Agent}
		}
		opts = append(opts, runtime.WithAgent(c.Agent))
	}
	if c.Model != "" {
		llm, err := w.newModel(ctx, w.cfg, c.Model)
		if err != nil {
			return nil, fmt.Errorf("/%s: model %q: %s", name, c.Model, ModelErrorSummary(err, w.cfg))
		}
		opts = append(opts, runtime.WithModel(llm))
	}
	if len(c.AllowedTools) > 0 {
		opts = append(opts, runtime.WithAllowedTools(c.AllowedTools))
	}
	return opts, nil
}
