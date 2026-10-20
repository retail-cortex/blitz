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
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/engine/connectors"
)

// answer is a fake tool's reply: its text parts, an error or not.
type answer struct {
	parts []string
	isErr bool
}

// session is a client on an in-memory MCP server whose tools answer as
// tools says (by name, then by the dataset asked about).
func session(t *testing.T, tools map[string]func(args map[string]any) answer) *mcp.ClientSession {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "fake", Version: "1"}, nil)
	for name, f := range tools {
		mcp.AddTool(server, &mcp.Tool{Name: name}, func(_ context.Context, _ *mcp.CallToolRequest, in map[string]any) (*mcp.CallToolResult, any, error) {
			a := f(in)
			res := &mcp.CallToolResult{IsError: a.isErr}
			for _, p := range a.parts {
				res.Content = append(res.Content, &mcp.TextContent{Text: p})
			}
			return res, nil, nil
		})
	}
	ct, st := mcp.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := server.Connect(ctx, st, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// BigQuery's reader lists the datasets, their tables (a list or a name a
// part) and each table's metadata, as Toolbox's tools answer: a dataset it
// can't list is skipped, a table it can't describe has no columns, and it
// stops at MaxSchemaTables. Nothing listed, with problems, is an error.
func TestBigQuerySchema(t *testing.T) {
	ctx := context.Background()
	tools := map[string]func(map[string]any) answer{
		schemaTool + "_datasets": func(in map[string]any) answer {
			assert.Equal(t, "acme", in["project"])
			return answer{parts: []string{`["sales", "ops"]`, `{"not": "a name"}`}}
		},
		schemaTool + "_tables": func(in map[string]any) answer {
			if in["dataset"] == "ops" {
				return answer{parts: []string{"access denied"}, isErr: true}
			}
			return answer{parts: []string{`"orders"`, `"customers"`}}
		},
		schemaTool + "_table": func(in map[string]any) answer {
			if in["table"] == "customers" {
				return answer{parts: []string{"not found"}, isErr: true}
			}
			return answer{parts: []string{`{"Description": "Every order", "Schema": [{"Name": "day", "Type": "DATE"}]}`}}
		},
	}
	cs := session(t, tools)
	tables, err := bigquerySchema(ctx, cs, "acme")
	require.NoError(t, err)
	assert.Equal(t, []connectors.Table{
		{Name: "sales.orders", About: "Every order", Columns: []connectors.Column{{Name: "day", Type: "DATE"}}},
		{Name: "sales.customers"},
	}, tables)

	old := MaxSchemaTables
	MaxSchemaTables = 1
	t.Cleanup(func() { MaxSchemaTables = old })
	tables, err = bigquerySchema(ctx, cs, "acme")
	require.NoError(t, err)
	assert.Len(t, tables, 1, "at most MaxSchemaTables")
	MaxSchemaTables = old

	tools[schemaTool+"_tables"] = func(map[string]any) answer { return answer{parts: []string{"access denied"}, isErr: true} }
	_, err = bigquerySchema(ctx, session(t, tools), "acme")
	assert.ErrorContains(t, err, "sales: access denied; ops: access denied", "nothing listed: the problems")

	tools[schemaTool+"_datasets"] = func(map[string]any) answer { return answer{parts: []string{"no project"}, isErr: true} }
	_, err = bigquerySchema(ctx, session(t, tools), "acme")
	assert.ErrorContains(t, err, "no project")
	_, err = bigquerySchema(ctx, session(t, map[string]func(map[string]any) answer{}), "acme")
	assert.Error(t, err, "no such tool")
}

// The statement reader takes a row per table, skipping rows that aren't
// one; the tool's error is the read's.
func TestStatementSchema(t *testing.T) {
	ctx := context.Background()
	cs := session(t, map[string]func(map[string]any) answer{schemaTool: func(map[string]any) answer {
		return answer{parts: []string{`{"table": "public.orders", "about": "Orders", "columns": "id integer, total numeric"}`, `not json`, `{"about": "no name"}`}}
	}})
	tables, err := statementSchema(ctx, cs)
	require.NoError(t, err)
	assert.Equal(t, []connectors.Table{{Name: "public.orders", About: "Orders", Columns: []connectors.Column{{Name: "id", Type: "integer"}, {Name: "total", Type: "numeric"}}}}, tables)

	cs = session(t, map[string]func(map[string]any) answer{schemaTool: func(map[string]any) answer { return answer{parts: []string{"permission denied"}, isErr: true} }})
	_, err = statementSchema(ctx, cs)
	assert.ErrorContains(t, err, "permission denied")
	_, err = statementSchema(ctx, session(t, map[string]func(map[string]any) answer{}))
	assert.Error(t, err)

	_, err = Backend{}.ReadSchema(ctx, connectors.Env{}, &connectors.Kind{Schema: &connectors.SchemaSpec{Reader: "unknown"}}, connectors.Connection{})
	assert.ErrorContains(t, err, `no schema reader "unknown"`)
}
