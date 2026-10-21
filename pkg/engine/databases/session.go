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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/connectors"
	"github.com/retail-cortex/blitz/pkg/secrets"
)

// schemaTool is the tool a schema read adds to the connection: a name no
// connection's tool takes (they're <name>_<id>, and an ID has no __).
const schemaTool = "blitz__schema"

// MaxSchemaTables bounds the tables a schema read describes one by one
// (BigQuery reads each table's metadata).
var MaxSchemaTables = 60

// Test connects to c as written and lists the tools it offers: the error
// says why it can't be reached.
func (Backend) Test(ctx context.Context, env connectors.Env, k *connectors.Kind, c connectors.Connection) ([]connectors.ToolInfo, error) {
	var out []connectors.ToolInfo
	err := with(ctx, env, k, c, nil, func(ctx context.Context, cs *mcp.ClientSession, readOnly []string) error {
		res, err := cs.ListTools(ctx, nil)
		if err != nil {
			return err
		}
		for _, t := range res.Tools {
			out = append(out, connectors.ToolInfo{Name: t.Name, Description: t.Description, ReadOnly: slices.Contains(readOnly, t.Name)})
		}
		slices.SortFunc(out, func(a, b connectors.ToolInfo) int { return strings.Compare(a.Name, b.Name) })
		return nil
	})
	return out, err
}

// Try runs one of c's tools (<connection>_<tool>, or the tool alone) with
// args and returns what it answered, and whether that's an error.
func (Backend) Try(ctx context.Context, env connectors.Env, k *connectors.Kind, c connectors.Connection, tool string, args map[string]any) (string, bool, error) {
	if !strings.HasPrefix(tool, c.Name+"_") {
		tool = c.Name + "_" + tool
	}
	var text string
	var isErr bool
	err := with(ctx, env, k, c, nil, func(ctx context.Context, cs *mcp.ClientSession, _ []string) error {
		parts, e, err := call(ctx, cs, tool, args)
		text, isErr = maskText(env.Values, strings.Join(parts, "\n")), e
		return err
	})
	return text, isErr, err
}

// call runs a tool: its answer's texts, and whether it's the tool's
// error.
func call(ctx context.Context, cs *mcp.ClientSession, tool string, args map[string]any) ([]string, bool, error) {
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return nil, false, err
	}
	var parts []string
	for _, ct := range res.Content {
		if tc, ok := ct.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return parts, res.IsError, nil
}

// ReadSchema reads c's tables: by its kind's statement (a query whose
// rows are table, about, and columns as "name type, …"), or its reader.
func (Backend) ReadSchema(ctx context.Context, env connectors.Env, k *connectors.Kind, c connectors.Connection) ([]connectors.Table, error) {
	if k == nil || k.Schema == nil {
		return nil, errors.New("its kind doesn't say how to read its schema")
	}
	var extra []*yaml.Node
	switch {
	case k.Schema.Statement != "" && k.Queries != nil:
		extra = append(extra, queryDoc(c.Name, k.Queries.Type, schemaTool, connectors.Query{Description: "The tables and their columns.", Statement: k.Schema.Statement}))
	case k.Schema.Reader == "bigquery":
		for _, t := range []struct{ id, typ string }{{"datasets", "bigquery-list-dataset-ids"}, {"tables", "bigquery-list-table-ids"}, {"table", "bigquery-get-table-info"}} {
			extra = append(extra, mapping("kind", "tool", "name", schemaTool+"_"+t.id, "type", t.typ, "source", c.Name, "description", "Schema."))
		}
	default:
		return nil, fmt.Errorf("no schema reader %q", k.Schema.Reader)
	}
	var tables []connectors.Table
	err := with(ctx, env, k, c, extra, func(ctx context.Context, cs *mcp.ClientSession, _ []string) error {
		var err error
		if k.Schema.Reader == "" {
			tables, err = statementSchema(ctx, cs)
		} else {
			tables, err = bigquerySchema(ctx, cs, c.Settings["project"])
		}
		return err
	})
	return tables, err
}

// statementSchema runs the schema query: a row per table.
func statementSchema(ctx context.Context, cs *mcp.ClientSession) ([]connectors.Table, error) {
	parts, isErr, err := call(ctx, cs, schemaTool, map[string]any{})
	if err != nil {
		return nil, err
	}
	if isErr {
		return nil, errors.New(strings.Join(parts, " "))
	}
	var tables []connectors.Table
	for _, p := range parts {
		var row map[string]any
		if json.Unmarshal([]byte(p), &row) != nil {
			continue
		}
		name, _ := row["table"].(string)
		if name == "" {
			continue
		}
		t := connectors.Table{Name: name}
		t.About, _ = row["about"].(string)
		cols, _ := row["columns"].(string)
		for _, col := range strings.Split(cols, ", ") {
			if n, typ, _ := strings.Cut(strings.TrimSpace(col), " "); n != "" {
				t.Columns = append(t.Columns, connectors.Column{Name: n, Type: typ})
			}
		}
		tables = append(tables, t)
	}
	return tables, nil
}

// bigquerySchema lists the datasets (the allowed ones), their tables, and
// each table's columns from its metadata (no query is billed), up to
// MaxSchemaTables.
func bigquerySchema(ctx context.Context, cs *mcp.ClientSession, project string) ([]connectors.Table, error) {
	strs := func(tool string, args map[string]any) ([]string, error) {
		parts, isErr, err := call(ctx, cs, tool, args)
		if err != nil {
			return nil, err
		}
		if isErr {
			return nil, errors.New(strings.Join(parts, " "))
		}
		var out []string
		for _, p := range parts {
			var list []string
			var one string
			switch {
			case json.Unmarshal([]byte(p), &list) == nil:
				out = append(out, list...)
			case json.Unmarshal([]byte(p), &one) == nil:
				out = append(out, one)
			}
		}
		return out, nil
	}
	datasets, err := strs(schemaTool+"_datasets", map[string]any{"project": project})
	if err != nil {
		return nil, err
	}
	var tables []connectors.Table
	var problems []string
	for _, ds := range datasets {
		ids, err := strs(schemaTool+"_tables", map[string]any{"project": project, "dataset": ds})
		if err != nil {
			problems = append(problems, ds+": "+err.Error())
			continue
		}
		for _, id := range ids {
			if len(tables) >= MaxSchemaTables {
				return tables, nil
			}
			parts, isErr, err := call(ctx, cs, schemaTool+"_table", map[string]any{"project": project, "dataset": ds, "table": id})
			t := connectors.Table{Name: ds + "." + id}
			if err == nil && !isErr && len(parts) > 0 {
				t.About, t.Columns = tableInfo(parts[0])
			}
			tables = append(tables, t)
		}
	}
	if len(tables) == 0 && len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "; "))
	}
	return tables, nil
}

// tableInfo reads BigQuery's table metadata (JSON, its Go field names):
// its description and its columns.
func tableInfo(text string) (string, []connectors.Column) {
	var md struct {
		Description string
		Schema      []struct {
			Name        string
			Type        string
			Description string
		}
	}
	if json.Unmarshal([]byte(text), &md) != nil {
		return "", nil
	}
	var cols []connectors.Column
	for _, f := range md.Schema {
		cols = append(cols, connectors.Column{Name: f.Name, Type: f.Type, About: f.Description})
	}
	return md.Description, cols
}

// with starts c alone, as written (with extra tools), and calls f with a
// session on it and its read-only tools. The secret values typed into
// the form (env.Values) are masked in what it returns: unsaved, they
// aren't Blitz's to remember (redact.Remember) for the process's life.
func with(ctx context.Context, env connectors.Env, k *connectors.Kind, c connectors.Connection, extra []*yaml.Node, f func(context.Context, *mcp.ClientSession, []string) error) error {
	return maskTyped(env.Values, startWith(ctx, env, k, c, extra, f))
}

// maskTyped is err with the values typed into the form masked (nil for
// nil).
func maskTyped(values map[string]string, err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	masked := maskText(values, msg)
	if masked == msg {
		return err
	}
	return errors.New(masked)
}

// maskText is s with the values typed into the form masked.
func maskText(values map[string]string, s string) string {
	for _, v := range values {
		if len(v) >= 4 {
			s = strings.ReplaceAll(s, v, "[REDACTED]")
		}
	}
	return s
}

// startWith is with, unmasked.
func startWith(ctx context.Context, env connectors.Env, k *connectors.Kind, c connectors.Connection, extra []*yaml.Node, f func(context.Context, *mcp.ClientSession, []string) error) error {
	text, err := Backend{}.Render(env.Category, k, c)
	if err != nil {
		return err
	}
	if len(extra) > 0 {
		var b bytes.Buffer
		b.WriteString(strings.TrimRight(text, "\n") + "\n---\n")
		if err := encode(&b, extra); err != nil {
			return err
		}
		text = b.String()
	}
	tmp, err := os.MkdirTemp("", "blitz-connector-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	file := filepath.Join(tmp, c.Name+".yaml")
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		return err
	}
	scope := env.SecretScope(c.Scope)
	vars := Vars([]string{file}, secrets.AnyScope, config.Vault(env.Prefix).Resolve(scope))
	for key, v := range env.Values {
		if v != "" {
			vars[connectors.SecretKey(env.Category, c.Name, key)] = v
		}
	}
	ctx, cancel := context.WithTimeout(ctx, connectors.TestTimeout)
	defer cancel()
	// Written in a temporary folder: its paths start where c belongs.
	base := scopeBase(c.Scope, env.Workspace)
	kinds := connectors.Load(env.Prefix, env.Workspace).Kinds
	if k != nil {
		kinds = append(kinds, *k)
	}
	t := &transport{files: []string{file}, vars: vars, keys: pathKeys(kinds), base: func(string) string { return base }}
	client := mcp.NewClient(&mcp.Implementation{Name: "blitz", Version: "studio"}, nil)
	cs, err := client.Connect(ctx, t, nil)
	if err != nil {
		return err
	}
	defer cs.Close()
	var readOnly []string
	if loaded, err := loadConfig(ctx, []string{file}, vars); err == nil {
		readOnly = readOnlyTools(loaded)
	}
	return f(ctx, cs, readOnly)
}
