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
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/connectors"
	"github.com/retail-cortex/blitz/pkg/engine/flows"
)

// A Data step (spec_059_data DAT-20..DAT-22, DAT-30): its SQL's
// parameter filled from the input, its table read by the next link's
// condition; a saved query; a write asked for and run where allowed; a
// result cut, or failing the step when it says so.
func TestFlowData(t *testing.T) {
	// The agents' execute_sql on pantry is allowed: so is the step's write.
	w := openCalls(t, func(c *config.Config) {
		c.Permissions.Allow = append(c.Permissions.Allow, "mcp(databases:db__pantry_execute_sql)")
	})
	ctx := context.Background()
	k := kitchen(t)
	_, probs, err := w.SaveConnector(ctx, k, nil, "")
	require.NoError(t, err)
	require.Empty(t, probs)
	pantry := k
	pantry.Name, pantry.ReadOnly, pantry.Queries = "pantry", false, nil
	_, probs, err = w.SaveConnector(ctx, pantry, nil, "")
	require.NoError(t, err)
	require.Empty(t, probs)

	writeFlow(t, w, "data", `nodes:
  - {id: q, type: data, connection: kitchen, sql: "SELECT name, minutes FROM recipes WHERE minutes > ? ORDER BY name", params: ["{{ input }}"]}
  - {id: n, type: data, connection: kitchen, saved_query: count}
  - {id: w, type: data, connection: pantry, sql: "UPDATE recipes SET minutes = minutes + 1 RETURNING name", allow_writes: true}
edges:
  - {from: q, to: n, when: "output.data.total == 1 && output.data.rows[0][0] == 'bread' && output.data.columns[1].type == 'integer'"}
  - {from: n, to: w}
`)
	s := newSession(t, w)
	var mu sync.Mutex
	outs := map[string]string{}
	res, err := w.Run(ctx, s.ID, api.Turn{Text: "/data 40", Command: true}, stepOutputs(&mu, outs))
	require.NoError(t, err)
	assert.Contains(t, outs["q"], "| bread | 90 |")
	assert.Contains(t, outs["n"], "| 2 |", "the condition read the table")
	assert.Contains(t, outs["w"], "done")
	assert.Contains(t, res.Output, "soup", "the write ran: its rows returned")

	// A write on a connection whose execute_sql isn't allowed is asked
	// about: nobody here can answer, so it doesn't run.
	larder := pantry
	larder.Name = "larder"
	_, _, err = w.SaveConnector(ctx, larder, nil, "")
	require.NoError(t, err)
	writeFlow(t, w, "asks", "nodes: [{id: w, type: data, connection: larder, sql: \"DELETE FROM recipes\", allow_writes: true}]\n")
	_, err = w.Run(ctx, s.ID, api.Turn{Text: "/asks", Command: true}, func(api.Event) {})
	assert.ErrorContains(t, err, "db__larder_execute_sql requires approval")

	writeFlow(t, w, "cut", `nodes:
  - {id: q, type: data, connection: kitchen, sql: "SELECT name FROM recipes", max_rows: 1}
  - {id: f, type: data, connection: kitchen, sql: "SELECT name FROM recipes", max_rows: 1, fail_when_cut: true}
edges:
  - {from: q, to: f}
`)
	outs = map[string]string{}
	_, err = w.Run(ctx, s.ID, api.Turn{Text: "/cut", Command: true}, stepOutputs(&mu, outs))
	assert.ErrorContains(t, err, "over the step's limit")
	assert.Contains(t, outs["q"], "cut at 1 rows", "cut and said")

	// What can't run here is the workflow's problem (WF-02).
	writeFlow(t, w, "bad", `nodes:
  - {id: gone, type: data, connection: nope, sql: "SELECT 1"}
  - {id: ro, type: data, connection: kitchen, sql: "DELETE FROM recipes"}
  - {id: unasked, type: data, connection: pantry, sql: "DELETE FROM recipes"}
  - {id: missing, type: data, connection: kitchen, saved_query: nope}
`)
	var bad flows.Flow
	for _, f := range w.Workflows() {
		if f.Name == "bad" {
			bad = f
		}
	}
	for _, want := range []string{`step gone: there's no connection "nope"`, "step ro: the connection kitchen is read-only", "step unasked: the SQL writes: allow writes", `step missing: kitchen has no query "nope"`} {
		assert.Contains(t, bad.Problem, want)
	}
	_, err = w.Run(ctx, s.ID, api.Turn{Text: "/bad", Command: true}, func(api.Event) {})
	assert.ErrorContains(t, err, "can't run")

	require.NoError(t, w.SetConnectorOn(ctx, "databases", connectors.ScopeWorkspace, "kitchen", false))
	assert.Equal(t, "the connection kitchen is off in this workspace", dataProblem(flows.Node{Type: flows.Data, Connection: "kitchen", SQL: "SELECT 1"}, w.Connectors()))
	_, err = w.dataStep(ctx, nil, flows.Node{Type: flows.Data, Connection: "kitchen", SQL: "SELECT 1"})
	assert.ErrorContains(t, err, "off")
}

func TestDataParts(t *testing.T) {
	broken := connectors.Connection{Name: "b", Category: "databases", Problem: "unreadable", On: true}
	assert.Contains(t, dataProblem(flows.Node{Type: flows.Data, Connection: "b"}, []connectors.Connection{broken}), "unreadable")
	assert.Empty(t, dataProblem(flows.Node{Type: flows.Agent}, nil))
	global := connectors.Connection{Name: "s", Category: "databases", Scope: connectors.ScopeGlobal}
	local := connectors.Connection{Name: "s", Category: "databases", Scope: connectors.ScopeWorkspace}
	c, ok := dataConnection([]connectors.Connection{local, global}, "s")
	assert.True(t, ok)
	assert.Equal(t, connectors.ScopeWorkspace, c.Scope, "the workspace's own wins")

	var notes []string
	check := costCheck(0, func(s string) { notes = append(notes, s) })
	require.NoError(t, check(10<<30))
	assert.Equal(t, []string{"the query reads 10.7 GB, about $0.06"}, notes)
	assert.ErrorContains(t, check(1<<40), "over the most this step may cost, $1.00")
	assert.NoError(t, costCheck(10, func(string) {})(1<<40))

	assert.Equal(t, "512 bytes", sizeText(512))
	assert.Equal(t, "1.5 MB", sizeText(1_500_000))
	assert.True(t, strings.HasSuffix(sizeText(1<<62), "PB"))
}
