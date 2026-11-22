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
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/agents"
	"github.com/retail-cortex/blitz/pkg/engine/connectors"
	"github.com/retail-cortex/blitz/pkg/engine/flows"
	"github.com/retail-cortex/blitz/pkg/engine/gates"
	"github.com/retail-cortex/blitz/pkg/secrets"
)

// Removing this workspace's items through the blitz tool, and testing a
// connection (spec_067_cli_parity CLP-34): each asked first, with the file
// that goes as a diff; never a global or built-in item (DRV-06).

// fileBody is the text of the file at path without its front matter ("" when
// it can't be read), for a diff against a new body.
func fileBody(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	s := string(b)
	if rest, ok := strings.CutPrefix(s, "---\n"); ok {
		if _, body, ok := strings.Cut(rest, "\n---\n"); ok {
			return strings.TrimLeft(body, "\n")
		}
	}
	return s
}

// fileText is the whole file at path ("" when it can't be read).
func fileText(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}

// relTo is path relative to the workspace, for an approval (as it is when
// outside).
func (w *Workspace) relTo(path string) string {
	if rel, err := filepath.Rel(w.Dir(), path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return mcpFileShown(path)
}

func (w *Workspace) cmdRuleRemove(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	name := str(args, "name")
	i := slices.IndexFunc(w.Rules(), func(r gates.Rule) bool { return r.Name == name })
	if i < 0 {
		return nil, fmt.Errorf("this workspace has no rule %q (rule list)", name)
	}
	r := w.Rules()[i]
	if err := w.askChange(ctx, c, "Remove the rule "+name, fileDiff(w.relTo(r.Path), fileText(r.Path), "")); err != nil {
		return nil, err
	}
	return map[string]any{"removed": name}, w.DeleteRule(name)
}

func (w *Workspace) cmdGuardrailRemove(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	name := str(args, "name")
	i := slices.IndexFunc(w.Guardrails(), func(g gates.Guardrail) bool { return g.Name == name && g.Builtin == "" })
	if i < 0 {
		return nil, fmt.Errorf("this workspace has no guardrail %q of its own (guardrail list; a built-in one is turned off in Studio)", name)
	}
	g := w.Guardrails()[i]
	if err := w.askChange(ctx, c, "Remove the guardrail "+name, fileDiff(w.relTo(g.Path), fileText(g.Path), "")); err != nil {
		return nil, err
	}
	return map[string]any{"removed": name}, w.DeleteGuardrail(name)
}

// ownConnection is the workspace's own connection named name.
func (w *Workspace) ownConnection(name string) (connectors.Connection, error) {
	for _, conn := range w.Connectors() {
		if conn.Name == name && conn.Scope == connectors.ScopeWorkspace {
			return conn, nil
		}
	}
	return connectors.Connection{}, fmt.Errorf("this workspace has no connection %q of its own (connector list; a global one is removed in Settings)", name)
}

func (w *Workspace) cmdConnectorRemove(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	conn, err := w.ownConnection(str(args, "name"))
	if err != nil {
		return nil, err
	}
	if err := w.askChange(ctx, c, "Remove the connection "+conn.Name+" (its secrets stay: secret list)", fileDiff(w.relTo(conn.Path), fileText(conn.Path), "")); err != nil {
		return nil, err
	}
	return map[string]any{"removed": conn.Name}, w.DeleteConnector(ctx, conn.Category, conn.Scope, conn.Name)
}

// cmdConnectorTest connects with a connection's saved settings and lists
// its tools.
func (w *Workspace) cmdConnectorTest(ctx context.Context, _ api.BlitzCommand, args map[string]any) (any, error) {
	name := str(args, "name")
	for _, conn := range w.Connectors() {
		if conn.Name != name {
			continue
		}
		tools, err := w.TestConnector(ctx, conn, nil)
		if err != nil {
			return map[string]any{"name": name, "ok": false, "error": err.Error()}, nil
		}
		names := make([]string, 0, len(tools))
		for _, t := range tools {
			names = append(names, t.Name)
		}
		return map[string]any{"name": name, "ok": true, "tools": names}, nil
	}
	return nil, fmt.Errorf("there's no connection %q (connector list)", name)
}

func (w *Workspace) cmdSecretRemove(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	name := str(args, "name")
	vault, scope := config.Vault(w.cfg.Dir), config.SecretScope(w.cfg.Dir, w.Dir())
	list, err := vault.List(scope)
	if err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(list, func(e secrets.Entry) bool { return e.Key == name }) {
		return nil, fmt.Errorf("this workspace has no secret %q (secret list; a global one is removed in Settings)", name)
	}
	if err := w.askChange(ctx, c, "Remove this workspace's secret "+name+": what names {secrets:"+name+"} stops working until it's set again", ""); err != nil {
		return nil, err
	}
	return map[string]any{"removed": name}, vault.Delete(scope, name)
}

func (w *Workspace) cmdWorkflowRemove(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	name := str(args, "name")
	i := slices.IndexFunc(w.Workflows(), func(f flows.Flow) bool { return f.Name == name })
	if i < 0 {
		return nil, fmt.Errorf("this workspace has no workflow %q (workflow list)", name)
	}
	f := w.Workflows()[i]
	if err := w.askChange(ctx, c, "Remove the workflow "+name+" (its runs' receipts stay)", fileDiff(w.relTo(f.Path), fileText(f.Path), "")); err != nil {
		return nil, err
	}
	return map[string]any{"removed": name}, w.DeleteWorkflow(name)
}

func (w *Workspace) cmdSkillRemove(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	name := str(args, "name")
	_, dir, err := w.ownSkill(name)
	if err != nil {
		return nil, fmt.Errorf("this workspace has no skill %q of its own (skill list): %w", name, err)
	}
	path := filepath.Join(dir, name, "SKILL.md")
	if err := w.askChange(ctx, c, "Remove the skill "+name+" and its folder (its scripts and references too)", fileDiff(w.relTo(path), fileText(path), "")); err != nil {
		return nil, err
	}
	return map[string]any{"removed": name}, w.DeleteSkill(name)
}

// cmdAgentRemove removes an agent of this workspace's own (not a built-in
// one), asked with its file.
func (w *Workspace) cmdAgentRemove(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	name := str(args, "name")
	path := filepath.Join(w.ProjectAgentsDir(), name+".md")
	if w.agents.IsBuiltin(name) {
		return nil, fmt.Errorf("%s is a built-in agent: it can be turned off in Studio, not removed", name)
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("this workspace has no agent %q of its own (agent list)", name)
	}
	if err := w.askChange(ctx, c, "Remove the agent "+name+" (its evals stay)", fileDiff(w.relTo(path), fileText(path), "")); err != nil {
		return nil, err
	}
	if err := agents.Delete(w.ProjectAgentsDir(), name); err != nil {
		return nil, err
	}
	w.RefreshAgents(ctx)
	return map[string]any{"removed": name}, nil
}
