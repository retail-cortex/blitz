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
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A process's diagnostics: the log opened where configured; a log that
// can't open is said and turned off, never fatal; configured secrets are
// masked.
func TestStartProcess(t *testing.T) {
	cfg := &config.Config{}
	cfg.Log = config.LogConfig{Level: "info", Dir: t.TempDir()}
	var warned []string
	stop := StartProcess(context.Background(), cfg, "test", func(m string) { warned = append(warned, m) })
	stop()
	assert.Empty(t, warned)

	file := filepath.Join(t.TempDir(), "a-file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	cfg.Log.Dir = filepath.Join(file, "logs") // can't be made
	StartProcess(context.Background(), cfg, "test", func(m string) { warned = append(warned, m) })()
	require.NotEmpty(t, warned)
	assert.Contains(t, warned[0], "diagnostic log disabled")

	cfg.LLM.Gemini.APIKey = "AIzaSyTESTSECRET-0123456789"
	cfg.MCP.Servers = []config.MCPServerConfig{{Env: map[string]string{"TOKEN": "mcp-secret-value-123"}, Headers: map[string]string{"Authorization": "Bearer hdr-secret-token-456"}}}
	r := SecretRedactor(cfg)
	assert.NotContains(t, r.String("key AIzaSyTESTSECRET-0123456789 and mcp-secret-value-123"), "SECRET")
	assert.NotContains(t, r.String("mcp-secret-value-123"), "mcp-secret")
	// A header's value, whole or its token alone.
	assert.NotContains(t, r.String("sent Authorization: Bearer hdr-secret-token-456"), "hdr-secret")
	assert.NotContains(t, r.String("token=hdr-secret-token-456"), "hdr-secret")
}

// Azure's key is masked like the others; RememberSecrets has a redactor
// built earlier (the process's log and telemetry, from the global
// settings at start) mask keys set later, a workspace's own, and MCP
// servers' tokens, but not their ordinary values.
func TestSecretRedactorLaterKeys(t *testing.T) {
	cfg := &config.Config{}
	cfg.LLM.Azure.APIKey = "0123456789abcdef0123456789abcdef"
	assert.Equal(t, "key [REDACTED]", SecretRedactor(cfg).String("key 0123456789abcdef0123456789abcdef"))

	early := SecretRedactor(&config.Config{}) // the process's, before any workspace
	later := &config.Config{}
	later.LLM.OpenAI.APIKey = "later-openai-key-Zq81"
	later.LLM.Azure.APIKey = "later-azure-key-Hk27x"
	later.Web.SearchAPIKey = "later-search-key-Pp55"
	later.MCP.Servers = []config.MCPServerConfig{{
		Env:     map[string]string{"GITHUB_TOKEN": "later-mcp-token-Ww90", "DATA_DIR": "/srv/plain-folder"},
		Headers: map[string]string{"Authorization": "Bearer later-bearer-Rr33", "Accept": "application/json"},
	}}
	for _, v := range []string{"later-openai-key-Zq81", "later-azure-key-Hk27x", "later-search-key-Pp55", "later-mcp-token-Ww90", "later-bearer-Rr33"} {
		assert.Contains(t, early.String(v), v, "not known yet")
	}
	RememberSecrets(later)
	cases := []struct {
		value  string
		masked bool
	}{
		{"later-openai-key-Zq81", true},
		{"later-azure-key-Hk27x", true},
		{"later-search-key-Pp55", true},
		{"later-mcp-token-Ww90", true},
		{"later-bearer-Rr33", true},
		{"/srv/plain-folder", false},
		{"application/json", false},
	}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			if tc.masked {
				assert.NotContains(t, early.String("x "+tc.value), tc.value)
			} else {
				assert.Contains(t, early.String("x "+tc.value), tc.value)
			}
		})
	}
}
