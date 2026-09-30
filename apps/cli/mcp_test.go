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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPCommand(t *testing.T) {
	isolate(t)
	steps := []struct {
		args []string
		says []string
		err  string
	}{
		{args: []string{"mcp", "list"}, says: []string{"No MCP servers"}},
		{args: []string{"mcp", "add", "fs", "npx", "@acme/fs", "--env", "ROOT=/tmp/work"}, says: []string{`Added MCP server "fs"`}},
		{args: []string{"mcp", "add", "docs", "https://mcp.example/docs", "--header", "Authorization: Bearer abcdef123"}, says: []string{`Added MCP server "docs"`}},
		{args: []string{"mcp", "add", "fs", "other"}, err: "already configured"},
		{args: []string{"mcp", "add", "x", "cmd", "--header", "A:b"}, err: "--header is for http servers"},
		{args: []string{"mcp", "add", "x", "cmd", "--env", "novalue"}, err: "--env"},
		{args: []string{"mcp", "disable", "fs"}, says: []string{`Disabled MCP server "fs"`}},
		{args: []string{"mcp", "list"}, says: []string{"fs (disabled)\tnpx @acme/fs", "docs\thttps://mcp.example/docs"}},
		{args: []string{"mcp", "get", "docs"}, says: []string{"url:      https://mcp.example/docs", "header:   Authorization: Be****23"}},
		{args: []string{"mcp", "get", "fs"}, says: []string{"enabled:  false", "env:      ROOT=/t****rk"}},
		{args: []string{"mcp", "enable", "fs"}, says: []string{`Enabled MCP server "fs"`}},
		{args: []string{"mcp", "remove", "docs"}, says: []string{`Removed MCP server "docs"`}},
		{args: []string{"mcp", "remove", "docs"}, err: "no MCP server"},
		{args: []string{"mcp", "list"}, says: []string{"fs\tnpx @acme/fs"}},
	}
	for _, s := range steps {
		out, err := runCLI(t, s.args...)
		if s.err != "" {
			require.Error(t, err, "%v: %s", s.args, out)
			assert.Contains(t, err.Error()+out, s.err, "%v", s.args)
			continue
		}
		require.NoError(t, err, "%v: %s", s.args, out)
		for _, want := range s.says {
			assert.Contains(t, out, want, "%v", s.args)
		}
	}
}
