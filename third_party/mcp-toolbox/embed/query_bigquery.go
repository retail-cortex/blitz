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

package embed

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	bigqueryapi "cloud.google.com/go/bigquery"
	bigqueryrestapi "google.golang.org/api/bigquery/v2"
	"google.golang.org/api/iterator"

	bigqueryds "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/sources/bigquery"
	bqutil "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/bigquery/bigquerycommon"
)

// queryBigQuery runs statement on s as Toolbox's execute_sql does (its
// dry run first, its dataset allow-list and write mode held), tells
// opts.Billed the bytes it will read before it runs, and reads its result
// with the schema's types, descriptions and modes.
func queryBigQuery(ctx context.Context, s *bigqueryds.Source, statement string, opts QueryOptions) (Result, error) {
	if s.UseClientAuthorization() {
		return Result{}, errors.New("this BigQuery connection uses each caller's own sign-in: query it through its tools")
	}
	client, rest := s.BigQueryClient(), s.BigQueryRestService()
	var connProps []*bigqueryapi.ConnectionProperty
	var session *bigqueryds.Session
	if s.BigQueryWriteMode() == bigqueryds.WriteModeProtected {
		var err error
		if session, err = s.BigQuerySession()(ctx); err != nil {
			return Result{}, fmt.Errorf("a BigQuery session for protected mode: %w", err)
		}
		connProps = []*bigqueryapi.ConnectionProperty{{Key: "session_id", Value: session.ID}}
	}
	restParams, params := bigQueryParams(opts.Params)
	var job *bigqueryrestapi.Job
	if len(s.BigQueryAllowedDatasets()) > 0 {
		var verr error
		job, verr = bqutil.ValidateQueryAgainstAllowedDatasets(ctx, rest, client.Project(), client.Location, statement, restParams, connProps, s, s.GetMaximumBytesBilled(), false)
		if verr != nil {
			return Result{}, verr
		}
	} else {
		var err error
		if job, err = bqutil.DryRunQuery(ctx, rest, client.Project(), client.Location, statement, restParams, connProps, s.GetMaximumBytesBilled(), false); err != nil {
			return Result{}, err
		}
	}
	if job.Statistics == nil || job.Statistics.Query == nil {
		return Result{}, errors.New("BigQuery's dry run gave no statistics")
	}
	if err := bigQueryWriteMode(s.BigQueryWriteMode(), job, session); err != nil {
		return Result{}, err
	}
	if opts.Billed != nil {
		if err := opts.Billed(job.Statistics.TotalBytesProcessed); err != nil {
			return Result{}, err
		}
	}
	q := client.Query(statement)
	q.Location = client.Location
	q.Parameters = params
	q.ConnectionProperties = connProps
	q.Labels = map[string]string{"blitz": "data"}
	if n := s.GetMaximumBytesBilled(); n > 0 {
		q.MaxBytesBilled = n
	}
	run, err := q.Run(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("running the query: %w", err)
	}
	it, err := run.Read(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("reading the query's results: %w", err)
	}
	var out Result
	b := budget{maxRows: opts.MaxRows, maxB: opts.MaxBytes}
	for {
		var row []bigqueryapi.Value
		err := it.Next(&row)
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return Result{}, fmt.Errorf("reading the query's results: %w", err)
		}
		vals := make([]any, len(row))
		for i, v := range row {
			vals[i] = bigqueryds.NormalizeValue(v)
		}
		if !b.fits(vals) {
			out.Truncated = true
			break
		}
		out.Rows = append(out.Rows, vals)
	}
	out.Columns = bigQueryColumns(it.Schema)
	return out, nil
}

// bigQueryWriteMode holds a source's write mode against a statement's dry
// run: blocked runs SELECT alone; protected writes only to its session's
// dataset.
func bigQueryWriteMode(mode string, job *bigqueryrestapi.Job, session *bigqueryds.Session) error {
	switch mode {
	case bigqueryds.WriteModeBlocked:
		if job.Statistics.Query.StatementType != "SELECT" {
			return ErrWrites
		}
	case bigqueryds.WriteModeProtected:
		if c := job.Configuration; c != nil && c.Query != nil && c.Query.DestinationTable != nil && session != nil && c.Query.DestinationTable.DatasetId != session.DatasetID {
			return fmt.Errorf("protected write mode writes only to its session's dataset, not %q", c.Query.DestinationTable.DatasetId)
		}
	}
	return nil
}

// bigQueryParams are positional parameters (?) for the dry run (REST)
// and the query (the client).
func bigQueryParams(values []any) ([]*bigqueryrestapi.QueryParameter, []bigqueryapi.QueryParameter) {
	if len(values) == 0 {
		return nil, nil
	}
	rest := make([]*bigqueryrestapi.QueryParameter, len(values))
	params := make([]bigqueryapi.QueryParameter, len(values))
	for i, v := range values {
		typ, text := "STRING", fmt.Sprint(v)
		switch x := v.(type) {
		case int, int32, int64:
			typ = "INT64"
		case float32, float64:
			typ = "FLOAT64"
		case bool:
			typ = "BOOL"
		case time.Time:
			typ, text = "TIMESTAMP", x.Format(time.RFC3339Nano)
		case nil:
			text = ""
		}
		if f, ok := v.(float64); ok {
			text = strconv.FormatFloat(f, 'g', -1, 64)
		}
		rest[i] = &bigqueryrestapi.QueryParameter{ParameterType: &bigqueryrestapi.QueryParameterType{Type: typ}, ParameterValue: &bigqueryrestapi.QueryParameterValue{Value: text}}
		if v == nil {
			rest[i].ParameterValue = &bigqueryrestapi.QueryParameterValue{NullFields: []string{"Value"}}
		}
		params[i] = bigqueryapi.QueryParameter{Value: v}
	}
	return rest, params
}

// bigQueryColumns are a result's columns from its schema: a repeated
// field is an ARRAY, a NULLABLE one may be NULL.
func bigQueryColumns(schema bigqueryapi.Schema) []Column {
	cols := make([]Column, len(schema))
	for i, f := range schema {
		typ := string(f.Type)
		if f.Repeated {
			typ = "ARRAY"
		}
		nullable := !f.Required && !f.Repeated
		cols[i] = Column{Name: f.Name, DBType: typ, Nullable: &nullable, Description: f.Description}
		if f.MaxLength > 0 {
			n := f.MaxLength
			cols[i].Length = &n
		}
	}
	return cols
}
