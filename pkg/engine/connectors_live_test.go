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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/connectors"
	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// olderRecipes is a read-only SQLite connection as Studio saved it before
// execute_sql was offered read-only (spec_046_databases DB-21).
const olderRecipes = `# kind: sqlite
# A connection for Blitz's agents (spec_047_connectors), written by Studio.
# ${NAME} is the Blitz secret NAME.
kind: source
name: recipes
type: sqlite
database: ./recipes.db
readOnly: true
---
kind: tool
name: recipes_list_tables
type: sqlite-sql
source: recipes
description: List the tables in recipes, with their columns.
statement: SELECT name, type, sql FROM sqlite_master WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%' ORDER BY name
`

// The paid check of DB-21: with the real Gemini agent and an older
// read-only connection, a question about the data is answered through the
// connection's execute_sql, never the shell, and nothing asks. Only with
// BLITZ_LIVE=1 and a Gemini key; it costs a little:
//
//	bazel test //pkg/engine:engine_test --test_filter=TestConnectorsLive \
//	  --test_env=BLITZ_LIVE=1 --test_env=GEMINI_API_KEY
func TestConnectorsLive(t *testing.T) {
	if os.Getenv("BLITZ_LIVE") != "1" || os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("BLITZ_LIVE=1 and GEMINI_API_KEY run it")
	}
	key := os.Getenv("GEMINI_API_KEY")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	t.Setenv("GEMINI_API_KEY", key)
	secrets.SetDefault(&secrets.Memory{})
	t.Chdir(t.TempDir())
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = dir
	cfg.Session.StorageDir = t.TempDir()
	cfg.Tools.ApprovalsFile = filepath.Join(t.TempDir(), "a.json")
	cfg.Blitz.PermissionMode = "default"

	data, err := os.ReadFile(kitchenDB(t))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "recipes.db"), data, 0o600))
	file := filepath.Join(connectors.WorkspaceDir(dir), "databases", "recipes.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
	require.NoError(t, os.WriteFile(file, []byte(olderRecipes), 0o600))
	require.NoError(t, connectorTrust(cfg).Trust(file, connectors.Hash(file)), "the person trusted it, as in Studio")

	w, err := Open(context.Background(), cfg, Options{})
	require.NoError(t, err)
	defer w.Close()
	var asked []string
	w.SetUI(func(_ context.Context, r api.ApprovalRequest) (api.Decision, error) {
		asked = append(asked, r.Tool+": "+r.Detail)
		return api.DecisionDeny, nil
	}, func(context.Context, string, []string) (string, error) { return "", nil })
	s, err := w.NewSession()
	require.NoError(t, err)
	var calls []string
	res, err := w.Run(context.Background(), s.ID, api.Turn{Text: "What recipes are in the database? Just their names."}, func(ev api.Event) {
		if ev.ToolCall != nil && !ev.ToolCall.Partial {
			calls = append(calls, ev.ToolCall.Name)
		}
	})
	require.NoError(t, err)
	t.Logf("tools %v; asked %v; answer %q", calls, asked, res.Output)
	assert.Contains(t, calls, "db__recipes_execute_sql")
	assert.NotContains(t, calls, "run_shell_command")
	assert.Empty(t, asked, "nothing asks")
	assert.Contains(t, strings.ToLower(res.Output), "soup")
	assert.Contains(t, strings.ToLower(res.Output), "bread")
}
