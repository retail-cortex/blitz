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

package main

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/retail-cortex/blitz/apps/cli/internal/tui"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/spf13/cobra"
)

// dataOptions are blitz data query's flags.
type dataOptions struct {
	query   string
	params  string
	maxRows int
	json    bool
	csv     bool
}

// Errors of blitz data.
var (
	errDataFormats   = errors.New("choose one of --json and --csv")
	errDataStatement = errors.New("give the SQL, or a saved query with --query, not both")
	errDataNothing   = errors.New("give the SQL to run, or a saved query with --query")
)

// newDataCommand is `blitz data list|query`: the workspace's databases
// from the terminal (spec_059_data DAT-45).
func newDataCommand(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "data",
		Short: "The workspace's databases: list them and their saved queries, or run a query and print its table",
		Long: `The workspace's database connections (Studio › Connectors) from the terminal.
  blitz data list                                  the connections and their saved queries
  blitz data query shop "select * from orders"     run SQL, read-only, and print the table
  blitz data query shop --query top_customers      run a saved query
A query runs read-only, on a connection that's on in the workspace and
trusted, as a chat's @db: does. --json prints the table as JSON (its
columns described, its rows), --csv as CSV, for scripts.`,
	}
	cmd.AddCommand(newDataListCommand(g), newDataQueryCommand(g))
	return cmd
}

func newDataListCommand(g *globalFlags) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the workspace's database connections and their saved queries",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			b, err := openDataBackend(cmd, g)
			if err != nil {
				return err
			}
			defer b.Close()
			conns, err := b.DataConnections(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				return writeJSON(out, dataConnectionsJSON(conns))
			}
			var sb strings.Builder
			tui.PrintDataConnections(&sb, conns)
			return writePlain(out, sb.String())
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the connections as JSON")
	return cmd
}

func newDataQueryCommand(g *globalFlags) *cobra.Command {
	var o dataOptions
	cmd := &cobra.Command{
		Use:   `query <connection> ["<sql>"]`,
		Short: "Run a query on a connection, read-only, and print its table",
		Long: `Runs SQL, or one of the connection's saved queries (--query), read-only, and
prints the table: its columns, its rows, and how many. --params gives the
parameters as JSON: an array in order for SQL ($1, ?, @name as the database
takes them), an object by name for a saved query. --max-rows is the most rows
(1,000 by default); the table says when it was cut.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			q, err := dataQueryOf(args, o)
			if err != nil {
				return withCode(exitUsage, err)
			}
			b, err := openDataBackend(cmd, g)
			if err != nil {
				return err
			}
			defer b.Close()
			t, err := b.RunDataQuery(cmd.Context(), q)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			switch {
			case o.json:
				return writeJSON(out, t)
			case o.csv:
				return writeCSV(out, t)
			}
			return writePlain(out, dataText(t))
		},
	}
	f := cmd.Flags()
	f.StringVarP(&o.query, "query", "q", "", "Run this saved query of the connection instead of SQL")
	f.StringVar(&o.params, "params", "", `The parameters as JSON: ["a", 2] for SQL, {"name": "a"} for a saved query`)
	f.IntVar(&o.maxRows, "max-rows", 0, "The most rows (default 1,000)")
	f.BoolVar(&o.json, "json", false, "Print the table as JSON: its columns described, its rows")
	f.BoolVar(&o.csv, "csv", false, "Print the table as CSV, a header row first")
	return cmd
}

// dataQueryOf is the query blitz data query's arguments and flags ask
// for.
func dataQueryOf(args []string, o dataOptions) (api.DataQuery, error) {
	q := api.DataQuery{Connection: args[0], Query: o.query, MaxRows: o.maxRows}
	if len(args) > 1 {
		q.Statement = args[1]
	}
	switch {
	case o.json && o.csv:
		return q, errDataFormats
	case q.Query != "" && q.Statement != "":
		return q, errDataStatement
	case q.Query == "" && strings.TrimSpace(q.Statement) == "":
		return q, errDataNothing
	case o.maxRows < 0:
		return q, fmt.Errorf("--max-rows is a number of rows, not %d", o.maxRows)
	}
	if strings.TrimSpace(o.params) == "" {
		return q, nil
	}
	d := json.NewDecoder(strings.NewReader(o.params))
	d.UseNumber() // whole numbers stay whole
	var err error
	if q.Query != "" {
		err = d.Decode(&q.Named)
	} else {
		err = d.Decode(&q.Params)
	}
	if err != nil {
		want := "an array, in order"
		if q.Query != "" {
			want = "an object, by name"
		}
		return q, fmt.Errorf("--params isn't JSON %s: %w", want, err)
	}
	return q, nil
}

// openDataBackend is the workspace's backend for blitz data: the
// service's, or one of this run's own.
func openDataBackend(cmd *cobra.Command, g *globalFlags) (api.Backend, error) {
	cfg, err := loadConfig(g)
	if err != nil {
		return nil, err
	}
	warn := func(s string) { fmt.Fprintln(cmd.ErrOrStderr(), s) }
	b, _, _, err := openBackend(cmd.Context(), cfg, backendOptions{trustProject: g.trustProject, flags: g}, warn)
	return b, err
}

// dataConnectionJSON is a connection as blitz data list --json prints it.
type dataConnectionJSON struct {
	Name     string          `json:"name"`
	Kind     string          `json:"kind"`
	ReadOnly bool            `json:"read_only"`
	On       bool            `json:"on"`
	Trusted  bool            `json:"trusted"`
	Problem  string          `json:"problem,omitempty"`
	Queries  []dataQueryJSON `json:"queries"`
}

type dataQueryJSON struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Params      []string `json:"params"`
}

func dataConnectionsJSON(conns []api.DataConnection) []dataConnectionJSON {
	out := make([]dataConnectionJSON, len(conns))
	for i, c := range conns {
		out[i] = dataConnectionJSON{Name: c.Name, Kind: c.Kind, ReadOnly: c.ReadOnly, On: c.On, Trusted: c.Trusted, Problem: c.Problem, Queries: []dataQueryJSON{}}
		for _, q := range c.Queries {
			out[i].Queries = append(out[i].Queries, dataQueryJSON{Name: q.Name, Description: q.Description, Params: append([]string{}, q.Params...)})
		}
	}
	return out
}

func writeJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// writePlain writes text, its colors left out when the output isn't a
// terminal.
func writePlain(out io.Writer, text string) error {
	if !stdoutIsTerminal() {
		text = ansiRE.ReplaceAllString(text, "")
	}
	_, err := io.WriteString(out, text)
	return err
}

// writeCSV writes t as CSV: its columns' names, then its rows; NULL is
// empty, JSON values as JSON.
func writeCSV(out io.Writer, t api.DataTable) error {
	w := csv.NewWriter(out)
	header := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		header[i] = c.Name
	}
	if err := w.Write(header); err != nil {
		return err
	}
	for _, row := range t.Rows {
		rec := make([]string, len(row))
		for i, v := range row {
			rec[i] = cellText(v)
		}
		if err := w.Write(rec); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

// cellText is a value as a cell shows it: NULL empty, text as it is,
// numbers as written, the rest as JSON.
func cellText(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case json.Number:
		return v.String()
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// cellWidth is the widest a column of dataText gets; longer values are
// cut with an ellipsis.
const cellWidth = 40

// dataText is t as aligned columns under their names and types, then
// how many rows and whether a limit cut them.
func dataText(t api.DataTable) string {
	cells := make([][]string, 0, len(t.Rows)+2)
	names := make([]string, len(t.Columns))
	types := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		names[i], types[i] = c.Name, c.Type
	}
	cells = append(cells, names, types)
	for _, row := range t.Rows {
		line := make([]string, len(t.Columns))
		for i := range line {
			if i < len(row) {
				line[i] = oneLine(cellText(row[i]))
			}
		}
		cells = append(cells, line)
	}
	widths := make([]int, len(t.Columns))
	for _, line := range cells {
		for i, s := range line {
			widths[i] = max(widths[i], min(utf8.RuneCountInString(s), cellWidth))
		}
	}
	var b strings.Builder
	for n, line := range cells {
		var l strings.Builder
		for i, s := range line {
			if i > 0 {
				l.WriteString("  ")
			}
			s = cut(s, widths[i])
			style, reset := "", ""
			switch n {
			case 0:
				style, reset = tui.Bold, tui.Reset
			case 1:
				style, reset = tui.Dim, tui.Reset
			}
			pad := ""
			if i < len(line)-1 {
				pad = strings.Repeat(" ", widths[i]-utf8.RuneCountInString(s))
			}
			l.WriteString(style + s + reset + pad)
		}
		b.WriteString(strings.TrimRight(l.String(), " ") + "\n") // an empty last cell leaves no spaces
	}
	if t.Truncated {
		b.WriteString(i18n.T("data.rows_cut", "count", t.Total) + "\n")
	} else {
		b.WriteString(i18n.T("data.rows", "count", t.Total) + "\n")
	}
	return b.String()
}

// cut is s at most n runes, an ellipsis ending one that was longer.
func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
