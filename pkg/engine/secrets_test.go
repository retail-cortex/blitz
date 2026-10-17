// Copyright 2026 Retail Cortex
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package engine

import (
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MCP servers' env and header values naming secrets get their values;
// the settings they came from keep the names.
func TestMCPWithSecrets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	secrets.SetDefault(&secrets.Memory{})
	dir := t.TempDir()
	v := config.Vault("")
	require.NoError(t, v.Set(secrets.GlobalScope, "gh", "global-gh-token"))
	require.NoError(t, v.Set(config.SecretScope("", dir), "db", "workspace-db-pass"))

	in := []config.MCPServerConfig{
		{Name: "github", Command: "x", Env: map[string]string{"GITHUB_TOKEN": "{secrets:gh}", "PLAIN": "1"}},
		{Name: "api", URL: "https://x", Headers: map[string]string{"Authorization": "Bearer {secrets:db}"}},
		{Name: "none", Command: "y"},
	}
	out := mcpWithSecrets(in, dir)
	assert.Equal(t, "global-gh-token", out[0].Env["GITHUB_TOKEN"])
	assert.Equal(t, "1", out[0].Env["PLAIN"])
	assert.Equal(t, "Bearer workspace-db-pass", out[1].Headers["Authorization"])
	assert.Equal(t, "{secrets:gh}", in[0].Env["GITHUB_TOKEN"], "the settings keep the name")
	assert.Nil(t, out[2].Env)
}
