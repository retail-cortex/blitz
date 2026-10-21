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

package databases

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"gopkg.in/yaml.v3"

	"github.com/retail-cortex/blitz/pkg/engine/connectors"
)

// A connection's paths (spec_databases_046 DB-16): a setting its kind
// marks as a path (SQLite's database) is written as the person wrote it,
// so a committed file means the same on every machine, and resolved only
// when Blitz connects: a relative one against the workspace's folder (a
// workspace connection) or the home folder (a global one), ~/ as the home
// folder. A file that isn't there is the connection's problem: SQLite
// would otherwise make an empty one where the service runs.

// pathKeys are the source settings that name a file, by the source type
// of each kind that marks them.
func pathKeys(kinds []connectors.Kind) map[string][]string {
	out := map[string][]string{}
	for _, k := range kinds {
		for _, f := range k.Fields {
			if f.Type == connectors.FieldPath && k.Source != "" && !slices.Contains(out[k.Source], f.Key) {
				out[k.Source] = append(out[k.Source], f.Key)
			}
		}
	}
	return out
}

// baseOf is the folder a connection file's relative paths start from:
// its workspace's, for <workspace>/.agents/connectors/<category>/<name>,
// else (a global one) the home folder.
func baseOf(file string) string {
	connectorsDir := filepath.Dir(filepath.Dir(file))
	if ws := filepath.Dir(filepath.Dir(connectorsDir)); connectors.WorkspaceDir(ws) == connectorsDir {
		return ws
	}
	return homeDir()
}

// homeDir is the home folder ("" when there's none).
func homeDir() string {
	home, _ := os.UserHomeDir()
	return home
}

// scopeBase is the folder a connection's relative paths start from, by
// its scope: the workspace's, or the home folder.
func scopeBase(scope, workspace string) string {
	if scope != connectors.ScopeGlobal && workspace != "" {
		return workspace
	}
	return homeDir()
}

// errNoFile is a path setting naming a file that isn't there.
var errNoFile = errors.New("no file")

// resolver is the rewrite embed.LoadWith gives each file: its sources'
// path settings resolved against base(file), and a file that isn't there
// an error.
func resolver(keys map[string][]string, base func(file string) string) func(string, []byte) ([]byte, error) {
	return func(file string, raw []byte) ([]byte, error) {
		return resolvePaths(raw, keys, base(file))
	}
}

// resolvePaths rewrites raw, a connection file, with its sources' path
// settings resolved against base; raw as it is when none needs it.
func resolvePaths(raw []byte, keys map[string][]string, base string) ([]byte, error) {
	if len(keys) == 0 {
		return raw, nil
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	var docs []*yaml.Node
	changed := false
	for {
		var doc yaml.Node
		if err := dec.Decode(&doc); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return raw, nil // Toolbox says what's wrong with it
		}
		docs = append(docs, &doc)
		m := &doc
		if m.Kind == yaml.DocumentNode && len(m.Content) == 1 {
			m = m.Content[0]
		}
		if m.Kind != yaml.MappingNode || value(m, "kind") == nil || value(m, "kind").Value != "source" || value(m, "type") == nil {
			continue
		}
		for _, k := range keys[value(m, "type").Value] {
			v := value(m, k)
			if v == nil || v.Kind != yaml.ScalarNode {
				continue
			}
			p, isFile := connectors.ResolvePath(v.Value, base)
			if !isFile {
				continue
			}
			if _, err := os.Stat(p); err != nil {
				return nil, fmt.Errorf("%w at %s (%s: %s)", errNoFile, p, k, v.Value)
			}
			if p != v.Value {
				v.Value, v.Style, changed = p, 0, true
			}
		}
	}
	if !changed {
		return raw, nil
	}
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	for _, d := range docs {
		if err := enc.Encode(d); err != nil {
			return nil, err
		}
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// value is mapping m's value for key (nil when it has none).
func value(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}
