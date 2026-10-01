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

package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// counterCommand prints key-<n>, n counting its runs.
func counterCommand(t *testing.T) (string, func() int) {
	t.Helper()
	dir := t.TempDir()
	count := filepath.Join(dir, "n")
	cmd := fmt.Sprintf(`n=$(cat %q 2>/dev/null || echo 0); n=$((n+1)); echo $n > %q; echo "key-$n"`, count, count)
	return cmd, func() int {
		data, _ := os.ReadFile(count)
		var n int
		fmt.Sscan(string(data), &n)
		return n
	}
}

func TestCommandKey(t *testing.T) {
	cmd, runs := counterCommand(t)
	k, err := keyFromCommand(cmd, "50ms")
	require.NoError(t, err)
	ctx := context.Background()
	key, err := k.get(ctx)
	require.NoError(t, err)
	assert.Equal(t, "key-1", key)
	key, _ = k.get(ctx)
	assert.Equal(t, "key-1", key, "cached")
	time.Sleep(60 * time.Millisecond)
	key, _ = k.get(ctx)
	assert.Equal(t, "key-2", key, "run again after the TTL")
	assert.Equal(t, 2, runs())

	same, _ := keyFromCommand(cmd, "50ms")
	assert.Same(t, k, same, "shared")

	for _, tt := range []struct{ name, cmd, ttl, wantErr string }{
		{"bad ttl", "echo k", "soon", "api_key_ttl"},
		{"fails", "echo oops >&2; exit 3", "", "oops"},
		{"prints nothing", "true", "", "printed no key"},
		{"two lines", "printf 'a\\nb\\n'", "", "more than one line"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := commandKeyNow(ctx, tt.cmd, tt.ttl)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

// Each provider's requests carry the command's current key.
func TestKeyCommandProviders(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization")+r.Header.Get("X-Api-Key")+r.Header.Get("X-Goog-Api-Key"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/models") && strings.Contains(r.URL.Path, "/v1/"):
			if r.Header.Get("X-Api-Key") != "" {
				json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"id": "claude-test", "type": "model", "display_name": "t", "created_at": "2026-01-01T00:00:00Z"}}, "has_more": false})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []any{map[string]any{"id": "gpt-test", "object": "model", "created": 1, "owned_by": "x"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cmd, _ := counterCommand(t)
	cfg := config.DefaultConfig()
	cfg.LLM.OpenAI = config.OpenAIConfig{APIKeyCommand: cmd, BaseURL: srv.URL + "/v1"}
	cfg.LLM.Anthropic = config.AnthropicConfig{APIKeyCommand: cmd, BaseURL: srv.URL}
	cfg.LLM.Gemini = config.GeminiConfig{}
	assert.Equal(t, []string{"anthropic", "openai"}, ConfiguredProviders(cfg))

	got := ListModels(context.Background(), cfg)
	require.Len(t, got, 2)
	for _, pm := range got {
		require.NoError(t, pm.Err, pm.Provider)
	}
	assert.Equal(t, "claude-test", got[0].Models[0].ID)
	assert.Equal(t, "gpt-test", got[1].Models[0].ID)
	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(seen, " ")
	assert.Contains(t, joined, "Bearer key-", "openai")
	assert.Regexp(t, `(^| )key-\d`, joined, "anthropic's x-api-key")

	_, err := listProvider(context.Background(), cfg, "carrier-pigeon")
	assert.ErrorContains(t, err, "unknown provider")
}

// The key transport puts the command's key in the named header, as a
// bearer token when asked, and fails the request when the command does.
func TestKeyTransport(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("X-Key")+"|"+r.Header.Get("Authorization"))
	}))
	defer srv.Close()
	k, err := keyFromCommand("echo transport-key", "")
	require.NoError(t, err)

	for _, bearer := range []bool{false, true} {
		header := "X-Key"
		if bearer {
			header = "Authorization"
		}
		c := withKeyCommand(&http.Client{}, k, header, bearer)
		resp, err := c.Get(srv.URL)
		require.NoError(t, err)
		resp.Body.Close()
	}
	assert.Equal(t, []string{"transport-key|", "|Bearer transport-key"}, got)

	bad, err := keyFromCommand("exit 1", "")
	require.NoError(t, err)
	_, err = withKeyCommand(srv.Client(), bad, "X-Key", false).Get(srv.URL)
	assert.ErrorContains(t, err, "api_key_command failed")
}
