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
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// `blitz sessions usage` lists each call, then the totals (spec_056
// CA-125); a session with none says so.
func TestSessionUsageText(t *testing.T) {
	when := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	calls := []api.Call{
		{Time: when, Model: "gemini-3-pro", Purpose: api.PurposeTurn, Usage: api.Usage{Calls: 1, Input: 1200, Output: 80, CostUSD: 0.0034, Priced: true}},
		{Time: when, Model: "llama", Purpose: api.PurposeCompaction, Usage: api.Usage{Calls: 1, Input: 900, Output: 40, Unpriced: 1}},
		{Purpose: api.PurposeBefore, Usage: api.Usage{Calls: 3, CostUSD: 0.1, Priced: true}},
	}
	out := sessionUsageText("abc", calls, api.Sum(calls))
	assert.Contains(t, out, "Session usage (abc):")
	assert.Contains(t, out, "the conversation")
	assert.Contains(t, out, "1200 in, 80 out, $0.0034")
	assert.Contains(t, out, "900 in, 40 out, no price")
	assert.Contains(t, out, "before calls were recorded one by one")
	assert.Contains(t, out, "Estimated cost: $0.1034, plus 1 calls with no price")
	assert.Contains(t, out, "By model:")

	assert.Equal(t, "No usage recorded for abc.\n", sessionUsageText("abc", nil, api.Totals{}))
	all := []api.Call{{Model: "m", Usage: api.Usage{Calls: 1, CostUSD: 0.5, Priced: true}}}
	assert.Contains(t, sessionUsageText("abc", all, api.Sum(all)), "Estimated cost: $0.5000")
}

func TestSessionsExport(t *testing.T) {
	isolate(t)
	store, err := session.NewStorage(config.DefaultConfig().Session.StorageDir)
	require.NoError(t, err)
	rec, err := store.CreateSession("", "Fix the build", "blitz")
	require.NoError(t, err)
	require.NoError(t, store.AddMessage("user", "why does it fail?"))
	require.NoError(t, store.AddMessage("model", "A missing import."))

	out, err := runCLI(t, "sessions", "export", rec.ID)
	require.NoError(t, err)
	assert.Contains(t, out, "# Fix the build")
	assert.Contains(t, out, "why does it fail?")
	assert.Contains(t, out, "A missing import.")

	file := filepath.Join(t.TempDir(), "s.md")
	_, err = runCLI(t, "sessions", "export", rec.ID, "-o", file)
	require.NoError(t, err)
	data, _ := os.ReadFile(file)
	assert.Contains(t, string(data), "A missing import.")

	_, err = runCLI(t, "sessions", "export", "nope")
	assert.Equal(t, exitUsage, exitCodeFor(err))
}

// `blitz sessions usage <id>` lists a saved session's calls through the
// service; one it doesn't have is a usage error (spec_056 CA-125).
func TestSessionsUsageCommand(t *testing.T) {
	isolate(t)
	store, err := session.NewStorage(config.DefaultConfig().Session.StorageDir)
	require.NoError(t, err)
	rec, err := store.CreateSession("", "Fix the build", "blitz")
	require.NoError(t, err)
	require.NoError(t, store.AddMessage("user", "why does it fail?"))

	out, err := runCLI(t, "sessions", "usage", rec.ID)
	require.NoError(t, err)
	assert.Contains(t, out, rec.ID)
	_, err = runCLI(t, "sessions", "usage", "nope")
	assert.Equal(t, exitUsage, exitCodeFor(err))
}

func TestForkAndNameFlags(t *testing.T) {
	isolate(t)
	for name, args := range map[string][]string{
		"fork without resume": {"--fork", "hi"},
		"name with continue":  {"--name", "x", "--continue", "hi"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := runCLI(t, args...)
			assert.Equal(t, exitUsage, exitCodeFor(err), "%v", err)
		})
	}
}

// export needs a workspace and somewhere to write.
func TestSessionsExportErrors(t *testing.T) {
	isolate(t)
	store, err := session.NewStorage(config.DefaultConfig().Session.StorageDir)
	require.NoError(t, err)
	rec, err := store.CreateSession("", "t", "blitz")
	require.NoError(t, err)
	_, err = runCLI(t, "-d", filepath.Join(t.TempDir(), "missing"), "sessions", "export", rec.ID)
	assert.Equal(t, exitUsage, exitCodeFor(err))
	_, err = runCLI(t, "sessions", "export", rec.ID, "-o", filepath.Join(t.TempDir(), "no", "such", "file.md"))
	assert.Error(t, err)
}

func TestExportData(t *testing.T) {
	md, err := exportData("# Chat", "chat.md")
	require.NoError(t, err)
	assert.Equal(t, "# Chat", string(md))
	doc, err := exportData("# Chat", "out/Chat.DOCX")
	require.NoError(t, err)
	assert.Equal(t, "PK", string(doc[:2]), "a Word document is a zip")
}

func TestSessionsServiceDown(t *testing.T) {
	isolate(t)
	inner := startShared
	startShared = func(context.Context, string) error { return errors.New("service down") }
	t.Cleanup(func() { startShared = inner })
	for _, args := range [][]string{
		{"sessions", "export", "s1"},
		{"sessions", "usage", "s1"},
	} {
		_, err := runCLI(t, args...)
		assert.ErrorContains(t, err, "service down")
	}
}

// blitz sessions list and reasoning (spec_067_cli_parity CLP-47, CLP-48).
func TestSessionsListAndReasoning(t *testing.T) {
	isolate(t)
	store, err := session.NewStorage(config.DefaultConfig().Session.StorageDir)
	require.NoError(t, err)
	rec, err := store.CreateSession("", "Fix the build", "blitz")
	require.NoError(t, err)
	require.NoError(t, store.AddMessage("user", "why does it fail?"))

	out, err := runCLI(t, "sessions", "list", "--all")
	require.NoError(t, err)
	assert.Contains(t, out, rec.ID)
	assert.Contains(t, out, "Fix the build")
	out, err = runCLI(t, "sessions", "list", "--all", "--json")
	require.NoError(t, err)
	var list []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &list))
	require.NotEmpty(t, list)
	assert.Equal(t, rec.ID, list[0]["ID"])

	out, err = runCLI(t, "sessions", "reasoning", rec.ID)
	require.NoError(t, err)
	assert.Contains(t, out, "No turn to show yet", "no event log yet: nothing to draw")
}

// blitz help with a question and no model answers from the docs; blitz
// configure without one says why (CLP-22, CLP-23, CLP-30).
func TestHelpQuestionAndConfigureWithoutAModel(t *testing.T) {
	isolate(t)
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	out, err := runCLI(t, "help", "how", "do", "I", "add", "an", "MCP", "server")
	require.NoError(t, err)
	assert.Contains(t, out, "MCP servers")
	assert.Contains(t, out, "blitz config set-key")
	_, err = runCLI(t, "configure", "the", "dracula", "theme")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blitz doctor")
}

// The JSON is for scripts: an empty list is [], a session without turns has
// none, and the session list names what was saved (CLP-53).
func TestRunsAndReasoningJSON(t *testing.T) {
	isolate(t)
	store, err := session.NewStorage(config.DefaultConfig().Session.StorageDir)
	require.NoError(t, err)
	rec, err := store.CreateSession("", "Fix the build", "blitz")
	require.NoError(t, err)
	require.NoError(t, store.AddMessage("user", "why does it fail?")) // an empty session isn't listed

	out, err := runCLI(t, "sessions", "reasoning", rec.ID, "--json")
	require.NoError(t, err)
	var r api.Reasoning
	require.NoError(t, json.Unmarshal([]byte(out), &r))
	assert.Empty(t, r.Turns, "nothing has run in it")

	out, err = runCLI(t, "runs", "--json")
	require.NoError(t, err)
	assert.JSONEq(t, "[]", out, "no runs is an empty list, not null")

	out, err = runCLI(t, "sessions", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "No saved sessions here yet", "it belongs to no workspace, so not this one")
	out, err = runCLI(t, "sessions", "list", "--all")
	require.NoError(t, err)
	assert.Contains(t, out, rec.ID+"  Fix the build  1 message")
}
