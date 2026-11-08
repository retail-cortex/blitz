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

package tui

import (
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
)

// dataConns are a usable connection with a query that takes parameters
// and one that doesn't, an off one, an untrusted one and a broken one.
var dataConns = []api.DataConnection{
	{Name: "kitchen", Kind: "sqlite", ReadOnly: true, On: true, Trusted: true, Queries: []api.DataQueryInfo{
		{Name: "count", Description: "How many recipes"}, {Name: "longer", Params: []string{"m", "n"}},
	}},
	{Name: "attic", Kind: "postgres", Trusted: true},
	{Name: "clone", Kind: "sqlite", On: true},
	{Name: "broken", On: true, Trusted: true, Problem: "bad yaml"},
}

// /data lists the databases, what keeps one from a chat, and their
// queries (spec_059_data DAT-45).
func TestDataCommand(t *testing.T) {
	runCommandCases(t, map[string]commandCase{
		"connections": {
			setup: func(s *stubBackend) {
				s.dataConnections = func() ([]api.DataConnection, error) { return dataConns, nil }
			},
			line: "/data",
			want: []string{"Databases", "@db:kitchen", "sqlite · read-only", "/count: How many recipes", "/longer", "(takes m, n)",
				"@db:attic", "off in this workspace", "not trusted", "can't be read: bad yaml", "blitz data query"},
		},
		"none": {
			setup: func(s *stubBackend) { s.dataConnections = func() ([]api.DataConnection, error) { return nil, nil } },
			line:  "/data",
			want:  []string{"No database connections"},
		},
		"failed": {
			setup: func(s *stubBackend) { s.dataConnections = func() ([]api.DataConnection, error) { return nil, errBoom } },
			line:  "/data",
			want:  []string{"✗ boom"},
		},
		"in help": {line: "/help", want: []string{"/data", "@db:<name>"}},
	})
}

// A mention is offered for each connection a chat may use, and its
// queries a mention can fill (DAT-40).
func TestDataMentions(t *testing.T) {
	assert.Equal(t, []string{"db:kitchen", "db:kitchen/count"}, DataMentions(dataConns))
	assert.Empty(t, DataMentions(nil))
}

// The completer offers the mentions after @, @d or @db:, beside the
// workspace's paths, and asks for them only then.
func TestCompleterMentions(t *testing.T) {
	c := NewCompleter(t.TempDir())
	asked := 0
	c.Mentions(func() []string { asked++; return DataMentions(dataConns) })
	do := func(line string) string {
		cands, _ := c.Do([]rune(line), len([]rune(line)))
		var out []string
		for _, r := range cands {
			out = append(out, string(r))
		}
		return strings.Join(out, ",")
	}
	assert.Equal(t, "itchen,itchen/count", do("look at @db:k"))
	assert.Equal(t, "/count", do("@db:kitchen"))
	assert.Equal(t, "b:kitchen,b:kitchen/count", do("@d"))
	assert.Equal(t, 3, asked)
	assert.Empty(t, do("@src/"))
	assert.Equal(t, 3, asked, "a path isn't a mention: not asked")
}
