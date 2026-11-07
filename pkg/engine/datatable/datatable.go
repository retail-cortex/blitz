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

// Package datatable is a query's result in one shape every step and chat
// can read (spec_059_data DAT-01..DAT-05): its columns with portable
// types, its rows as arrays in the columns' order, its values as JSON's.
package datatable

import (
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// The portable types (DAT-02): the same across databases.
const (
	String    = "string"
	Integer   = "integer"
	Number    = "number"
	Boolean   = "boolean"
	Date      = "date"
	Time      = "time"
	Timestamp = "timestamp"
	JSON      = "json"
	Bytes     = "bytes"
	Other     = "other"
)

// Column is a result's column: its name, its schema's comment, its
// portable type and declared length (nil when it has none), and the
// database's own type and whether it may be NULL (nil when unsaid).
type Column struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Length      *int64 `json:"length"`
	DBType      string `json:"db_type,omitempty"`
	Nullable    *bool  `json:"nullable,omitempty"`
}

// Table is a query's result (DAT-01): Total is how many rows it holds,
// Truncated whether a limit cut the query's rows there.
type Table struct {
	Columns   []Column `json:"columns"`
	Rows      [][]any  `json:"rows"`
	Total     int      `json:"total"`
	Truncated bool     `json:"truncated"`
}

// RowItem is one row standing alone (DAT-04): its place in the result,
// its values by column name, and its columns.
type RowItem struct {
	Index   int            `json:"index"`
	Row     map[string]any `json:"row"`
	Columns []Column       `json:"columns"`
}

// New is the table of columns (Type left empty is worked out from DBType,
// else from the first value) and rows as a driver read them, each value
// made JSON's (DAT-03).
func New(columns []Column, rows [][]any, truncated bool) Table {
	cols := make([]Column, len(columns))
	copy(cols, columns)
	for i := range cols {
		if cols[i].Type != "" {
			continue
		}
		cols[i].Type = TypeOf(cols[i].DBType)
		if cols[i].Type == Other && cols[i].DBType == "" {
			cols[i].Type = typeOfValues(rows, i)
		}
	}
	out := Table{Columns: cols, Rows: make([][]any, len(rows)), Total: len(rows), Truncated: truncated}
	for r, row := range rows {
		vals := make([]any, len(cols))
		for i := range cols {
			if i < len(row) {
				vals[i] = Value(row[i], cols[i].Type)
			}
		}
		out.Rows[r] = vals
	}
	return out
}

// Items are t's rows as row items (DAT-04).
func (t Table) Items() []RowItem {
	out := make([]RowItem, len(t.Rows))
	for r, row := range t.Rows {
		m := make(map[string]any, len(t.Columns))
		for i, c := range t.Columns {
			if i < len(row) {
				m[c.Name] = row[i]
			}
		}
		out[r] = RowItem{Index: r, Row: m, Columns: t.Columns}
	}
	return out
}

// exact are database type names and their portable types.
var exact = map[string]string{
	"uuid": String, "name": String, "citext": String, "inet": String, "cidr": String, "macaddr": String, "interval": String,
	"bool": Boolean, "boolean": Boolean,
	"date":  Date,
	"money": Number, "real": Number, "float4": Number, "float8": Number, "float64": Number, "bignumeric": Number,
	"bytea": Bytes, "bytes": Bytes,
	"json": JSON, "jsonb": JSON, "record": JSON, "struct": JSON, "array": JSON, "geography": String,
}

// TypeOf is a database type's portable type: VARCHAR(80), int4,
// "timestamp with time zone", SQLite's declared types by their affinity.
func TypeOf(dbType string) string {
	t := strings.ToLower(strings.TrimSpace(dbType))
	if i := strings.IndexByte(t, '('); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}
	if t == "" {
		return Other
	}
	if strings.HasPrefix(t, "_") || strings.HasSuffix(t, "[]") {
		return JSON // PostgreSQL's arrays
	}
	if p, ok := exact[t]; ok {
		return p
	}
	has := func(parts ...string) bool {
		for _, p := range parts {
			if strings.Contains(t, p) {
				return true
			}
		}
		return false
	}
	switch {
	case has("timestamp", "datetime"):
		return Timestamp
	case has("time"):
		return Time
	case has("int", "serial"):
		return Integer
	case has("char", "text", "clob", "string", "enum"):
		return String
	case has("blob", "binary"):
		return Bytes
	case has("real", "floa", "doub", "dec", "num"):
		return Number
	case has("bool"):
		return Boolean
	case has("json"):
		return JSON
	}
	return Other
}

// typeOfValues is column i's type from its first value that isn't NULL
// (an expression's column, which SQLite gives no declared type).
func typeOfValues(rows [][]any, i int) string {
	for _, row := range rows {
		if i >= len(row) || row[i] == nil {
			continue
		}
		switch row[i].(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			return Integer
		case float32, float64:
			return Number
		case bool:
			return Boolean
		case string:
			return String
		case []byte:
			if utf8.Valid(row[i].([]byte)) {
				return String
			}
			return Bytes
		case time.Time:
			return Timestamp
		}
		return Other
	}
	return Other
}

// maxExact is the largest integer a double holds exactly (2^53).
const maxExact = 1 << 53

// Value is v, as a driver read it for a column of type typ, made JSON's
// (DAT-03): integers past 2^53 and decimals with more digits than a double
// keeps as strings, times as ISO 8601, bytes as base64, JSON as itself.
func Value(v any, typ string) any {
	switch x := v.(type) {
	case nil:
		return nil
	case bool:
		return x
	case int64:
		return integer(x, typ)
	case int:
		return integer(int64(x), typ)
	case int32:
		return integer(int64(x), typ)
	case int16:
		return integer(int64(x), typ)
	case int8:
		return integer(int64(x), typ)
	case uint64:
		if x > maxExact {
			return strconv.FormatUint(x, 10)
		}
		return integer(int64(x), typ)
	case uint32:
		return integer(int64(x), typ)
	case uint16:
		return integer(int64(x), typ)
	case uint8:
		return integer(int64(x), typ)
	case float64:
		return float(x)
	case float32:
		return float(float64(x))
	case *big.Int:
		if x == nil {
			return nil
		}
		if x.IsInt64() {
			return integer(x.Int64(), typ)
		}
		return x.String()
	case *big.Rat:
		if x == nil {
			return nil
		}
		return decimal(x.FloatString(digitsOf(x)))
	case time.Time:
		return timeText(x, typ)
	case []byte:
		return bytesValue(x, typ)
	case string:
		return text(x, typ)
	case [16]byte: // PostgreSQL's uuid, as pgx reads it
		return fmt.Sprintf("%x-%x-%x-%x-%x", x[0:4], x[4:6], x[6:8], x[8:10], x[10:16])
	case json.RawMessage:
		return x
	case map[string]any, []any:
		return x
	case driver.Valuer: // pgx's Numeric, Interval, Time…
		if dv, err := x.Value(); err == nil && dv != v {
			return Value(dv, typ)
		}
	case fmt.Stringer:
		return x.String()
	}
	if _, err := json.Marshal(v); err == nil {
		return v
	}
	return fmt.Sprint(v)
}

// integer is n as JSON: a number, a string past 2^53, a boolean for a
// boolean column (SQLite keeps them as 0 and 1).
func integer(n int64, typ string) any {
	if typ == Boolean && (n == 0 || n == 1) {
		return n == 1
	}
	if n > maxExact || n < -maxExact {
		return strconv.FormatInt(n, 10)
	}
	return n
}

// float is f as JSON, whose numbers have no NaN or infinities.
func float(f float64) any {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return strconv.FormatFloat(f, 'g', -1, 64)
	}
	return f
}

// digitsOf is how many decimals a rational's decimal form needs (at most
// 40).
func digitsOf(r *big.Rat) int {
	d := 0
	den := new(big.Int).Set(r.Denom())
	ten := big.NewInt(10)
	for d < 40 && new(big.Int).Mod(new(big.Int).Exp(ten, big.NewInt(int64(d)), nil), den).Sign() != 0 {
		d++
	}
	return d
}

// decimal is a decimal's text as a number when a double keeps all its
// digits (15 significant), else as the text.
func decimal(s string) any {
	digits := 0
	leading := true
	for _, r := range strings.TrimLeft(s, "+-") {
		switch {
		case r == '.':
			continue
		case r < '0' || r > '9':
			return s // an exponent, NaN, Infinity: kept as written
		case r == '0' && leading:
			continue
		}
		leading = false
		digits++
	}
	if t := strings.TrimRight(s, "0"); strings.Contains(s, ".") {
		digits -= len(s) - len(t) // trailing zeros after the point
	}
	if digits > 15 {
		return s
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}

// timeText is t in ISO 8601: a date's day, a time's clock, else the moment.
func timeText(t time.Time, typ string) string {
	switch typ {
	case Date:
		return t.Format(time.DateOnly)
	case Time:
		return t.Format("15:04:05.999999999")
	}
	return t.Format(time.RFC3339Nano)
}

// bytesValue is b for a column of type typ: base64 for bytes, JSON for
// JSON, text for the rest when it's text.
func bytesValue(b []byte, typ string) any {
	switch typ {
	case Bytes:
		return base64.StdEncoding.EncodeToString(b)
	case JSON:
		if json.Valid(b) {
			return json.RawMessage(append([]byte(nil), b...))
		}
	}
	if utf8.Valid(b) {
		return text(string(b), typ)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// text is s for a column of type typ: JSON's itself, a decimal's number
// when a double keeps it (DAT-03), else the text.
func text(s string, typ string) any {
	switch typ {
	case JSON:
		if json.Valid([]byte(s)) {
			return json.RawMessage(s)
		}
	case Number:
		return decimal(s)
	}
	return s
}

// MarkdownRows is how many rows Markdown shows (DAT-05).
const MarkdownRows = 50

// Markdown is t as a Markdown table, its first max rows (MarkdownRows when
// max is 0), then a line saying how many more there are and whether a
// limit cut the result (DAT-05).
func (t Table) Markdown(max int) string {
	if max <= 0 {
		max = MarkdownRows
	}
	if len(t.Columns) == 0 {
		return "(no columns)"
	}
	var b strings.Builder
	cell := func(s string) string {
		s = strings.ReplaceAll(s, "|", `\|`)
		return strings.Join(strings.Fields(s), " ")
	}
	b.WriteString("|")
	for _, c := range t.Columns {
		b.WriteString(" " + cell(c.Name) + " |")
	}
	b.WriteString("\n|")
	for _, c := range t.Columns {
		if c.Type == Integer || c.Type == Number {
			b.WriteString(" ---: |")
		} else {
			b.WriteString(" --- |")
		}
	}
	b.WriteString("\n")
	shown := min(max, len(t.Rows))
	for _, row := range t.Rows[:shown] {
		b.WriteString("|")
		for i := range t.Columns {
			var v any
			if i < len(row) {
				v = row[i]
			}
			b.WriteString(" " + cell(display(v)) + " |")
		}
		b.WriteString("\n")
	}
	if more := len(t.Rows) - shown; more > 0 {
		fmt.Fprintf(&b, "\n%d more rows (%d in all).\n", more, len(t.Rows))
	}
	if t.Truncated {
		fmt.Fprintf(&b, "\nThe result was cut at %d rows: the query returned more.\n", len(t.Rows))
	}
	return b.String()
}

// display is a value as a Markdown cell shows it: NULL as empty.
func display(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.RawMessage:
		return string(x)
	case map[string]any, []any:
		b, _ := json.Marshal(x)
		return string(b)
	}
	return fmt.Sprint(v)
}
