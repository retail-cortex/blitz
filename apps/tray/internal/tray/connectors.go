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

package tray

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/i18n"
)

// A workspace's connectors in its submenu (spec_connectors_047 CON-20):
// each connection it reaches, its own and the global ones, checked while
// it's on there; a click turns it off or on in that workspace only, as
// Studio's switch does. Read from disk, as the workflows are, so the
// service needn't open a workspace for the tray to list them.

// Conn is a connection a workspace reaches.
type Conn struct {
	Category string
	// Scope is workspace (.agents/connectors/<category>) or global.
	Scope string
	Name  string
	// On says it's served in the workspace (CON-17).
	On bool
}

// Conns are the connections workspace dir reaches: each category's
// global ones, then its own, by category and name, with whether each is
// on there (the person's choices in the settings folder of prefixDir).
// The folders, names and keys are the engine's (pkg/config).
func Conns(prefixDir, dir string) []Conn {
	off := config.OpenConnectorScope(config.ConfigDir(prefixDir)).Off(dir)
	roots := []struct{ scope, dir string }{
		{config.ConnectorScopeGlobal, config.GlobalConnectorsDir(prefixDir)},
		{config.ConnectorScopeWorkspace, config.WorkspaceConnectorsDir(dir)},
	}
	var out []Conn
	for _, r := range roots {
		for _, catDir := range config.ConnectorCategoryDirs(r.dir) {
			category := filepath.Base(catDir)
			for _, f := range config.ConnectorFiles(catDir) {
				name := config.ConnectorFileName(f)
				if !config.ValidConnectorName(name) {
					continue
				}
				out = append(out, Conn{Category: category, Scope: r.scope, Name: name, On: !off[config.ConnectorKey(r.scope, category, name)]})
			}
		}
	}
	slices.SortStableFunc(out, func(a, b Conn) int { return strings.Compare(a.Category, b.Category) })
	return out
}

// connEntries are a workspace's Connectors section: its heading, then
// each connection, checked while it's on there, a global one saying so;
// a click turns it off or on in that workspace.
func connEntries(s Space, conns []Conn) []Entry {
	if len(conns) == 0 {
		return nil
	}
	out := []Entry{{Title: i18n.T("tray.connectors"), Icon: IconConnector, Disabled: true}}
	for _, c := range conns {
		title := c.Name
		if c.Scope == config.ConnectorScopeGlobal {
			title += " · " + i18n.T("tray.connector.global")
		}
		out = append(out, Entry{Title: title, Checkbox: true, Checked: c.On, Action: Action{Kind: SetConnector, Workspace: s.Dir, Category: c.Category, Scope: c.Scope, Name: c.Name, On: !c.On}})
	}
	return out
}
