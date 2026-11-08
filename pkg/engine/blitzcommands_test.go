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
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/connectors"
	builtinskills "github.com/retail-cortex/blitz/pkg/engine/skills/builtin"
	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every command of the catalog runs somewhere: the workspace's handlers,
// or the tool's own (help, the jobs) (spec_060_blitz_tools DRV-60).
func TestEveryBlitzCommandRuns(t *testing.T) {
	own := map[string]bool{"help": true, "job create": true, "job list": true}
	for _, c := range api.BlitzCommands {
		_, handled := blitzHandlers[c.Name]
		assert.True(t, handled || own[c.Name], "%s has no handler", c.Name)
	}
	for name := range blitzHandlers {
		_, ok := api.FindBlitzCommand(name)
		assert.True(t, ok, "%s is handled but not in the catalog", name)
	}
}

// blitzWorkspace is a test workspace whose approvals are answered with d
// (recorded in asked) and whose secret questions with secret.
func blitzWorkspace(t *testing.T, d api.Decision, secret string, mutate func(*config.Config)) (*Workspace, *[]api.ApprovalRequest, *[]string) {
	t.Helper()
	secrets.SetDefault(&secrets.Memory{}) // never the machine's keychain
	w, _ := openTestWith(t, mutate)
	var asked []api.ApprovalRequest
	var questions []string
	w.SetUI(func(_ context.Context, r api.ApprovalRequest) (api.Decision, error) {
		asked = append(asked, r)
		return d, nil
	}, func(ctx context.Context, q string, _ []string) (string, error) {
		questions = append(questions, q)
		if !api.SecretAnswer(ctx) {
			t.Errorf("a secret asked without the secret mark: %s", q)
		}
		return secret, nil
	})
	return w, &asked, &questions
}

func runCmd(t *testing.T, w *Workspace, name string, args map[string]any) (any, error) {
	t.Helper()
	c, ok := api.FindBlitzCommand(name)
	require.True(t, ok, name)
	return w.runBlitzCommand(context.Background(), c, args)
}

// connector save writes a SQLite connection after the person approves the
// file it shows; it's trusted, on and tested. Declined, nothing is written
// (spec_060_blitz_tools DRV-11, DRV-15).
func TestConnectorSaveSQLite(t *testing.T) {
	w, asked, _ := blitzWorkspace(t, api.DecisionOnce, "", nil)
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "recipes.db"), nil, 0o600))
	args := map[string]any{"kind": "sqlite", "name": "recipes", "description": "The recipes", "settings": map[string]any{"database": "recipes.db"}}
	out, err := runCmd(t, w, "connector save", args)
	require.NoError(t, err)
	require.Len(t, *asked, 1)
	req := (*asked)[0]
	assert.Equal(t, "blitz:connector save", req.Tool)
	assert.Equal(t, api.ActionBlitz, req.Kind)
	assert.Contains(t, req.Diff, "+++ b/.agents/connectors/databases/recipes.yaml")
	assert.Contains(t, req.Diff, "recipes.db")
	res := out.(map[string]any)
	assert.Equal(t, filepath.Join(".agents", "connectors", "databases", "recipes.yaml"), res["saved"])
	assert.Equal(t, true, res["on"])
	assert.Contains(t, res["next"], "@db:recipes")
	got := connectionNamed(t, w, "recipes")
	assert.True(t, got.Trusted, "approved in the form: trusted")
	assert.True(t, got.On)

	// Declined: nothing written.
	w2, _, _ := blitzWorkspace(t, api.DecisionDeny, "", nil)
	_, err = runCmd(t, w2, "connector save", args)
	assert.Error(t, err)
	_, statErr := os.Stat(filepath.Join(w2.Dir(), ".agents", "connectors", "databases", "recipes.yaml"))
	assert.True(t, os.IsNotExist(statErr), "declined: nothing written")
}

func connectionNamed(t *testing.T, w *Workspace, name string) connectors.Connection {
	t.Helper()
	for _, c := range w.Connectors() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no connection %s", name)
	return connectors.Connection{}
}

// A connection that's wrong is refused with why, asking nothing; a secret
// passed as an argument is never written (DRV-05, DRV-11).
func TestConnectorSaveRefusals(t *testing.T) {
	w, asked, _ := blitzWorkspace(t, api.DecisionOnce, "", nil)
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{name: "an unknown kind", args: map[string]any{"kind": "mongo", "name": "x"}, want: "there's no kind"},
		{name: "an unknown field", args: map[string]any{"kind": "sqlite", "name": "x", "settings": map[string]any{"colour": "red"}}, want: "has no field"},
		{name: "no name", args: map[string]any{"kind": "sqlite", "name": "", "settings": map[string]any{"database": "a.db"}}, want: "name"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := runCmd(t, w, "connector save", c.args)
			assert.ErrorContains(t, err, c.want)
		})
	}
	assert.Empty(t, *asked, "nothing asked for a connection that can't be saved")

	// A path naming no file is saved, and its test says why it can't
	// connect (spec_046 DB-17: the connection's problem, not the save's).
	out, err := runCmd(t, w, "connector save", map[string]any{"kind": "sqlite", "name": "later", "settings": map[string]any{"database": "nope.db"}})
	require.NoError(t, err)
	assert.Contains(t, out.(map[string]any)["test"], "couldn't connect")
}

// A PostgreSQL connection's password is asked of the person, hidden, after
// the file's approved; it's never an argument, and never in the file.
func TestConnectorSaveAsksForTheSecret(t *testing.T) {
	w, asked, questions := blitzWorkspace(t, api.DecisionOnce, "s3cret!", nil)
	_, err := runCmd(t, w, "connector save", map[string]any{"kind": "postgres", "name": "shop",
		"settings": map[string]any{"host": "127.0.0.1", "port": "1", "database": "shop", "user": "me", "password": "from-the-model"}})
	require.NoError(t, err)
	require.Len(t, *asked, 1)
	assert.Contains(t, (*asked)[0].Detail, "then you're asked for password")
	require.Len(t, *questions, 1)
	text, err := os.ReadFile(filepath.Join(w.Dir(), ".agents", "connectors", "databases", "shop.yaml"))
	require.NoError(t, err)
	assert.NotContains(t, string(text), "from-the-model")
	assert.NotContains(t, string(text), "s3cret!")
}

// The mode only goes stricter; other settings change after approval
// (DRV-71, DRV-80).
func TestSettingsSet(t *testing.T) {
	w, asked, _ := blitzWorkspace(t, api.DecisionOnce, "", nil)
	_, err := runCmd(t, w, "settings set", map[string]any{"key": "mode", "value": "bypass"})
	assert.ErrorContains(t, err, "only changes to a stricter one")
	assert.Empty(t, *asked)
	_, err = runCmd(t, w, "settings set", map[string]any{"key": "mode", "value": "plan"})
	require.NoError(t, err)
	assert.Equal(t, "plan", w.Settings().PermissionMode)
	_, err = runCmd(t, w, "settings set", map[string]any{"key": "effort", "value": "high"})
	require.NoError(t, err)
	assert.Equal(t, "high", w.Settings().Effort)
	out, err := runCmd(t, w, "settings show", nil)
	require.NoError(t, err)
	assert.Equal(t, "plan", out.(map[string]any)["mode"])
}

// MCP servers bring code: always asked (MustAsk), and refused where the
// organization keeps people's own out (DRV-72, DRV-75).
func TestMCPAdd(t *testing.T) {
	w, asked, _ := blitzWorkspace(t, api.DecisionDeny, "", nil)
	_, err := runCmd(t, w, "mcp add", map[string]any{"name": "gh", "command": "npx", "args": []any{"-y", "github-mcp"}})
	assert.Error(t, err)
	require.Len(t, *asked, 1)
	assert.True(t, (*asked)[0].MustAsk)
	assert.Contains(t, (*asked)[0].Detail, "runs: npx -y github-mcp")
	_, err = runCmd(t, w, "mcp add", map[string]any{"name": "gh"})
	assert.ErrorContains(t, err, "either command")
}

// Doctor answers each check with its state; the organization's policy
// says there's none without one (DRV-81, DRV-82).
func TestDoctorAndOrg(t *testing.T) {
	w, _, _ := blitzWorkspace(t, api.DecisionOnce, "", nil)
	out, err := runCmd(t, w, "doctor", nil)
	require.NoError(t, err)
	checks := out.([]doctorCheck)
	require.NotEmpty(t, checks)
	assert.Equal(t, "model", checks[0].Check)
	pol, err := runCmd(t, w, "org policy", nil)
	require.NoError(t, err)
	assert.Equal(t, false, pol.(map[string]any)["organization"])
}

// From the CLI, a change first answers with what to confirm and the
// secrets it needs, then runs with them (DRV-02).
func TestRunBlitzCommandFromTheCLI(t *testing.T) {
	w, asked, _ := blitzWorkspace(t, api.DecisionDeny, "", nil)
	ctx := context.Background()
	run, err := w.RunBlitzCommand(ctx, "help", nil, false, nil)
	require.NoError(t, err)
	assert.Contains(t, run.Help, "connector save")
	run, err = w.RunBlitzCommand(ctx, " Secret  SET ", map[string]any{"name": "API_TOKEN"}, false, nil)
	require.NoError(t, err)
	assert.Contains(t, run.Confirm, "Set this workspace's secret API_TOKEN")
	assert.Equal(t, []string{"API_TOKEN"}, run.Asks)
	run, err = w.RunBlitzCommand(ctx, "secret set", map[string]any{"name": "API_TOKEN"}, true, map[string]string{"API_TOKEN": "abc"})
	require.NoError(t, err)
	assert.Equal(t, "${API_TOKEN}", run.Result.(map[string]any)["use"])
	assert.Empty(t, *asked, "the terminal confirmed: the window wasn't asked")
	names, err := w.RunBlitzCommand(ctx, "secret list", nil, false, nil)
	require.NoError(t, err)
	assert.Contains(t, names.Result.(map[string]any)["names"], "API_TOKEN")

	_, err = w.RunBlitzCommand(ctx, "connector frobnicate", nil, false, nil)
	assert.ErrorContains(t, err, "no command")
	run, err = w.RunBlitzCommand(ctx, "connector enable", nil, false, nil)
	assert.ErrorContains(t, err, "name is required")
	assert.True(t, strings.HasPrefix(run.Help, "connector enable:"))
}

// The blitz skill names only commands the catalog has (DRV-22): every
// backticked "group verb" whose group is a catalog group.
func TestBlitzSkillNamesRealCommands(t *testing.T) {
	text, err := fs.ReadFile(builtinskills.FS, "blitz/SKILL.md")
	require.NoError(t, err)
	groups := map[string]bool{}
	for _, g := range api.BlitzGroups() {
		groups[g[0].Group()] = true
	}
	named := 0
	for _, m := range regexp.MustCompile("`([a-z_]+(?: [a-z_]+)?)`").FindAllStringSubmatch(string(text), -1) {
		words := strings.Fields(m[1])
		if !groups[words[0]] || len(words) == 1 && !slices.ContainsFunc(api.BlitzCommands, func(c api.BlitzCommand) bool { return c.Name == words[0] }) {
			continue // a group named alone, or not a command
		}
		named++
		_, ok := api.FindBlitzCommand(m[1])
		assert.True(t, ok, "the skill names %q, which isn't a command", m[1])
	}
	assert.Greater(t, named, 10, "the skill names its commands")
}
