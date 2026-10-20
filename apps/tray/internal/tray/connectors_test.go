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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/config"
)

// touch writes an empty file, making its folder.
func touch(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, nil, 0o600))
}

// A workspace's connections are read from disk: the global ones, then its
// own, by category; the descriptors' folders and other files aren't
// connections; each says whether it's on there.
func TestConns(t *testing.T) {
	settings, ws, other := t.TempDir(), t.TempDir(), t.TempDir()
	assert.Empty(t, Conns(settings, ws), "none")
	touch(t, filepath.Join(settings, "connectors", "databases", "sales.yaml"))
	touch(t, filepath.Join(settings, "connectors", "kinds", "supabase.yaml"))
	touch(t, filepath.Join(settings, "connectors", "email", "inbox.yml"))
	touch(t, filepath.Join(ws, ".agents", "connectors", "databases", "shop.yaml"))
	touch(t, filepath.Join(ws, ".agents", "connectors", "databases", "notes.md"))
	touch(t, filepath.Join(ws, ".agents", "connectors", "databases", "Bad Name.yaml"))
	touch(t, filepath.Join(ws, ".agents", "connectors", "categories", "crm.yaml"))
	touch(t, filepath.Join(ws, ".agents", "connectors", "readme.yaml"))
	require.NoError(t, config.OpenConnectorScope(settings).Set(ws, "global/databases/sales", false))

	assert.Equal(t, []Conn{
		{Category: "databases", Scope: "global", Name: "sales", On: false},
		{Category: "databases", Scope: "workspace", Name: "shop", On: true},
		{Category: "email", Scope: "global", Name: "inbox", On: true},
	}, Conns(settings, ws))
	assert.Equal(t, []Conn{
		{Category: "databases", Scope: "global", Name: "sales", On: true},
		{Category: "email", Scope: "global", Name: "inbox", On: true},
	}, Conns(settings, other), "another workspace: its own choice, none of the first's files")
}

// The Connectors section: a heading, then each connection checked while
// it's on; a click turns it the other way in that workspace only.
func TestConnEntries(t *testing.T) {
	s := Space{Dir: "/w/shop", Name: "Shop"}
	assert.Empty(t, connEntries(s, nil), "no connections: no section")
	got := SpaceEntries(s, nil, []Flow{{Name: "brief"}}, []Conn{
		{Category: "databases", Scope: "global", Name: "sales", On: false},
		{Category: "databases", Scope: "workspace", Name: "shop", On: true},
	}, Live{})
	require.Len(t, got, 5)
	assert.Equal(t, "Workflows", got[0].Title)
	assert.Equal(t, Entry{Title: "Connectors", Icon: IconConnector, Disabled: true}, got[2])
	assert.Equal(t, Entry{Title: "sales · every workspace", Checkbox: true, Action: Action{Kind: SetConnector, Workspace: "/w/shop", Category: "databases", Scope: "global", Name: "sales", On: true}}, got[3], "off: a click turns it on")
	assert.Equal(t, Entry{Title: "shop", Checkbox: true, Checked: true, Action: Action{Kind: SetConnector, Workspace: "/w/shop", Category: "databases", Scope: "workspace", Name: "shop", On: false}}, got[4], "on: a click turns it off")
	assert.Len(t, SpaceEntries(Space{Dir: "/w/new"}, nil, nil, []Conn{{Category: "databases", Scope: "global", Name: "sales", On: true}}, Live{}), 2, "connections alone make a submenu")
}
