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

// ConnectorSet is a set of connections the API serves alike: a
// workspace's in Studio (Workspace.ConnectorSet; the global ones listed,
// not changed), or the global ones in Settings (GlobalConnectors).
type ConnectorSet interface {
	// Dirs is where its connections are (secrets are kept by them).
	Dirs() connectors.Dirs
	Catalog() connectors.Catalog
	List() []connectors.Connection
	Save(ctx context.Context, c connectors.Connection, values map[string]string, previous string) (connectors.Connection, []string, error)
	Delete(ctx context.Context, r connectors.Ref) error
	Test(ctx context.Context, c connectors.Connection, values map[string]string) ([]connectors.ToolInfo, error)
	Try(ctx context.Context, c connectors.Connection, values map[string]string, tool string, args map[string]any) (string, bool, error)
	ReadSchema(ctx context.Context, c connectors.Connection, values map[string]string) ([]connectors.Table, error)
	DraftQuery(ctx context.Context, c connectors.Connection, values map[string]string, request string) (QueryDraft, error)
}

// settingsDir is the settings folder of cfg.
func settingsDir(cfg *config.Config) string { return config.ConfigDir(cfg.Dir) }

// connectorTrust is the store of the workspace connection files the user
// trusted.
func connectorTrust(cfg *config.Config) *config.HashTrust {
	return config.OpenConnectorTrust(settingsDir(cfg))
}

// connectorScope is the store of the connections the person turned off,
// by workspace.
func connectorScope(cfg *config.Config) *config.ConnectorScope {
	return config.OpenConnectorScope(settingsDir(cfg))
}

// connectorDirs is where the workspace's connections are.
func (w *Workspace) connectorDirs() connectors.Dirs {
	return connectors.Dirs{Prefix: w.cfg.Dir, Workspace: w.Dir()}
}

// connectorTrusted says whether the person trusted a workspace connection
// file: they trusted the project's settings, or saved it as it is now.
func (w *Workspace) connectorTrusted(file string) bool {
	if w.project.Loaded {
		return true
	}
	return connectorTrust(w.cfg).Reviewed(file, connectors.Hash(file))
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
	return connectors.With(servers, connectors.ServeOptions{
		Dirs:    connectors.Dirs{Prefix: prefixDir, Workspace: dir},
		Trusted: w.connectorTrusted,
		On:      w.connectorOn(dir),
		Warn:    warn,
	})
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
	list := connectors.List(w.ConnectorCatalog(), w.connectorDirs())
	on := w.connectorOn(w.Dir())
	for i := range list {
		list[i].Trusted = list[i].Scope == connectors.ScopeGlobal || w.connectorTrusted(list[i].Path)
		list[i].On = on(list[i].Key())
	}
	return list
}

// connector is the workspace's connection r, as Connectors lists it.
func (w *Workspace) connector(r connectors.Ref) (connectors.Connection, error) {
	list := w.Connectors()
	if i := slices.IndexFunc(list, func(c connectors.Connection) bool { return c.Ref() == r }); i >= 0 {
		return list[i], nil
	}
	return connectors.Connection{}, fmt.Errorf("%w: %q", connectors.ErrUnknown, r.Name)
}

// SetConnectorOn turns a connection on or off in this workspace (CON-17;
// connectors.ErrUnknown when it isn't there): one that's off keeps its
// file and secrets, Studio can still test it, and the agents here don't
// get its tools. Its category's server is served again.
func (w *Workspace) SetConnectorOn(ctx context.Context, category, scope, name string, on bool) error {
	r := connectors.Ref{Scope: scope, Category: category, Name: name}
	if _, err := w.connector(r); err != nil {
		return err
	}
	if err := connectorScope(w.cfg).Set(w.Dir(), r.Key(), on); err != nil {
		return err
	}
	return w.ReloadConnectors(ctx)
}

// ConnectorState is how a category's server is doing: idle (not used
// yet), connected, failed (Error says why), or "" when it has no
// connections.
func (w *Workspace) ConnectorState(category connectors.Category) tools.MCPState {
	for _, st := range w.tools.MCP().States() {
		if st.Name == category.Server {
			return st
		}
	}
	return tools.MCPState{}
}

// HasConnectorSecret says whether a connection's secret field (its
// password) has a value.
func (w *Workspace) HasConnectorSecret(c connectors.Connection, field string) bool {
	cat, ok := w.ConnectorCatalog().Category(c.Category)
	return ok && connectors.HasSecret(w.connectorDirs(), cat, c, field)
}

// SaveConnector writes one of the workspace's connections from Studio's
// form, in place of previous ("" for a new one), keeping the secret
// values given; problems, with nothing written, when it can't be saved.
// Saving it trusts it as written (a renamed one's old trust goes). The
// servers restart with it. A global one is Settings' to change
// (ErrGlobalConnector, CON-18).
func (w *Workspace) SaveConnector(ctx context.Context, c connectors.Connection, values map[string]string, previous string) (connectors.Connection, []string, error) {
	if c.Scope == connectors.ScopeGlobal {
		return connectors.Connection{}, nil, ErrGlobalConnector
	}
	c.Scope = connectors.ScopeWorkspace
	var old string
	if previous != "" && previous != c.Name {
		if prev, err := w.connector(connectors.Ref{Scope: c.Scope, Category: c.Category, Name: previous}); err == nil {
			old = prev.Path
		}
	}
	saved, problems, err := connectors.Save(w.ConnectorCatalog(), w.connectorDirs(), c, values, previous)
	if err != nil || len(problems) > 0 {
		return saved, problems, err
	}
	trust := connectorTrust(w.cfg)
	if err := trust.Trust(saved.Path, connectors.Hash(saved.Path)); err != nil {
		return saved, nil, err
	}
	if old != "" { // a rename keeps whether it's on, and its old file's trust goes
		if err := errors.Join(trust.Forget(old), connectorScope(w.cfg).Rename(w.Dir(), connectors.Key(saved.Scope, saved.Category, previous), saved.Key())); err != nil {
			return saved, nil, err
		}
	}
	saved.Trusted = true
	saved.On = w.connectorOn(w.Dir())(saved.Key())
	return saved, nil, w.ReloadConnectors(ctx)
}

// ErrGlobalConnector is a change to a global connection asked of a
// workspace: Settings changes those (CON-18).
var ErrGlobalConnector = errors.New("a global connection is changed in Settings, not in a workspace")

// ErrUntrustedConnector is a workspace connection the person hasn't
// trusted, which can't be made global until they save it (CON-19).
var ErrUntrustedConnector = errors.New("save and trust it first")

// DeleteConnector removes one of the workspace's connections, its
// secrets, whether it's on here and its trust (connectors.ErrUnknown when
// it isn't there; ErrGlobalConnector for a global one).
func (w *Workspace) DeleteConnector(ctx context.Context, category, scope, name string) error {
	r := connectors.Ref{Scope: scope, Category: category, Name: name}
	if r.Scope == connectors.ScopeGlobal {
		return ErrGlobalConnector
	}
	c, err := w.connector(r)
	if err != nil {
		return err
	}
	if err := connectors.Delete(w.ConnectorCatalog(), w.connectorDirs(), r); err != nil {
		return err
	}
	if err := errors.Join(connectorScope(w.cfg).Forget(w.Dir(), r.Key()), connectorTrust(w.cfg).Forget(c.Path)); err != nil {
		return err
	}
	return w.ReloadConnectors(ctx)
}

// MakeConnectorGlobal moves one of the workspace's connections, with its
// secrets, to the global folder, so every workspace has it (CON-19):
// problems, with nothing moved, when it can't be; ErrUntrustedConnector
// when the person hasn't trusted it (a clone's file mustn't reach the
// global secrets). Whether it's on here goes with it.
func (w *Workspace) MakeConnectorGlobal(ctx context.Context, category, name string) (connectors.Connection, []string, error) {
	r := connectors.Ref{Scope: connectors.ScopeWorkspace, Category: category, Name: name}
	c, err := w.connector(r)
	if err != nil {
		return connectors.Connection{}, nil, err
	}
	if !c.Trusted {
		return connectors.Connection{}, nil, ErrUntrustedConnector
	}
	saved, problems, err := connectors.MakeGlobal(w.connectorDirs(), category, name)
	if err != nil || len(problems) > 0 {
		return saved, problems, err
	}
	if err := errors.Join(connectorScope(w.cfg).Rename(w.Dir(), r.Key(), saved.Key()), connectorTrust(w.cfg).Forget(c.Path)); err != nil {
		return saved, nil, err
	}
	saved.Trusted = true
	saved.On = w.connectorOn(w.Dir())(saved.Key())
	return saved, nil, w.ReloadConnectors(ctx)
}

// connectorEnv is where Studio tests a connection: the secret values
// typed.
func (w *Workspace) connectorEnv(values map[string]string) connectors.Env {
	return connectors.Env{Dirs: w.connectorDirs(), Values: values}
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

// ConnectorSet is the workspace's connections as the API serves them.
func (w *Workspace) ConnectorSet() ConnectorSet { return workspaceConnectors{w} }

// workspaceConnectors is a Workspace as a ConnectorSet.
type workspaceConnectors struct{ w *Workspace }

func (s workspaceConnectors) Dirs() connectors.Dirs         { return s.w.connectorDirs() }
func (s workspaceConnectors) Catalog() connectors.Catalog   { return s.w.ConnectorCatalog() }
func (s workspaceConnectors) List() []connectors.Connection { return s.w.Connectors() }
func (s workspaceConnectors) Save(ctx context.Context, c connectors.Connection, values map[string]string, previous string) (connectors.Connection, []string, error) {
	return s.w.SaveConnector(ctx, c, values, previous)
}
func (s workspaceConnectors) Delete(ctx context.Context, r connectors.Ref) error {
	return s.w.DeleteConnector(ctx, r.Category, r.Scope, r.Name)
}
func (s workspaceConnectors) Test(ctx context.Context, c connectors.Connection, values map[string]string) ([]connectors.ToolInfo, error) {
	return s.w.TestConnector(ctx, c, values)
}
func (s workspaceConnectors) Try(ctx context.Context, c connectors.Connection, values map[string]string, tool string, args map[string]any) (string, bool, error) {
	return s.w.TryConnectorTool(ctx, c, values, tool, args)
}
func (s workspaceConnectors) ReadSchema(ctx context.Context, c connectors.Connection, values map[string]string) ([]connectors.Table, error) {
	return s.w.ReadConnectorSchema(ctx, c, values)
}
func (s workspaceConnectors) DraftQuery(ctx context.Context, c connectors.Connection, values map[string]string, request string) (QueryDraft, error) {
	return s.w.DraftConnectorQuery(ctx, c, values, request)
}

// ErrNoCapability is a connection whose kind can't do what's asked (a
// custom file, or a kind without the capability).
var ErrNoCapability = errors.New("its kind can't")

// ErrNoQueryBuilder is a connection whose kind can't draft queries.
var ErrNoQueryBuilder = fmt.Errorf("%w draft queries", ErrNoCapability)

// kindWith is c's category and kind in cat, when the kind has capability:
// connectors.ErrUnknown for a category or kind that isn't there, else
// ErrNoQueryBuilder (for the query builder) or ErrNoCapability.
func kindWith(cat connectors.Catalog, c connectors.Connection, capability connectors.Capability) (connectors.Category, connectors.Kind, error) {
	category, ok := cat.Category(c.Category)
	if !ok {
		return connectors.Category{}, connectors.Kind{}, fmt.Errorf("%w: category %q", connectors.ErrUnknown, c.Category)
	}
	if c.Custom {
		return category, connectors.Kind{}, noCapability(capability)
	}
	k, ok := cat.Kind(c.Kind)
	if !ok {
		return category, k, fmt.Errorf("%w: kind %q", connectors.ErrUnknown, c.Kind)
	}
	if !k.Can(capability) {
		return category, k, noCapability(capability)
	}
	return category, k, nil
}

// noCapability is the error for a kind without capability.
func noCapability(capability connectors.Capability) error {
	if capability == connectors.QueryBuilder {
		return ErrNoQueryBuilder
	}
	return fmt.Errorf("%w %s", ErrNoCapability, capability)
}
