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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/connectors"
	"github.com/retail-cortex/blitz/pkg/engine/datatable"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/enterprise"
	"github.com/retail-cortex/blitz/pkg/secrets"
)

// The organization's shared connectors (spec_053 EN-70 to EN-74): the
// service holds the connections and runs the queries, under the person's
// policy; Blitz offers them to the agents, the Data menu and @db:
// mentions beside the person's own connections.

// orgServer is the in-process MCP server that offers the organization's
// shared connectors as tools, prefixed orgPrefix: org__<name>_execute_sql
// and org__<name>_list_tables.
const (
	orgServer = "organization"
	orgPrefix = "org"
)

func init() { tools.RegisterInProcess(orgServer, orgTransport) }

// sharedAllowed are the shared connectors the person's policy lets them
// use, by name, and whether it lets them write to each (EN-70): what the
// policy's explanation says, so the tools are known before the service
// is asked.
func sharedAllowed(o config.OrgLayer) (names []string, write map[string]bool) {
	write = map[string]bool{}
	for path, d := range o.Explain {
		rest, ok := strings.CutPrefix(path, "connectors.shared.")
		if !ok {
			continue
		}
		name, field, _ := strings.Cut(rest, ".")
		on, _ := d.Value.(bool)
		switch field {
		case "use":
			if on {
				names = append(names, name)
			}
		case "write":
			write[name] = on
		}
	}
	slices.Sort(names)
	return names, write
}

// withOrgConnectors is servers with the organization's shared connectors'
// server, when the person's policy lets them use any (EN-71): every
// agent's; a connector's tools run without asking when the policy
// doesn't let them write, as a read-only database's do; restarted when
// what the policy allows changes.
func withOrgConnectors(cfg *config.Config, servers []config.MCPServerConfig) []config.MCPServerConfig {
	if cfg.Enterprise.URL == "" {
		return servers
	}
	names, write := sharedAllowed(cfg.Org)
	if len(names) == 0 {
		return servers
	}
	c := config.MCPServerConfig{Name: orgServer, Prefix: orgPrefix, InProcess: orgServer, URL: cfg.Enterprise.URL, Args: names, Agents: []string{"*"}}
	h := sha256.New()
	for _, n := range names {
		fmt.Fprintf(h, "%s:%v;", n, write[n])
		c.ReadOnlyTools = append(c.ReadOnlyTools, n+"_list_tables")
		if !write[n] {
			c.ReadOnlyTools = append(c.ReadOnlyTools, n+"_execute_sql")
		}
	}
	c.Fingerprint = hex.EncodeToString(h.Sum(nil))
	return append(servers, c)
}

// orgClient is the process's connection to the organization's service at
// u: blitzd's, registered with its store, or one reading this user's
// keychain.
func orgClient(u string) (*enterprise.Client, error) {
	n, err := enterprise.Normalize(u)
	if err != nil {
		return nil, fmt.Errorf("[enterprise] url: %w", err)
	}
	return enterprise.Shared(n, func() secrets.Store { return secrets.Default(config.ConfigDir("")) }, enterprise.Options{}), nil
}

// orgTransport connects to the in-process server for c: on connecting,
// it asks the service which shared connectors the person may use and
// offers each one's tools.
func orgTransport(c config.MCPServerConfig) mcp.Transport { return &orgMCP{url: c.URL} }

type orgMCP struct{ url string }

// sharedQueryRows are the rows a tool's query answers at most, under the
// policy's own limit.
const sharedQueryRows = 200

func (o *orgMCP) Connect(ctx context.Context) (mcp.Connection, error) {
	client, err := orgClient(o.url)
	if err != nil {
		return nil, err
	}
	list, err := client.SharedConnectors(ctx)
	if err != nil {
		return nil, fmt.Errorf("your organization's connectors: %w", err)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: orgServer, Version: "1"}, nil)
	for _, sc := range list {
		about := sc.Description
		if about == "" {
			about = "a " + sc.Kind + " database"
		}
		access := "read-only"
		if !sc.ReadOnly {
			access = "it may change data"
		}
		if len(sc.Tables) > 0 {
			access += "; only the tables " + strings.Join(sc.Tables, ", ")
		}
		name := sc.Name
		mcp.AddTool(server, &mcp.Tool{Name: name + "_execute_sql",
			Description: fmt.Sprintf("Runs one SQL statement on your organization's %s (%s), through its service: %s. At most %d rows come back.", name, about, access, min(sc.MaxRows, sharedQueryRows))},
			func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
				SQL string `json:"sql" jsonschema:"the SQL statement to run"`
			}) (*mcp.CallToolResult, any, error) {
				raw, err := client.SharedQuery(ctx, name, in.SQL, sharedQueryRows)
				if err != nil {
					return toolError(err), nil, nil
				}
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}}, nil, nil
			})
		mcp.AddTool(server, &mcp.Tool{Name: name + "_list_tables", Description: fmt.Sprintf("Lists the tables of your organization's %s (%s) you may use, with their columns.", name, about)},
			func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
				tables, err := client.SharedSchema(ctx, name)
				if err != nil {
					return toolError(err), nil, nil
				}
				b, _ := json.Marshal(tables)
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil, nil
			})
	}
	ct, st := mcp.NewInMemoryTransports()
	if _, err := server.Connect(context.WithoutCancel(ctx), st, nil); err != nil {
		return nil, err
	}
	return ct.Connect(ctx)
}

// toolError is err as a tool's answer the agent reads: a refusal of the
// policy says who decided it.
func toolError(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}
}

// orgShared are the shared connectors the person may use, asked of the
// service at most once every sharedTTL per workspace; none when it can't
// be asked.
func (w *Workspace) orgShared(ctx context.Context) []enterprise.SharedConnector {
	cfg := w.settings()
	if cfg.Enterprise.URL == "" {
		return nil
	}
	if names, _ := sharedAllowed(cfg.Org); len(names) == 0 {
		return nil
	}
	w.shared.Lock()
	defer w.shared.Unlock()
	if time.Since(w.shared.at) < sharedTTL {
		return w.shared.list
	}
	client, err := orgClient(cfg.Enterprise.URL)
	if err != nil {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	list, err := client.SharedConnectors(cctx)
	if err != nil {
		slog.Warn("the organization's connectors", "error", err)
		return w.shared.list // the last known
	}
	w.shared.list, w.shared.at = list, time.Now()
	return list
}

// sharedTTL is how long the list of shared connectors is kept.
const sharedTTL = 30 * time.Second

// sharedCache keeps a workspace's list of shared connectors.
type sharedCache struct {
	sync.Mutex
	list []enterprise.SharedConnector
	at   time.Time
}

// orgDataConnections are the shared connectors as data connections, but
// those named as one of the person's own (which is the one a name means).
func (w *Workspace) orgDataConnections(ctx context.Context, own []api.DataConnection) []api.DataConnection {
	var out []api.DataConnection
	for _, sc := range w.orgShared(ctx) {
		if slices.ContainsFunc(own, func(c api.DataConnection) bool { return c.Name == sc.Name }) {
			continue
		}
		out = append(out, api.DataConnection{Name: sc.Name, Kind: sc.Kind, ReadOnly: sc.ReadOnly, On: true, Trusted: true, Org: true})
	}
	return out
}

// orgDataQuery runs q on the shared connector it names, when it names
// one: false when it doesn't.
func (w *Workspace) orgDataQuery(ctx context.Context, q api.DataQuery) (api.DataTable, bool, error) {
	i := slices.IndexFunc(w.orgShared(ctx), func(sc enterprise.SharedConnector) bool { return sc.Name == q.Connection })
	if i < 0 {
		return api.DataTable{}, false, nil
	}
	if q.Statement == "" {
		return api.DataTable{}, true, fmt.Errorf("your organization's %s has no saved queries: give a statement (SQL)", q.Connection)
	}
	if len(q.Params) > 0 || len(q.Named) > 0 {
		return api.DataTable{}, true, errors.New("your organization's connectors take a statement without parameters")
	}
	t, err := w.RunOrgQuery(ctx, q.Connection, q.Statement, q.MaxRows)
	if err != nil {
		return api.DataTable{}, true, err
	}
	dt, err := DataTable(t)
	return dt, true, err
}

// orgSchemaText is a shared connector as a turn reads it (EN-73): what it
// holds, its tables and columns the person may use, and its tools; ""
// when name isn't one.
func (w *Workspace) orgSchemaText(ctx context.Context, name string) string {
	i := slices.IndexFunc(w.orgShared(ctx), func(sc enterprise.SharedConnector) bool { return sc.Name == name })
	if i < 0 {
		return ""
	}
	sc := w.orgShared(ctx)[i]
	var b strings.Builder
	fmt.Fprintf(&b, "Your organization's database connection %s (%s", sc.Name, sc.Kind)
	if sc.ReadOnly {
		b.WriteString(", read-only")
	}
	b.WriteString(")")
	if sc.Description != "" {
		b.WriteString(": " + sc.Description)
	}
	b.WriteString("\n")
	client, err := orgClient(w.settings().Enterprise.URL)
	if err == nil {
		var tables []enterprise.SharedTable
		if tables, err = client.SharedSchema(ctx, name); err == nil {
			b.WriteString("Tables:\n")
			shown := 0
			for _, t := range tables {
				if shown >= addedSchemaColumns {
					break
				}
				fmt.Fprintf(&b, "- %s\n", t.Name)
				for _, col := range t.Columns {
					if shown >= addedSchemaColumns {
						break
					}
					fmt.Fprintf(&b, "  - %s %s\n", col.Name, col.Type)
					shown++
				}
			}
		}
	}
	if err != nil {
		fmt.Fprintf(&b, "Its schema couldn't be read: %s\n", err)
	}
	fmt.Fprintf(&b, "Query it as you need to with its tools: %s__%s_execute_sql, %s__%s_list_tables.\n", orgPrefix, name, orgPrefix, name)
	return b.String()
}

// OrgConnections are the shared connectors the person may use as Studio
// and the Data menu list connections (EN-72): the organization's, not
// opened or switched off here, their tools the organization's server's.
func (w *Workspace) OrgConnections(ctx context.Context) []connectors.Connection {
	var out []connectors.Connection
	for _, sc := range w.orgShared(ctx) {
		out = append(out, connectors.Connection{Name: sc.Name, Category: dataCategory, Scope: connectors.ScopeOrgShared, Kind: sc.Kind,
			Description: sc.Description, ReadOnly: sc.ReadOnly, Tools: []string{"execute_sql", "list_tables"}, Trusted: true, On: true})
	}
	return out
}

// RunOrgQuery runs statement on the shared connector name, as the person,
// at most maxRows rows (EN-72).
func (w *Workspace) RunOrgQuery(ctx context.Context, name, statement string, maxRows int) (datatable.Table, error) {
	u := w.settings().Enterprise.URL
	if u == "" {
		return datatable.Table{}, errors.New("not connected to an organization")
	}
	client, err := orgClient(u)
	if err != nil {
		return datatable.Table{}, err
	}
	raw, err := client.SharedQuery(ctx, name, statement, maxRows)
	if err != nil {
		return datatable.Table{}, err
	}
	var t datatable.Table
	if err := json.Unmarshal(raw, &t); err != nil {
		return datatable.Table{}, fmt.Errorf("your organization's answer can't be read: %w", err)
	}
	return t, nil
}

// orgTables are the tables of the shared connector name the person may
// use, as Blitz's connections' schemas are read.
func (w *Workspace) orgTables(ctx context.Context, name string) ([]connectors.Table, error) {
	client, err := orgClient(w.settings().Enterprise.URL)
	if err != nil {
		return nil, err
	}
	shared, err := client.SharedSchema(ctx, name)
	if err != nil {
		return nil, err
	}
	out := make([]connectors.Table, 0, len(shared))
	for _, t := range shared {
		ct := connectors.Table{Name: t.Name, About: t.About}
		for _, c := range t.Columns {
			ct.Columns = append(ct.Columns, connectors.Column{Name: c.Name, Type: c.Type, About: c.About})
		}
		out = append(out, ct)
	}
	return out, nil
}
