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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelsCommand(t *testing.T) {
	isolate(t)
	_, err := runCLI(t, "models")
	assert.Equal(t, exitUsage, exitCodeFor(err), "no provider set up: %v", err)
	out, err := runCLI(t, "models", "carrier-pigeon")
	assert.Equal(t, exitFailure, exitCodeFor(err))
	assert.Contains(t, out, "can't list its models: unknown provider carrier-pigeon")
}

// blitz models lists each configured provider's models, with the prices
// Blitz knows and a note where only the configured model is known.
func TestModelsCommandLists(t *testing.T) {
	home := isolate(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"object":"list","data":[{"id":"local-model","object":"model"},{"id":"gpt-4o","object":"model"}]}`)
	}))
	t.Cleanup(srv.Close)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".blitz"), 0o700))
	settings := fmt.Sprintf("[llm]\nprovider = \"ollama\"\n[llm.openai]\nbase_url = %q\n[llm.azure]\nresource = \"res\"\nmodel = \"gpt-4o\"\n", srv.URL+"/v1")
	require.NoError(t, os.WriteFile(filepath.Join(home, ".blitz", ".env.toml"), []byte(settings), 0o600))
	out, err := runCLI(t, "models")
	require.NoError(t, err, out)
	assert.Contains(t, out, "ollama/local-model")
	assert.Regexp(t, `ollama/gpt-4o\s+\$[0-9.]+ in, \$[0-9.]+ out per million tokens`, out)
	assert.Contains(t, out, "(the configured model: azure's console lists the others)")
	assert.Contains(t, out, "azure/gpt-4o")

	_, err = runCLI(t, "-d", filepath.Join(t.TempDir(), "missing"), "models")
	assert.Equal(t, exitUsage, exitCodeFor(err))
}
