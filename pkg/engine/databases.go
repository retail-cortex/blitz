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

	"github.com/retail-cortex/blitz/pkg/engine/databases"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
)

// Studio's databases (spec_databases_046): the workspace's database
// files and the global ones, saved, tested and tried; a save or delete
// serves the files as they are now.

// Databases are the workspace's database connections, the global ones
// first.
func (w *Workspace) Databases() []databases.Connection {
	return databases.List(w.cfg.Dir, w.Dir())
}

// DatabaseState is how the databases server is doing: idle (not used
// yet), connected, failed (Error says why), or "" when there are no
// database files.
func (w *Workspace) DatabaseState() tools.MCPState {
	for _, st := range w.tools.MCP().States() {
		if st.Name == databases.Server {
			return st
		}
	}
	return tools.MCPState{}
}

// HasDatabaseSecret says whether a connection's secret field (its
// password) has a value.
func (w *Workspace) HasDatabaseSecret(c databases.Connection, field string) bool {
	return databases.HasSecret(w.cfg.Dir, w.Dir(), c, field)
}

// SaveDatabase writes a connection from Studio's form, in place of
// previous ("" for a new one), keeping the secret values given; problems,
// with nothing written, when it can't be saved. The databases server
// restarts with it.
func (w *Workspace) SaveDatabase(ctx context.Context, c databases.Connection, values map[string]string, previous string) (databases.Connection, []string, error) {
	saved, problems, err := databases.Save(w.cfg.Dir, w.Dir(), c, values, previous)
	if err != nil || len(problems) > 0 {
		return saved, problems, err
	}
	return saved, nil, w.ReloadDatabases(ctx)
}

// DeleteDatabase removes a connection and its secrets (ErrUnknown when
// it isn't there).
func (w *Workspace) DeleteDatabase(ctx context.Context, scope, name string) error {
	if err := databases.Delete(w.cfg.Dir, w.Dir(), scope, name); err != nil {
		return err
	}
	return w.ReloadDatabases(ctx)
}

// TestDatabase connects to a connection as written and lists its tools.
func (w *Workspace) TestDatabase(ctx context.Context, c databases.Connection, values map[string]string) ([]databases.ToolInfo, error) {
	return databases.Test(ctx, w.cfg.Dir, w.Dir(), c, values)
}

// TryDatabaseTool runs one of a connection's tools, as written, with
// args.
func (w *Workspace) TryDatabaseTool(ctx context.Context, c databases.Connection, values map[string]string, tool string, args map[string]any) (string, bool, error) {
	return databases.Try(ctx, w.cfg.Dir, w.Dir(), c, values, tool, args)
}
