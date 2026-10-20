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
	"slices"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/connectors"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
)

// Studio's connectors (spec_connectors_047): the catalog of categories
// and kinds, the workspace's connection files and the global ones, saved,
// tested, tried and read; a save or delete serves the files as they are
// now.

// connectorTrust is the store of the workspace connection files the user
// trusted.
func connectorTrust(cfg *config.Config) *config.SkillTrust {
	dir := cfg.Dir
	if dir == "" {
		dir = config.ConfigDir("")
	}
	return config.OpenConnectorTrust(dir)
}

// connectorTrusted says whether the person trusted a workspace connection
// file: they trusted the project's settings, or saved it as it is now.
func (w *Workspace) connectorTrusted(file string) bool {
	if w.project.Loaded {
		return true
	}
	return connectorTrust(w.cfg).Reviewed(file, connectors.Hash(file))
}

// connectorScope is the store of the connections the person turned off,
// by workspace.
func connectorScope(cfg *config.Config) *config.ConnectorScope {
	dir := cfg.Dir
	if dir == "" {
		dir = config.ConfigDir("")
	}
	return config.OpenConnectorScope(dir)
}

// connectorOn says whether the connection with key is on in the workspace
// in dir (CON-17): every one is, until the person turns it off.
func (w *Workspace) connectorOn(dir string) func(key string) bool {
	off := connectorScope(w.cfg).Off(dir)
	return func(key string) bool { return !off[key] }
}

// withConnectors is servers with the workspace's connector servers, the
// connections turned off left out.
func (w *Workspace) withConnectors(servers []config.MCPServerConfig, prefixDir, dir string, warn func(string)) []config.MCPServerConfig {
	return connectors.With(servers, prefixDir, dir, w.connectorTrusted, w.connectorOn(dir), warn)
}

// ConnectorCatalog is the categories and kinds of connectors there are
// for the workspace, with any descriptor's problem.
func (w *Workspace) ConnectorCatalog() connectors.Catalog {
	return connectors.Load(w.cfg.Dir, w.Dir())
}

// Connectors are the workspace's connections, every category's, the
// global ones first, each saying whether the person trusted it and
// whether it's on here.
func (w *Workspace) Connectors() []connectors.Connection {
	list := connectors.List(w.ConnectorCatalog(), w.cfg.Dir, w.Dir())
	on := w.connectorOn(w.Dir())
	for i := range list {
		list[i].Trusted = list[i].Scope == connectors.ScopeGlobal || w.connectorTrusted(list[i].Path)
		list[i].On = on(list[i].Key())
	}
	return list
}

// SetConnectorOn turns a connection on or off in this workspace (CON-17;
// connectors.ErrUnknown when it isn't there): one that's off keeps its
// file and secrets, Studio can still test it, and the agents here don't
// get its tools. Its category's server is served again.
func (w *Workspace) SetConnectorOn(ctx context.Context, category, scope, name string, on bool) error {
	if !slices.ContainsFunc(w.Connectors(), func(c connectors.Connection) bool {
		return c.Category == category && c.Scope == scope && c.Name == name
	}) {
		return fmt.Errorf("%w: %q", connectors.ErrUnknown, name)
	}
	if err := connectorScope(w.cfg).Set(w.Dir(), connectors.Key(scope, category, name), on); err != nil {
		return err
	}
	return w.ReloadConnectors(ctx)
}

// ConnectorState is how a category's server is doing: idle (not used
// yet), connected, failed (Error says why), or "" when it has no
// connections.
func (w *Workspace) ConnectorState(category string) tools.MCPState {
	c, ok := w.ConnectorCatalog().Category(category)
	if !ok {
		return tools.MCPState{}
	}
	for _, st := range w.tools.MCP().States() {
		if st.Name == c.Server {
			return st
		}
	}
	return tools.MCPState{}
}

// HasConnectorSecret says whether a connection's secret field (its
// password) has a value.
func (w *Workspace) HasConnectorSecret(c connectors.Connection, field string) bool {
	cat, ok := w.ConnectorCatalog().Category(c.Category)
	return ok && connectors.HasSecret(w.cfg.Dir, w.Dir(), cat, c, field)
}

// SaveConnector writes a connection from Studio's form, in place of
// previous ("" for a new one), keeping the secret values given; problems,
// with nothing written, when it can't be saved. Saving a workspace one
// trusts it as written. The servers restart with it.
func (w *Workspace) SaveConnector(ctx context.Context, c connectors.Connection, values map[string]string, previous string) (connectors.Connection, []string, error) {
	saved, problems, err := connectors.Save(w.ConnectorCatalog(), w.cfg.Dir, w.Dir(), c, values, previous)
	if err != nil || len(problems) > 0 {
		return saved, problems, err
	}
	if saved.Scope == connectors.ScopeWorkspace {
		if err := connectorTrust(w.cfg).Trust(saved.Path, connectors.Hash(saved.Path)); err != nil {
			return saved, nil, err
		}
	}
	if previous != "" { // a rename keeps whether it's on, in every workspace
		if err := connectorScope(w.cfg).Rename(connectors.Key(saved.Scope, saved.Category, previous), saved.Key()); err != nil {
			return saved, nil, err
		}
	}
	saved.Trusted = true
	saved.On = w.connectorOn(w.Dir())(saved.Key())
	return saved, nil, w.ReloadConnectors(ctx)
}

// DeleteConnector removes a category's connection and its secrets
// (connectors.ErrUnknown when it isn't there).
func (w *Workspace) DeleteConnector(ctx context.Context, category, scope, name string) error {
	if err := connectors.Delete(w.ConnectorCatalog(), w.cfg.Dir, w.Dir(), category, scope, name); err != nil {
		return err
	}
	if err := connectorScope(w.cfg).Forget(connectors.Key(scope, category, name)); err != nil {
		return err
	}
	return w.ReloadConnectors(ctx)
}

// connectorEnv is where Studio tests a connection: the secret values
// typed.
func (w *Workspace) connectorEnv(values map[string]string) connectors.Env {
	return connectors.Env{PrefixDir: w.cfg.Dir, Workspace: w.Dir(), Values: values}
}

// TestConnector connects to a connection as written and lists its tools.
func (w *Workspace) TestConnector(ctx context.Context, c connectors.Connection, values map[string]string) ([]connectors.ToolInfo, error) {
	return connectors.TestConnection(ctx, w.ConnectorCatalog(), w.connectorEnv(values), c)
}

// TryConnectorTool runs one of a connection's tools, as written, with
// args.
func (w *Workspace) TryConnectorTool(ctx context.Context, c connectors.Connection, values map[string]string, tool string, args map[string]any) (string, bool, error) {
	return connectors.TryTool(ctx, w.ConnectorCatalog(), w.connectorEnv(values), c, tool, args)
}

// ReadConnectorSchema reads a connection's tables and their columns, as
// written: connectors.ErrNoTables when it can read none.
func (w *Workspace) ReadConnectorSchema(ctx context.Context, c connectors.Connection, values map[string]string) ([]connectors.Table, error) {
	return connectors.ReadSchema(ctx, w.ConnectorCatalog(), w.connectorEnv(values), c)
}

// ReloadConnectors serves the connection files as they are now: each
// category's server starts, restarts with the changed files or secrets,
// or stops when there are none.
func (w *Workspace) ReloadConnectors(ctx context.Context) error {
	return w.applyMCP(ctx, w.mcpConfig())
}

// ErrNoQueryBuilder is a connection whose kind can't draft queries.
var ErrNoQueryBuilder = errors.New("its kind has no query builder")

// connectorKind is a connection's kind, when it has capability c.
func (w *Workspace) connectorKind(c connectors.Connection, capability connectors.Capability) (connectors.Category, connectors.Kind, error) {
	cat := w.ConnectorCatalog()
	category, ok := cat.Category(c.Category)
	if !ok {
		return connectors.Category{}, connectors.Kind{}, connectors.ErrUnknown
	}
	k, ok := cat.Kind(c.Kind)
	if c.Custom || !ok || !slices.Contains(k.Capabilities, capability) {
		return category, k, ErrNoQueryBuilder
	}
	return category, k, nil
}
