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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/secrets"
)

// Before a job moves to the organization's service, Blitz says what can't
// go with it (spec_053 EN-41): what runs or lives only on this computer.
func TestServiceProblems(t *testing.T) {
	w, _ := openTestWith(t, func(c *config.Config) {
		c.MCP.Servers = []config.MCPServerConfig{
			{Name: "files", Command: "npx", Args: []string{"server-files"}},
			{Name: "dev", URL: "http://localhost:3000/mcp"},
			{Name: "docs", URL: "https://mcp.acme.com/mcp"},
			{Name: "old", Command: "old-server", Disabled: true},
		}
	})
	addWorker(t, w, "report", "---\nschedule: daily at 6 AM\npermissions: [\"write:reports/\", \"write:/Users/ana/Desktop/\"]\n---\n"+
		"Read ~/Downloads/sales.csv and data/q4.csv, check http://127.0.0.1:8080/health, and write reports/q4.md.\n")
	problems, err := w.ServiceProblems("report")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"the MCP server files is a program on this computer (npx): the service can't start it",
		"the MCP server dev is at http://localhost:3000/mcp, on this computer",
		"it names /Users/ana/Desktop/, outside the workspace: the service has only the workspace",
		"it names ~/Downloads/sales.csv, outside the workspace: the service has only the workspace",
		"it reaches 127.0.0.1:8080, which on the service is the runner itself",
	}, problems)

	_, err = w.ServiceProblems("nope")
	assert.Error(t, err)
}

// A job that uses only the workspace and the internet can move.
func TestServiceProblemsNone(t *testing.T) {
	w, _ := openTestWith(t, nil)
	addWorker(t, w, "digest", "---\nschedule: daily at 6 AM\n---\nSummarize docs/notes.md from https://example.com/feed into out/digest.md.\n")
	problems, err := w.ServiceProblems("digest")
	require.NoError(t, err)
	assert.Empty(t, problems)

	// Moved: runs_on service, the rest as written.
	_, info, moved, err := w.MoveToService("digest", false)
	require.NoError(t, err)
	assert.True(t, moved)
	assert.Equal(t, "service", info.RunsOn)
	assert.Equal(t, "daily at 6 AM", info.Schedule)
}

// A job with problems moves only when asked to anyway.
func TestMoveToServiceForced(t *testing.T) {
	w, _ := openTestWith(t, nil)
	addWorker(t, w, "local", "---\nschedule: daily at 6 AM\n---\nCheck http://localhost:3000.\n")
	problems, _, moved, err := w.MoveToService("local", false)
	require.NoError(t, err)
	assert.False(t, moved)
	require.Len(t, problems, 1)
	_, info, moved, err := w.MoveToService("local", true)
	require.NoError(t, err)
	assert.True(t, moved, "anyway")
	assert.Equal(t, "service", info.RunsOn)
	_, _, _, err = w.MoveToService("nope", true)
	assert.Error(t, err)

	// One that doesn't read isn't moved: it says why.
	addWorker(t, w, "odd", "---\nschedule: whenever\n---\nDo it.\n")
	_, _, moved, err = w.MoveToService("odd", true)
	assert.False(t, moved)
	assert.ErrorContains(t, err, "doesn't read")
}

// A connector reading a file on this computer, outside the workspace,
// can't go with a job to the service (EN-41).
func TestServiceProblemsConnector(t *testing.T) {
	secrets.SetDefault(&secrets.Memory{})
	w, _ := openTestWith(t, nil)
	c := kitchen(t)
	_, problems, err := w.SaveConnector(context.Background(), c, nil, "")
	require.NoError(t, err)
	require.Empty(t, problems)
	addWorker(t, w, "menu", "---\nschedule: daily at 6 AM\n---\nList the recipes.\n")
	got, err := w.ServiceProblems("menu")
	require.NoError(t, err)
	assert.Equal(t, []string{"the connector kitchen reads " + c.Settings["database"] + ", outside the workspace"}, got)

	// Off in this workspace: not this job's.
	require.NoError(t, w.SetConnectorOn(context.Background(), c.Category, c.Scope, c.Name, false))
	got, err = w.ServiceProblems("menu")
	require.NoError(t, err)
	assert.Empty(t, got)
}
