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

package observability

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeLog(t *testing.T, dir, day, text string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blitz-"+day+".jsonl"), []byte(text), 0o600))
}

func TestLogDays(t *testing.T) {
	dir := t.TempDir()
	days, err := LogDays(filepath.Join(dir, "none"))
	require.NoError(t, err)
	assert.Empty(t, days)
	writeLog(t, dir, "2026-09-27", "")
	writeLog(t, dir, "2026-09-28", "")
	writeLog(t, dir, "not-a-day", "")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "audit-2026-09-28.jsonl"), nil, 0o600))
	days, err = LogDays(dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"2026-09-28", "2026-09-27"}, days)
}

func TestReadLog(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "2026-09-27", `{"time":"2026-09-27T10:00:00Z","level":"INFO","msg":"older day"}`+"\n")
	writeLog(t, dir, "2026-09-28", `{"time":"2026-09-28T10:00:00.5Z","level":"DEBUG","msg":"tick","n":1}
{"time":"2026-09-28T10:00:01Z","level":"INFO","msg":"workspace opened","workspace":"/home/me/proj"}
{"time":"2026-09-28T10:00:02Z","level":"WARN","msg":"model unavailable","workspace":"/home/me/proj","error":"quota exceeded"}
{"time":"2026-09-28T10:00:03Z","level":"ERROR","msg":"turn failed","error":"boom","attempt":2,"ok":false}
{"time":"2026-09-28T10:00:04Z","level":"INF`)
	cases := []struct {
		name    string
		q       LogQuery
		msgs    []string
		matched int
	}{
		{"newest day, all, newest first", LogQuery{}, []string{"turn failed", "model unavailable", "workspace opened", "tick"}, 4},
		{"a day", LogQuery{Day: "2026-09-27"}, []string{"older day"}, 1},
		{"warnings and errors", LogQuery{MinLevel: "warn"}, []string{"turn failed", "model unavailable"}, 2},
		{"errors", LogQuery{MinLevel: "ERROR"}, []string{"turn failed"}, 1},
		{"text in any field", LogQuery{Text: "PROJ quota"}, []string{"model unavailable"}, 1},
		{"limit keeps the newest", LogQuery{Limit: 2}, []string{"turn failed", "model unavailable"}, 4},
		{"a day with no log", LogQuery{Day: "2026-01-01"}, nil, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			page, err := ReadLog(dir, c.q)
			require.NoError(t, err)
			var msgs []string
			for _, e := range page.Entries {
				msgs = append(msgs, e.Msg)
			}
			assert.Equal(t, c.msgs, msgs)
			assert.Equal(t, c.matched, page.Matched)
		})
	}
	page, err := ReadLog(dir, LogQuery{MinLevel: "error"})
	require.NoError(t, err)
	e := page.Entries[0]
	assert.Equal(t, "2026-09-28", page.Day)
	assert.Equal(t, filepath.Join(dir, "blitz-2026-09-28.jsonl"), page.Path)
	assert.Equal(t, "ERROR", e.Level)
	assert.Equal(t, 3, e.Time.Second())
	assert.Equal(t, []LogAttr{{"error", "boom"}, {"attempt", "2"}, {"ok", "false"}}, e.Attrs, "fields in order, values as text")

	for _, bad := range []LogQuery{{Day: "../x"}, {MinLevel: "loud"}} {
		_, err := ReadLog(dir, bad)
		assert.Error(t, err, "%+v", bad)
	}
	empty, err := ReadLog(filepath.Join(dir, "none"), LogQuery{})
	require.NoError(t, err)
	assert.Empty(t, empty.Entries)
}
