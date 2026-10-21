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

package config

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Where connectors' files live (spec_connectors_047), shared by the
// engine's connectors and the tray, which lists them without the engine:
// a connectors folder in the settings folder (global) and in a
// workspace's .agents, holding descriptors (kinds/, categories/) and a
// folder per category of connection files.

// ConnectorsFolder is a connectors folder's name.
const ConnectorsFolder = "connectors"

// Descriptor folders, in a connectors folder: no category takes their
// names.
const (
	ConnectorKindsFolder      = "kinds"
	ConnectorCategoriesFolder = "categories"
)

// Connection scopes.
const (
	ConnectorScopeWorkspace = "workspace"
	ConnectorScopeGlobal    = "global"
)

// connectorNameRE is a connection's (or a query's) name.
var connectorNameRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// ValidConnectorName says whether name can name a connection or a query:
// lower case letters, digits and _, starting with a letter, at most 40.
func ValidConnectorName(name string) bool { return connectorNameRE.MatchString(name) }

// GlobalConnectorsDir is the global connectors folder.
func GlobalConnectorsDir(prefixDir string) string {
	return filepath.Join(ConfigDir(prefixDir), ConnectorsFolder)
}

// WorkspaceConnectorsDir is a workspace's connectors folder.
func WorkspaceConnectorsDir(workspace string) string {
	return filepath.Join(workspace, ".agents", ConnectorsFolder)
}

// ConnectorKey names a connection in a workspace's choices
// (ConnectorScope): <scope>/<category>/<name>.
func ConnectorKey(scope, category, name string) string {
	return scope + "/" + category + "/" + name
}

// ConnectorFiles are a folder's YAML files (.yaml, .yml), sorted; hidden
// ones aren't.
func ConnectorFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && !strings.HasPrefix(n, ".") && (strings.HasSuffix(n, ".yaml") || strings.HasSuffix(n, ".yml")) {
			out = append(out, filepath.Join(dir, n))
		}
	}
	return out
}

// ConnectorFileName is a connector file's name without its extension: a
// connection's name, a descriptor's ID.
func ConnectorFileName(file string) string {
	return strings.TrimSuffix(strings.TrimSuffix(filepath.Base(file), ".yaml"), ".yml")
}

// ConnectorCategoryDirs are the category folders in a connectors folder
// (not the descriptors' folders, nor hidden ones), sorted.
func ConnectorCategoryDirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() && !strings.HasPrefix(n, ".") && !slices.Contains([]string{ConnectorKindsFolder, ConnectorCategoriesFolder}, n) {
			out = append(out, filepath.Join(dir, n))
		}
	}
	return out
}
