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

// Package databases is the connectors' toolbox backend
// (spec_046_databases, spec_047_connectors): MCP Toolbox for Databases,
// run inside Blitz's process (third_party/mcp-toolbox/embed), serving a
// category's connection files (Toolbox YAML: sources, tools, toolsets)
// as one MCP server, its tools named <prefix>__<tool> (db__shop_list_tables).
//
// A ${NAME} in a file is the Blitz secret NAME, the workspace's own, else
// the global one; never an environment variable. A workspace's own files
// reach the global secrets, and their read-only tools run unasked, only
// once the person trusts them (saved in Studio, or the project's settings
// trusted).
package databases

import (
	"context"
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/connectors"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/redact"
	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/retail-cortex/blitz/third_party/mcp-toolbox/embed"
)

// ID is the backend's ID, which kinds name, and its in-process server's.
const ID = "toolbox"

// Backend is the toolbox backend.
type Backend struct{}

func init() {
	connectors.Register(Backend{})
	tools.RegisterInProcess(ID, Transport)
}

// ID is toolbox.
func (Backend) ID() string { return ID }

// Capabilities are all of them: Toolbox connects, runs tools and queries,
// and the kinds say how their schema is read.
func (Backend) Capabilities() []connectors.Capability { return connectors.Capabilities }

// placeholderRE is Toolbox's ${NAME} (or ${NAME:default}).
var placeholderRE = regexp.MustCompile(`\$\{(\w+)(?::[^}]*)?\}`)

// Names are the ${NAME}s the files use, sorted.
func Names(files []string) []string {
	seen := map[string]bool{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, m := range placeholderRE.FindAllSubmatch(b, -1) {
			seen[string(m[1])] = true
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// Vars are the values of the files' ${NAME}s that lookup finds (a secrets
// lookup, as secrets.Vault.Resolve gives) in scope (secrets.AnyScope, or
// secrets.OnlyWorkspace). The values are remembered for every redactor
// to mask.
func Vars(files []string, scope string, lookup func(scope, key string) (string, bool)) map[string]string {
	vars := map[string]string{}
	for _, n := range Names(files) {
		if v, ok := lookup(scope, n); ok {
			vars[n] = v
			redact.Remember(v)
		}
	}
	return vars
}

// Serve is the category's toolbox server, when it has files: in-process,
// its files in Args and their secrets in Env, offered to every agent; its
// tools on read-only sources run without asking. A workspace file the
// person hasn't trusted gets only the workspace's own secrets (its
// ${NAME}s are kept apart in Env: WorkspaceOnly), and its tools all ask
// first. A workspace file that doesn't load on its own is left out, and
// Warn says why, so it can't stop the others being served.
func (Backend) Serve(s connectors.Serving) []config.MCPServerConfig {
	lookup := config.Vault(s.Prefix).Resolve(s.SecretScope(connectors.ScopeWorkspace))
	trusted, untrusted := s.Split()
	vars := Vars(append(slices.Clone(s.Global), trusted...), secrets.AnyScope, lookup)
	for k, v := range Vars(untrusted, secrets.OnlyWorkspace, lookup) {
		vars[workspaceOnly(k)] = v
	}
	only := map[string]bool{}
	for _, f := range untrusted {
		only[f] = true
	}
	keys := pathKeys(connectors.Load(s.Prefix, s.Workspace).Kinds)
	files := slices.Clone(s.Global)
	for _, f := range s.Local {
		if _, err := embed.LoadWith(context.Background(), []string{f}, vars, rewriter(keys, baseOf, only)); err != nil {
			if s.Warn != nil {
				s.Warn(fmt.Sprintf("%s isn't served: %v", f, err))
			}
			delete(only, f)
			continue
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		return nil
	}
	c := config.MCPServerConfig{
		Name:          s.Category.Server,
		Prefix:        s.Category.Prefix,
		InProcess:     ID,
		Args:          files,
		Env:           vars,
		Agents:        []string{"*"},
		Fingerprint:   connectors.Fingerprint(files),
		FileSettings:  keys,
		WorkspaceOnly: slices.Sorted(maps.Keys(only)),
	}
	// A global file that doesn't load still makes the server: listing its
	// tools fails, saying why, in /mcp and Studio.
	var unasked []string
	for _, f := range files {
		if !only[f] {
			unasked = append(unasked, f)
		}
	}
	if len(unasked) > 0 {
		if cfg, err := loadConfig(context.Background(), unasked, vars); err == nil {
			c.ReadOnlyTools = readOnlyTools(cfg)
		}
	}
	return []config.MCPServerConfig{c}
}

// workspaceOnlyPrefix starts the Env name of a ${NAME} in a file that
// gets only the workspace's own secrets.
const workspaceOnlyPrefix = "BLITZ_WORKSPACE_ONLY__"

// workspaceOnly is name's Env name in such a file.
func workspaceOnly(name string) string { return workspaceOnlyPrefix + name }

// keepApart renames raw's ${NAME}s to their workspace-only names, so
// Toolbox fills them from the workspace's own secrets alone.
func keepApart(raw []byte) []byte {
	return placeholderRE.ReplaceAllFunc(raw, func(m []byte) []byte {
		return append([]byte("${"+workspaceOnlyPrefix), m[2:]...)
	})
}

// rewriter is how a file's text is read: its ${NAME}s kept apart when
// it's one of only's, then its relative paths resolved.
func rewriter(keys map[string][]string, base func(string) string, only map[string]bool) func(string, []byte) ([]byte, error) {
	resolve := resolver(keys, base)
	return func(file string, raw []byte) ([]byte, error) {
		if only[file] {
			raw = keepApart(raw)
		}
		return resolve(file, raw)
	}
}

var (
	loadConfig    = embed.Load
	readOnlyTools = embed.ReadOnlyTools
)

// ReadsOnly says whether a SQL statement only reads, as Toolbox's tools
// are marked (a hint: a read-only connection is the guard).
func ReadsOnly(statement string) bool { return embed.ReadsOnly(statement) }

// Transport connects to a toolbox server c: each connection loads its
// files (Args) with their secrets (Env), their file settings
// (FileSettings, which Serve works out from the workspace's catalog)
// resolved where each file belongs (baseOf: its workspace, else the home
// folder), and starts Toolbox, connecting to the databases; closing the
// connection stops it.
func Transport(c config.MCPServerConfig) mcp.Transport {
	only := map[string]bool{}
	for _, f := range c.WorkspaceOnly {
		only[f] = true
	}
	return &transport{files: slices.Clone(c.Args), vars: maps.Clone(c.Env), keys: c.FileSettings, base: baseOf, only: only}
}

type transport struct {
	files []string
	vars  map[string]string
	// keys are the path settings by source type, resolved against base(file).
	keys map[string][]string
	base func(file string) string
	// only are the files that get the workspace's own secrets alone.
	only map[string]bool
}

// Connect starts Toolbox and connects to it.
func (t *transport) Connect(ctx context.Context) (mcp.Connection, error) {
	cfg, err := embed.LoadWith(ctx, t.files, t.vars, rewriter(t.keys, t.base, t.only))
	if err != nil {
		return nil, err
	}
	// The server outlives the call that started it, so it isn't tied to
	// ctx, but connecting to the databases gives up when ctx does.
	type started struct {
		s   *embed.Session
		err error
	}
	ch := make(chan started, 1)
	go func() {
		s, err := embed.Start(context.WithoutCancel(ctx), cfg, nil)
		ch <- started{s, err}
	}()
	var s *embed.Session
	select {
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		s = r.s
	case <-ctx.Done():
		go func() {
			if r := <-ch; r.s != nil {
				_ = r.s.Close()
			}
		}()
		return nil, fmt.Errorf("connecting to the databases: %w", ctx.Err())
	}
	conn, err := (&mcp.IOTransport{Reader: s, Writer: s}).Connect(ctx)
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	return conn, nil
}
