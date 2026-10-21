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
	"slices"

	"google.golang.org/adk/v2/model"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/connectors"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
)

// GlobalConnectors are the person's own connections, every workspace's
// (spec_connectors_047 CON-18): Settings › Connectors changes them, never
// a workspace's Studio. They're read with no workspace: the built-in and
// global descriptors only, the global secrets, and the built-in sql-query
// skill.
type GlobalConnectors struct {
	// Config is the global settings (config.Load).
	Config *config.Config
	// NewModel makes the query builder's model (nil: runtime.NewModel).
	NewModel func(ctx context.Context, cfg *config.Config, name string) (model.LLM, error)
}

// Dirs is where the global connections are.
func (g GlobalConnectors) Dirs() connectors.Dirs { return connectors.Dirs{Prefix: g.Config.Dir} }

// Catalog is the built-in and global descriptors, with any problem.
func (g GlobalConnectors) Catalog() connectors.Catalog {
	return connectors.Load(g.Config.Dir, "")
}

// List is the global connections, every category's; each is the
// person's own, so trusted.
func (g GlobalConnectors) List() []connectors.Connection {
	list := connectors.List(g.Catalog(), g.Dirs())
	for i := range list {
		list[i].Trusted, list[i].On = true, true
	}
	return list
}

// HasSecret says whether a global connection's secret field has a value.
func (g GlobalConnectors) HasSecret(c connectors.Connection, field string) bool {
	cat, ok := g.Catalog().Category(c.Category)
	return ok && connectors.HasSecret(g.Dirs(), cat, global(c), field)
}

// Save writes a global connection, in place of previous ("" for a new
// one), keeping the secret values given as global secrets; problems,
// with nothing written, when it can't be saved. A rename keeps whether
// it's on in each workspace.
func (g GlobalConnectors) Save(_ context.Context, c connectors.Connection, values map[string]string, previous string) (connectors.Connection, []string, error) {
	saved, problems, err := connectors.Save(g.Catalog(), g.Dirs(), global(c), values, previous)
	if err != nil || len(problems) > 0 {
		return saved, problems, err
	}
	if previous != "" {
		if err := connectorScope(g.Config).Rename("", connectors.Key(connectors.ScopeGlobal, saved.Category, previous), saved.Key()); err != nil {
			return saved, nil, err
		}
	}
	saved.Trusted, saved.On = true, true
	return saved, nil, nil
}

// Delete removes a global connection and its secrets
// (connectors.ErrUnknown when it isn't there), and what each workspace
// chose for it.
func (g GlobalConnectors) Delete(_ context.Context, r connectors.Ref) error {
	r.Scope = connectors.ScopeGlobal
	if err := connectors.Delete(g.Catalog(), g.Dirs(), r); err != nil {
		return err
	}
	return connectorScope(g.Config).Forget("", r.Key())
}

// env is where a global connection is tested: the secret values typed,
// else the global ones.
func (g GlobalConnectors) env(values map[string]string) connectors.Env {
	return connectors.Env{Dirs: g.Dirs(), Values: values}
}

// global is c as a global connection.
func global(c connectors.Connection) connectors.Connection {
	c.Scope = connectors.ScopeGlobal
	return c
}

// Test connects to a global connection as written and lists its tools.
func (g GlobalConnectors) Test(ctx context.Context, c connectors.Connection, values map[string]string) ([]connectors.ToolInfo, error) {
	return connectors.TestConnection(ctx, g.Catalog(), g.env(values), global(c))
}

// Try runs one of a global connection's tools, as written, with args.
func (g GlobalConnectors) Try(ctx context.Context, c connectors.Connection, values map[string]string, tool string, args map[string]any) (string, bool, error) {
	return connectors.TryTool(ctx, g.Catalog(), g.env(values), global(c), tool, args)
}

// ReadSchema reads a global connection's tables and their columns, as
// written: connectors.ErrNoTables when it can read none.
func (g GlobalConnectors) ReadSchema(ctx context.Context, c connectors.Connection, values map[string]string) ([]connectors.Table, error) {
	return connectors.ReadSchema(ctx, g.Catalog(), g.env(values), global(c))
}

// DraftQuery is the query builder for a global connection
// (Workspace.DraftConnectorQuery), with the built-in sql-query skill.
func (g GlobalConnectors) DraftQuery(ctx context.Context, c connectors.Connection, values map[string]string, request string) (QueryDraft, error) {
	newModel := g.NewModel
	if newModel == nil {
		newModel = runtime.NewModel
	}
	c = global(c)
	return draftQuery(ctx, queryBuilder{
		cfg:       g.Config,
		catalog:   g.Catalog(),
		model:     func(ctx context.Context) (model.LLM, error) { return suggestionsLLM(ctx, g.Config, newModel) },
		redact:    SecretRedactor(g.Config).String,
		skill:     builtinSkill(SQLSkill),
		schema:    func(ctx context.Context) ([]connectors.Table, error) { return g.ReadSchema(ctx, c, values) },
		schemaKey: schemaKey("", c, values),
	}, c, request)
}

// builtinSkill finds the built-in skill name.
func builtinSkill(name string) func() (*skills.Skill, bool) {
	return func() (*skills.Skill, bool) {
		all, err := skills.Builtins()
		if err != nil {
			return nil, false
		}
		i := slices.IndexFunc(all, func(s *skills.Skill) bool { return s.Name == name })
		if i < 0 {
			return nil, false
		}
		return all[i], true
	}
}

var _ ConnectorSet = GlobalConnectors{}
