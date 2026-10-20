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


// Package databases gives a workspace's agents its databases
// (spec_databases_046): MCP Toolbox for Databases, run inside Blitz's
// process (third_party/mcp-toolbox/embed), serving the tools the
// database files describe as one MCP server, "databases", its tools
// named db__<tool>.
//
// The files are Toolbox's YAML (sources, tools, toolsets), the global
// ones in <config>/databases and the workspace's in .agents/databases,
// merged. A ${NAME} in them is the Blitz secret NAME, the workspace's
// own, else the global one; never an environment variable.
package databases

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/redact"
	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/retail-cortex/blitz/third_party/mcp-toolbox/embed"
)

const (
	// Server is the MCP server's name, in /mcp and mcp(databases:…) rules.
	Server = "databases"
	// Prefix namespaces its tools: db__count_orders.
	Prefix = "db"
	// Folder is the database files' folder, in the config folder and in a
	// workspace's .agents.
	Folder = "databases"
)

func init() { tools.RegisterInProcess(Server, Transport) }

// GlobalDir is where the global database files are.
func GlobalDir(prefixDir string) string {
	return filepath.Join(config.ConfigDir(prefixDir), Folder)
}

// WorkspaceDir is where a workspace's database files are.
func WorkspaceDir(workspace string) string {
	return filepath.Join(workspace, ".agents", Folder)
}

// Files are the database files for a workspace: the global ones, then
// its own (empty workspace: the global ones only).
func Files(prefixDir, workspace string) []string {
	files := embed.Files(GlobalDir(prefixDir))
	if workspace != "" {
		files = append(files, embed.Files(WorkspaceDir(workspace))...)
	}
	return files
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

// Vars are the values of the files' ${NAME}s that lookup finds (a
// secrets lookup, as secrets.Vault.Resolve gives). The values are
// remembered for every redactor to mask.
func Vars(files []string, lookup func(scope, key string) (string, bool)) map[string]string {
	vars := map[string]string{}
	for _, n := range Names(files) {
		if v, ok := lookup(secrets.AnyScope, n); ok {
			vars[n] = v
			redact.Remember(v)
		}
	}
	return vars
}

// Config is the workspace's databases server, when it has database
// files: in-process, its files in Args and their secrets in Env, offered
// to every agent; its tools on read-only sources run without asking.
func Config(prefixDir, workspace string) (config.MCPServerConfig, bool) {
	files := Files(prefixDir, workspace)
	if len(files) == 0 {
		return config.MCPServerConfig{}, false
	}
	vars := Vars(files, config.Vault(prefixDir).Resolve(config.SecretScope(prefixDir, workspace)))
	c := config.MCPServerConfig{
		Name:        Server,
		Prefix:      Prefix,
		InProcess:   Server,
		Args:        files,
		Env:         vars,
		Agents:      []string{"*"},
		Fingerprint: Fingerprint(files),
	}
	// A file that doesn't load still makes the server: listing its tools
	// fails, saying why, in /mcp and Studio.
	if cfg, err := embed.Load(context.Background(), files, vars); err == nil {
		c.ReadOnlyTools = embed.ReadOnlyTools(cfg)
	}
	return c, true
}

// With is servers with the workspace's databases server in place of any
// in-process one there (none when it has no database files). A server of
// the user's own named databases wins, and warn says so.
func With(servers []config.MCPServerConfig, prefixDir, workspace string, warn func(string)) []config.MCPServerConfig {
	out := slices.DeleteFunc(slices.Clone(servers), func(s config.MCPServerConfig) bool { return s.InProcess == Server })
	c, ok := Config(prefixDir, workspace)
	if !ok {
		return out
	}
	if slices.ContainsFunc(out, func(s config.MCPServerConfig) bool { return s.Name == Server }) {
		if warn != nil {
			warn(fmt.Sprintf("an MCP server is named %q: the database files aren't served", Server))
		}
		return out
	}
	return append(out, c)
}

// Transport connects to a databases server c: each connection loads its
// files (Args) with their secrets (Env) and starts Toolbox, connecting to
// the databases; closing the connection stops it.
func Transport(c config.MCPServerConfig) mcp.Transport {
	return &transport{files: slices.Clone(c.Args), vars: maps.Clone(c.Env)}
}

type transport struct {
	files []string
	vars  map[string]string
}

// Connect starts Toolbox and connects to it.
func (t *transport) Connect(ctx context.Context) (mcp.Connection, error) {
	cfg, err := embed.Load(ctx, t.files, t.vars)
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
