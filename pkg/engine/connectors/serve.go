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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/retail-cortex/blitz/pkg/config"
)

// With is servers with the workspace's connector servers in place of any
// in-process ones there: each category's connection files that are on
// (on says, by Key; nil: all of them), served by their backends; trusted
// says which workspace files the person trusted (Serving.Trusted). A
// server of the user's own with a connector server's name wins, and warn
// says so.
func With(servers []config.MCPServerConfig, prefixDir, workspace string, trusted func(file string) bool, on func(key string) bool, warn func(string)) []config.MCPServerConfig {
	out := slices.DeleteFunc(slices.Clone(servers), func(s config.MCPServerConfig) bool { return s.InProcess != "" })
	cat := Load(prefixDir, workspace)
	for _, c := range cat.Usable() {
		type group struct{ global, local []string }
		groups := map[string]*group{}
		var order []string
		for _, scope := range []string{ScopeGlobal, ScopeWorkspace} {
			for _, f := range Files(prefixDir, workspace, scope, c.ID) {
				if on != nil && !on(Key(scope, c.ID, idOf(f))) {
					continue
				}
				text, _ := os.ReadFile(f)
				b, ok := backendFor(cat, c, string(text))
				if !ok {
					continue
				}
				g := groups[b.ID()]
				if g == nil {
					g = &group{}
					groups[b.ID()] = g
					order = append(order, b.ID())
				}
				if scope == ScopeGlobal {
					g.global = append(g.global, f)
				} else {
					g.local = append(g.local, f)
				}
			}
		}
		for _, id := range order {
			b, _ := BackendOf(id)
			g := groups[id]
			for _, s := range b.Serve(Serving{Category: c, PrefixDir: prefixDir, Workspace: workspace, Global: g.global, Local: g.local, Trusted: trusted}) {
				if slices.ContainsFunc(out, func(u config.MCPServerConfig) bool { return u.Name == s.Name }) {
					if warn != nil {
						warn(fmt.Sprintf("an MCP server is named %q: the %s connectors aren't served", s.Name, c.ID))
					}
					continue
				}
				out = append(out, s)
			}
		}
	}
	return out
}

// AllFiles are every descriptor and connection file the workspace's
// connectors read, for the settings watch.
func AllFiles(prefixDir, workspace string) []string {
	var out []string
	for _, scope := range []string{ScopeGlobal, ScopeWorkspace} {
		dir := Dir(prefixDir, workspace, scope)
		if dir == "" {
			continue
		}
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.IsDir() {
				out = append(out, yamlFiles(filepath.Join(dir, e.Name()))...)
			}
		}
	}
	return out
}

// Hash is a connection file's content hash, which a trust decision
// records ("" when it can't be read).
func Hash(file string) string {
	b, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Fingerprint changes when the files do (added, removed, changed).
func Fingerprint(files []string) string {
	h := sha256.New()
	for _, f := range files {
		fmt.Fprintf(h, "%s\x00", f)
		if info, err := os.Stat(f); err == nil {
			fmt.Fprintf(h, "%d\x00%d\x00", info.Size(), info.ModTime().UnixNano())
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// can is the backend, kind and connection, prepared, when conn's kind
// has capability c (a custom one: when its backend can).
func can(cat Catalog, env *Env, conn Connection, c Capability) (Backend, *Kind, Connection, error) {
	b, k, conn, err := Prepare(cat, *env, conn)
	if err != nil {
		return nil, nil, conn, err
	}
	if k != nil && !k.Can(c) || k == nil && !supports(b, c) {
		return nil, nil, conn, fmt.Errorf("%s can't %s", conn.Name, c)
	}
	env.Category, _ = cat.Category(conn.Category)
	return b, k, conn, nil
}

// TestConnection connects to conn as written and lists its tools.
func TestConnection(ctx context.Context, cat Catalog, env Env, conn Connection) ([]ToolInfo, error) {
	b, k, conn, err := can(cat, &env, conn, Test)
	if err != nil {
		return nil, err
	}
	return b.(Tester).Test(ctx, env, k, conn)
}

// TryTool runs one of conn's tools as written, with args.
func TryTool(ctx context.Context, cat Catalog, env Env, conn Connection, tool string, args map[string]any) (string, bool, error) {
	b, k, conn, err := can(cat, &env, conn, Try)
	if err != nil {
		return "", false, err
	}
	return b.(Tryer).Try(ctx, env, k, conn, tool, args)
}

// ReadSchema reads conn's tables and their columns: ErrNoTables, saying
// why, when it can read none.
func ReadSchema(ctx context.Context, cat Catalog, env Env, conn Connection) ([]Table, error) {
	b, k, conn, err := can(cat, &env, conn, Schema)
	if err != nil {
		return nil, err
	}
	tables, err := b.(SchemaReader).ReadSchema(ctx, env, k, conn)
	if err != nil {
		return nil, fmt.Errorf("%w in %s: %w", ErrNoTables, conn.Name, err)
	}
	if len(tables) == 0 {
		return nil, fmt.Errorf("%w in %s: it holds none this connection can read", ErrNoTables, conn.Name)
	}
	return tables, nil
}
