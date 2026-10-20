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

package connectors

import (
	"context"
	"slices"
	"sort"
	"sync"

	"github.com/retail-cortex/blitz/pkg/config"
)

// Backend serves kinds of connectors: it writes and reads their files and
// turns a category's files into MCP servers. What else it can do is the
// optional interfaces below (Tester, Tryer, SchemaReader), which
// Capabilities lists.
type Backend interface {
	// ID is the name kinds give it (toolbox).
	ID() string
	// Capabilities are what its kinds may offer.
	Capabilities() []Capability
	// Parse reads a connection file (name, its text) into the form, kinds
	// being the category's kinds it serves; one it can't describe is
	// Custom.
	Parse(cat Category, kinds []Kind, name, text string) Connection
	// Check lists what keeps c, of kind k (nil for a custom one), from
	// being written, beyond its name and required settings.
	Check(k *Kind, c Connection) []string
	// Render writes c, of kind k (nil for a custom one), as its file; its
	// secret settings name their secrets.
	Render(cat Category, k *Kind, c Connection) (string, error)
	// Serve is the MCP servers for a category's connection files; none
	// when there are none.
	Serve(s Serving) []config.MCPServerConfig
}

// Serving is what a backend serves: a category's connection files of
// its kinds, the global ones then the workspace's.
type Serving struct {
	Category  Category
	PrefixDir string
	Workspace string
	Global    []string
	Local     []string
	// Trusted says whether the person trusted a workspace connection file
	// (saved it in Studio, or trusted the project's settings): a trusted
	// one may use global secrets, and its read-only tools run without
	// asking. An untrusted one gets only the workspace's own secrets, and
	// every tool asks first. Nil trusts none.
	Trusted func(file string) bool
}

// Split is the workspace's files, trusted and not.
func (s Serving) Split() (trusted, untrusted []string) {
	for _, f := range s.Local {
		if s.Trusted != nil && s.Trusted(f) {
			trusted = append(trusted, f)
		} else {
			untrusted = append(untrusted, f)
		}
	}
	return trusted, untrusted
}

// Env is where a connection is tested, tried or read: the config folder,
// the workspace, and the secret values typed into the form (by field) in
// place of the kept ones.
type Env struct {
	PrefixDir string
	Workspace string
	Category  Category
	Values    map[string]string
}

// Tester connects to a connection as written and lists its tools.
type Tester interface {
	Test(ctx context.Context, env Env, k *Kind, c Connection) ([]ToolInfo, error)
}

// Tryer runs one of a connection's tools (<connection>_<tool>, or the
// tool alone) as written, with args: what it answered, and whether
// that's the tool's error.
type Tryer interface {
	Try(ctx context.Context, env Env, k *Kind, c Connection, tool string, args map[string]any) (string, bool, error)
}

// SchemaReader reads a connection's tables and their columns; ErrNoTables
// when it can read none.
type SchemaReader interface {
	ReadSchema(ctx context.Context, env Env, k *Kind, c Connection) ([]Table, error)
}

var (
	backendsMu sync.RWMutex
	backends   = map[string]Backend{}
)

// Register makes a backend available to kinds; a package registers its
// own in init.
func Register(b Backend) {
	backendsMu.Lock()
	defer backendsMu.Unlock()
	backends[b.ID()] = b
}

// BackendOf is the backend id.
func BackendOf(id string) (Backend, bool) {
	backendsMu.RLock()
	defer backendsMu.RUnlock()
	b, ok := backends[id]
	return b, ok
}

// Backends are the registered backends' IDs, sorted.
func Backends() []string {
	backendsMu.RLock()
	defer backendsMu.RUnlock()
	ids := make([]string, 0, len(backends))
	for id := range backends {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// supports says whether b has capability c, both by its list and by the
// interface the capability needs.
func supports(b Backend, c Capability) bool {
	if !slices.Contains(b.Capabilities(), c) {
		return false
	}
	switch c {
	case Test:
		_, ok := b.(Tester)
		return ok
	case Try:
		_, ok := b.(Tryer)
		return ok
	case Schema:
		_, ok := b.(SchemaReader)
		return ok
	}
	return true
}
