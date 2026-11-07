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
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/connectors"
	"github.com/retail-cortex/blitz/pkg/engine/databases"
	"github.com/retail-cortex/blitz/pkg/engine/datatable"
	"github.com/retail-cortex/blitz/pkg/engine/flows"
)

// Workflows' Data steps (spec_059_data DAT-20..DAT-25, DAT-30): a query
// on one of the workspace's connections, handed on as a table.

// dataCategory is the connectors' category a Data step queries.
const dataCategory = "databases"

// bigQueryRate is BigQuery's on-demand price, dollars a TiB (DAT-24).
var bigQueryRate = 6.25

// defaultMaxCost is the most a step's billed query may cost unless it
// says (DAT-24, the owner's $1).
const defaultMaxCost = 1.0

// dataConnection is n's connection among conns: the workspace's own when
// a global one has its name too.
func dataConnection(conns []connectors.Connection, name string) (connectors.Connection, bool) {
	var found connectors.Connection
	ok := false
	for _, c := range conns {
		if c.Category == dataCategory && c.Name == name && (!ok || c.Scope == connectors.ScopeWorkspace) {
			found, ok = c, true
		}
	}
	return found, ok
}

// dataStatement is the statement n runs on c: its SQL, or its saved
// query's.
func dataStatement(n flows.Node, c connectors.Connection) (string, error) {
	if n.SavedQuery == "" {
		return n.SQL, nil
	}
	i := slices.IndexFunc(c.Queries, func(q connectors.Query) bool { return q.Name == n.SavedQuery })
	if i < 0 {
		return "", fmt.Errorf("%s has no query %q", c.Name, n.SavedQuery)
	}
	return c.Queries[i].Statement, nil
}

// dataProblem is why a Data step can't run on the workspace's
// connections ("" when it can): its connection gone, off or broken; its
// saved query gone; its statement writing on a read-only connection, or
// without Allow writes (DAT-20, DAT-21).
func dataProblem(n flows.Node, conns []connectors.Connection) string {
	if n.Type != flows.Data || n.Connection == "" {
		return ""
	}
	c, ok := dataConnection(conns, n.Connection)
	switch {
	case !ok:
		return fmt.Sprintf("there's no connection %q", n.Connection)
	case c.Problem != "":
		return fmt.Sprintf("the connection %s: %s", c.Name, c.Problem)
	case !c.On:
		return fmt.Sprintf("the connection %s is off in this workspace", c.Name)
	}
	statement, err := dataStatement(n, c)
	if err != nil {
		return err.Error()
	}
	if strings.TrimSpace(statement) == "" || databases.ReadsOnly(statement) {
		return ""
	}
	if c.ReadOnly {
		return fmt.Sprintf("the connection %s is read-only and the SQL writes", c.Name)
	}
	if !n.AllowWrites {
		return "the SQL writes: allow writes in the step"
	}
	return ""
}

// withDataProblems is all with each workflow's Data steps checked
// against the workspace's connections (WF-02).
func (w *Workspace) withDataProblems(all []flows.Flow) []flows.Flow {
	var conns []connectors.Connection
	for i, f := range all {
		var probs []string
		for _, n := range f.Nodes {
			if n.Type != flows.Data {
				continue
			}
			if conns == nil {
				conns = w.Connectors()
			}
			if p := dataProblem(n, conns); p != "" {
				probs = append(probs, fmt.Sprintf("step %s: %s", n.ID, p))
			}
		}
		if len(probs) > 0 {
			all[i].Problem = strings.Join(append(slices.DeleteFunc([]string{f.Problem}, func(s string) bool { return s == "" }), probs...), "; ")
		}
	}
	return all
}

// dataStep runs a filled-in Data step: its connection as the workspace
// has it, its statement asked about first when it writes, its cost
// checked first when it's billed. Its data is the table (DAT-01), its
// text the table as Markdown (DAT-05, DAT-30).
func (w *Workspace) dataStep(ctx context.Context, r *flowRun, n flows.Node) (flows.Result, error) {
	conns := w.Connectors()
	if p := dataProblem(n, conns); p != "" {
		return flows.Result{}, errors.New(p)
	}
	c, _ := dataConnection(conns, n.Connection)
	statement, _ := dataStatement(n, c)
	writes := !databases.ReadsOnly(statement)
	if writes {
		cat, _ := w.ConnectorCatalog().Category(dataCategory)
		if err := w.tools.ApproveDataWrite(ctx, cat.Server, cat.Prefix+"__"+c.Name+"_execute_sql", statement); err != nil {
			return flows.Result{}, err
		}
	}
	note := func(string) {}
	if r != nil {
		note = func(text string) {
			r.emit(api.Event{Notice: &api.Notice{Text: fmt.Sprintf("/%s › %s: %s", r.f.Name, r.title(n.ID), text)}})
		}
	}
	run := connectors.QueryRun{Query: n.SavedQuery, Named: n.Arguments, Statement: n.SQL, Params: n.Params, MaxRows: n.MaxRows, MaxBytes: n.MaxBytes, ReadOnly: !writes,
		Billed: costCheck(n.MaxCost, note)}
	t, err := w.RunConnectorQuery(ctx, c, nil, run)
	if err != nil {
		return flows.Result{}, errors.New(w.secretRedactor().String(err.Error()))
	}
	if t.Truncated && n.FailWhenCut {
		return flows.Result{}, fmt.Errorf("the result is over the step's limit (%d rows read)", t.Total)
	}
	return dataResult(t, w.secretRedactor().String)
}

// costCheck stops a billed query that would cost more than maxCost
// dollars ($1 when 0), priced at BigQuery's rate, and says what one that
// runs costs (DAT-24).
func costCheck(maxCost float64, note func(string)) func(bytes int64) error {
	if maxCost == 0 {
		maxCost = defaultMaxCost
	}
	return func(bytes int64) error {
		cost := float64(bytes) / (1 << 40) * bigQueryRate
		if cost > maxCost {
			return fmt.Errorf("the query would read %s, about $%.2f: over the most this step may cost, $%.2f", sizeText(bytes), cost, maxCost)
		}
		note(fmt.Sprintf("the query reads %s, about $%.2f", sizeText(bytes), cost))
		return nil
	}
}

// dataResult is a table as a step's result: its text the Markdown, its
// data the table as JSON reads it, both masked.
func dataResult(t datatable.Table, mask func(string) string) (flows.Result, error) {
	raw, err := json.Marshal(t)
	if err != nil {
		return flows.Result{}, err
	}
	var data any
	if err := json.Unmarshal([]byte(mask(string(raw))), &data); err != nil {
		return flows.Result{}, err
	}
	return flows.Result{Text: mask(t.Markdown(0)), Data: data}, nil
}

// sizeText is a byte count as people read it: 3.2 GB.
func sizeText(n int64) string {
	units := []string{"bytes", "KB", "MB", "GB", "TB", "PB"}
	f, i := float64(n), 0
	for f >= 1000 && i < len(units)-1 {
		f /= 1000
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d bytes", n)
	}
	return fmt.Sprintf("%.1f %s", f, units[i])
}
