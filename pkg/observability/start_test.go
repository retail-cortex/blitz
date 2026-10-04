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
	cfg.MCP.Servers = []config.MCPServerConfig{{Env: map[string]string{"TOKEN": "mcp-secret-value-123"}}}
	r := SecretRedactor(cfg)
	assert.NotContains(t, r.String("key AIzaSyTESTSECRET-0123456789 and mcp-secret-value-123"), "SECRET")
	assert.NotContains(t, r.String("mcp-secret-value-123"), "mcp-secret")
}
