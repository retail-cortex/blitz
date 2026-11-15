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
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/connectors"
	"github.com/retail-cortex/blitz/pkg/enterprise"
	"github.com/retail-cortex/blitz/pkg/enterprise/enterprisetest"
	"github.com/retail-cortex/blitz/pkg/secrets"
)

// signedInOrg is a fake organization service with the person signed in,
// its client the process's, offering the shared connector shop.
func signedInOrg(t *testing.T) *enterprisetest.Service {
	t.Helper()
	idp := enterprisetest.NewProvider(t)
	org := enterprisetest.NewService(t, idp.URL)
	c := enterprise.New(org.URL, &secrets.Memory{}, enterprise.Options{})
	b := enterprisetest.Browse(t)
	require.NoError(t, c.SignInBrowser(context.Background(), func(p enterprise.Prompt) { b(p.URL) }, nil))
	enterprise.Register(c)
	org.Shared = []map[string]any{{"name": "shop", "kind": "postgres", "description": "Orders", "read_only": true, "tables": []string{"public.orders*"}, "max_rows": 100}}
	org.SharedTables = map[string][]map[string]any{"shop": {{"name": "public.orders", "columns": []map[string]any{{"name": "id", "type": "integer"}}}}}
	org.SharedResults = map[string]map[string]any{"select id from orders": {"columns": []map[string]any{{"name": "id"}}, "rows": [][]any{{1}, {2}}, "total": 2}}
	return org
}

// orgLayer is a policy that lets the person use shop, and write to crm.
func orgLayer() config.OrgLayer {
	return config.OrgLayer{Explain: map[string]config.PolicyDecision{
		"connectors.shared.shop.use":   {Value: true, Level: "group", By: []string{"Data"}},
		"connectors.shared.shop.write": {Value: false, Level: "default"},
		"connectors.shared.crm.use":    {Value: true, Level: "person"},
		"connectors.shared.crm.write":  {Value: true, Level: "person"},
		"connectors.shared.hr.use":     {Value: false, Level: "default"},
		"connectors.own":               {Value: true, Level: "global"},
	}}
}

// EN-70, EN-71: the policy says which shared connectors the person may
// use; the organization's server is offered only then, every agent's,
// what can't write running without asking.
func TestOrgConnectorsServer(t *testing.T) {
	names, write := sharedAllowed(orgLayer())
	assert.Equal(t, []string{"crm", "shop"}, names)
	assert.True(t, write["crm"])
	assert.False(t, write["shop"])

	cfg := config.DefaultConfig()
	mine := config.MCPServerConfig{Name: "mine"}
	assert.Equal(t, []config.MCPServerConfig{mine}, withOrgConnectors(cfg, []config.MCPServerConfig{mine}), "no organization")
	cfg.Enterprise.URL = "https://blitz.acme.com"
	assert.Equal(t, []config.MCPServerConfig{mine}, withOrgConnectors(cfg, []config.MCPServerConfig{mine}), "none allowed")
	cfg.Org = orgLayer()
	got := withOrgConnectors(cfg, []config.MCPServerConfig{mine})
	require.Len(t, got, 2)
	s := got[1]
	assert.Equal(t, orgServer, s.InProcess)
	assert.Equal(t, orgPrefix, s.Prefix)
	assert.Equal(t, []string{"*"}, s.Agents)
	assert.Equal(t, []string{"crm_list_tables", "shop_list_tables", "shop_execute_sql"}, s.ReadOnlyTools)
	before := s.Fingerprint
	cfg.Org.Explain["connectors.shared.shop.write"] = config.PolicyDecision{Value: true}
	assert.NotEqual(t, before, withOrgConnectors(cfg, nil)[0].Fingerprint, "what's allowed changed: restarted")
}

// EN-71: the organization's server offers each shared connector's tools,
// which run through the service as the person.
func TestOrgConnectorsTools(t *testing.T) {
	org := signedInOrg(t)
	ctx := context.Background()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, orgTransport(config.MCPServerConfig{URL: org.URL}), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })
	tools, err := cs.ListTools(ctx, nil)
	require.NoError(t, err)
	var names []string
	for _, tl := range tools.Tools {
		names = append(names, tl.Name)
	}
	assert.ElementsMatch(t, []string{"shop_execute_sql", "shop_list_tables"}, names)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "shop_execute_sql", Arguments: map[string]any{"sql": "select id from orders"}})
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.JSONEq(t, `{"columns":[{"name":"id"}],"rows":[[1],[2]],"total":2}`, res.Content[0].(*mcp.TextContent).Text)
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "shop_execute_sql", Arguments: map[string]any{"sql": "select nope"}})
	require.NoError(t, err)
	assert.True(t, res.IsError, "the database's answer, the agent's to read")
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "shop_list_tables", Arguments: map[string]any{}})
	require.NoError(t, err)
	assert.Contains(t, res.Content[0].(*mcp.TextContent).Text, "public.orders")

	_, err = mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, orgTransport(config.MCPServerConfig{URL: "not a url"}), nil)
	assert.Error(t, err)
}

// EN-72, EN-73: Studio, the Data menu and @db: mentions list the shared
// connectors beside the person's own (theirs winning a name), and a query
// on one runs through the service; its schema reads from it.
func TestOrgConnectorsData(t *testing.T) {
	secrets.SetDefault(&secrets.Memory{})
	org := signedInOrg(t)
	w := openTest(t)
	ctx := context.Background()
	w.cfg.Enterprise.URL = org.URL
	w.cfg.Org = orgLayer()

	conns, err := w.DataConnections(ctx)
	require.NoError(t, err)
	require.Len(t, conns, 1)
	assert.Equal(t, api.DataConnection{Name: "shop", Kind: "postgres", ReadOnly: true, On: true, Trusted: true, Org: true}, conns[0])
	listed := w.OrgConnections(ctx)
	require.Len(t, listed, 1)
	assert.Equal(t, connectors.ScopeOrgShared, listed[0].Scope)

	got, err := w.RunDataQuery(ctx, api.DataQuery{Connection: "shop", Statement: "select id from orders"})
	require.NoError(t, err)
	assert.Len(t, got.Rows, 2)
	_, err = w.RunDataQuery(ctx, api.DataQuery{Connection: "shop", Query: "saved"})
	assert.ErrorContains(t, err, "no saved queries")
	_, err = w.RunDataQuery(ctx, api.DataQuery{Connection: "shop", Statement: "select ?", Params: []any{1}})
	assert.ErrorContains(t, err, "without parameters")
	_, err = w.RunDataQuery(ctx, api.DataQuery{Connection: "nope", Statement: "select 1"})
	assert.ErrorContains(t, err, "no database connection")

	tbl, err := w.RunOrgQuery(ctx, "shop", "select id from orders", 0)
	require.NoError(t, err)
	assert.Equal(t, 2, tbl.Total)
	tables, err := w.orgTables(ctx, "shop")
	require.NoError(t, err)
	assert.Equal(t, "public.orders", tables[0].Name)

	text := w.schemaText(ctx, w.Connectors(), "shop")
	assert.Contains(t, text, "Your organization's database connection shop (postgres, read-only): Orders")
	assert.Contains(t, text, "  - id integer")
	assert.Contains(t, text, "org__shop_execute_sql")

	// One of the person's own of its name wins it.
	c := kitchen(t)
	c.Name = "shop"
	_, probs, err := w.SaveConnector(ctx, c, nil, "")
	require.NoError(t, err)
	require.Empty(t, probs)
	conns, err = w.DataConnections(ctx)
	require.NoError(t, err)
	require.Len(t, conns, 1)
	assert.False(t, conns[0].Org)
}

// EN-74: the service out of reach: the last list stands, and what asks
// it says why; nothing connected, nothing asked. A connector with no
// description that may write says so to the agent.
func TestOrgConnectorsUnreachable(t *testing.T) {
	secrets.SetDefault(&secrets.Memory{})
	org := signedInOrg(t)
	org.Shared = append(org.Shared, map[string]any{"name": "crm", "kind": "postgres", "read_only": false, "max_rows": 10})
	w := openTest(t)
	ctx := context.Background()

	_, err := w.RunOrgQuery(ctx, "shop", "select 1", 0)
	assert.ErrorContains(t, err, "not connected to an organization")
	assert.Empty(t, w.orgShared(ctx))

	w.cfg.Enterprise.URL = org.URL
	w.cfg.Org = orgLayer()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, orgTransport(config.MCPServerConfig{URL: org.URL}), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })
	tools, err := cs.ListTools(ctx, nil)
	require.NoError(t, err)
	var about string
	for _, tl := range tools.Tools {
		if tl.Name == "crm_execute_sql" {
			about = tl.Description
		}
	}
	assert.Contains(t, about, "(a postgres database")
	assert.Contains(t, about, "it may change data")
	require.Len(t, w.orgShared(ctx), 2, "kept")

	org.Close()
	assert.Len(t, w.orgShared(ctx), 2, "the last list, within its time")
	w.shared.at = time.Time{}
	assert.Len(t, w.orgShared(ctx), 2, "the last list, the service out of reach")
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "shop_list_tables", Arguments: map[string]any{}})
	require.NoError(t, err)
	assert.True(t, res.IsError)
	_, err = w.RunOrgQuery(ctx, "shop", "select 1", 0)
	assert.Error(t, err)
	_, err = w.orgTables(ctx, "shop")
	assert.Error(t, err)
	_, err = w.RunDataQuery(ctx, api.DataQuery{Connection: "shop", Statement: "select 1"})
	assert.Error(t, err)
	assert.Contains(t, w.orgSchemaText(ctx, "shop"), "Its schema couldn't be read")
	_, err = mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, orgTransport(config.MCPServerConfig{URL: org.URL}), nil)
	assert.ErrorContains(t, err, "your organization's connectors")
}

// A service address that can't be one, or a policy that allows none:
// nothing listed, and what asks says why.
func TestOrgConnectorsBadAddress(t *testing.T) {
	w := openTest(t)
	ctx := context.Background()
	w.cfg.Enterprise.URL = "https://blitz.acme.com"
	assert.Empty(t, w.orgShared(ctx), "the policy allows none")
	w.cfg.Enterprise.URL = "not an address"
	w.cfg.Org = orgLayer()
	assert.Empty(t, w.orgShared(ctx))
	_, err := w.RunOrgQuery(ctx, "shop", "select 1", 0)
	assert.ErrorContains(t, err, "[enterprise] url")
	_, err = w.orgTables(ctx, "shop")
	assert.ErrorContains(t, err, "[enterprise] url")
}
