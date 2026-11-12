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
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/observability"
	"github.com/retail-cortex/blitz/pkg/redact"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gatewayLogLines runs call with the default logger writing at levels, and
// returns the file's raw text and its gateway lines (LOG-01b).
func gatewayLogLines(t *testing.T, levels map[string]string, call func()) (string, []map[string]any) {
	t.Helper()
	dir := t.TempDir()
	logger, closer, err := observability.OpenLog(config.LogConfig{Level: "info", Levels: levels, Dir: dir}, redact.New())
	require.NoError(t, err)
	old := slog.Default()
	slog.SetDefault(logger)
	call()
	slog.SetDefault(old)
	require.NoError(t, closer.Close())

	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if len(files) == 0 {
		return "", nil
	}
	raw, err := os.ReadFile(files[0])
	require.NoError(t, err)
	var lines []map[string]any
	sc := bufio.NewScanner(strings.NewReader(string(raw)))
	for sc.Scan() {
		var m map[string]any
		require.NoError(t, json.Unmarshal(sc.Bytes(), &m))
		if name, _ := m[observability.LoggerKey].(string); name == "gateway" || name == "gateway.token" {
			lines = append(lines, m)
		}
	}
	return string(raw), lines
}

// With log.levels' gateway at debug, each call to the gateway is logged:
// the model, each attempt and why it was sent, the status, an error's
// body, and the token script's runs; the token never is, in a header or
// in the URL.
func TestGatewayDebugLog(t *testing.T) {
	g := newFakeGateway(t, [2]any{http.StatusUnauthorized, `token expired`})
	script, _ := tokenScript(t)
	raw, lines := gatewayLogLines(t, map[string]string{"gateway": "debug"}, func() {
		m := apiTestModel(t, g, script, 0, func(c *config.APIConfig) {
			c.BaseURL = g.URL + "/t/" + config.TokenPlaceholder + "/gw"
		})
		_, _, err := callAPI(inSession("s-log"), m, helloRequest())
		require.NoError(t, err)
	})
	assert.NotContains(t, raw, "gwtok-", "a token reached the log")

	var msgs []string
	var calls []map[string]any
	for _, l := range lines {
		msgs = append(msgs, l["msg"].(string))
		if l["msg"] == "gateway call" {
			calls = append(calls, l)
		}
	}
	assert.Contains(t, msgs, "gateway model")
	assert.Contains(t, msgs, "gateway token script ran")
	assert.Contains(t, msgs, "gateway token rejected: running the script again")
	require.Len(t, calls, 2, "lines %v", msgs)
	assert.Equal(t, "first", calls[0]["why"])
	assert.Equal(t, float64(401), calls[0]["status"])
	assert.Equal(t, "token expired", calls[0]["body"])
	assert.Equal(t, "responses", calls[0]["api"])
	assert.Equal(t, "gpt-5", calls[0]["model"])
	assert.Equal(t, "s-log", calls[0]["session"])
	assert.Contains(t, calls[0]["url"], "/t/"+config.TokenPlaceholder+"/gw/responses")
	assert.Equal(t, "a fresh token after 401", calls[1]["why"])
	assert.Equal(t, float64(2), calls[1]["attempt"])
	assert.Equal(t, float64(200), calls[1]["status"])
}

// The levels apply: at info nothing of the gateway is written, and
// gateway.token can be quieter than gateway.
func TestGatewayLogLevels(t *testing.T) {
	tests := []struct {
		name      string
		levels    map[string]string
		wantCalls bool
		wantToken bool
	}{
		{"root info", nil, false, false},
		{"gateway debug", map[string]string{"gateway": "debug"}, true, true},
		{"token quieter", map[string]string{"gateway": "debug", "gateway.token": "warn"}, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := newFakeGateway(t)
			script, _ := tokenScript(t)
			_, lines := gatewayLogLines(t, tc.levels, func() {
				_, _, err := callAPI(context.Background(), apiTestModel(t, g, script, 0), helloRequest())
				require.NoError(t, err)
			})
			var calls, tokens int
			for _, l := range lines {
				switch l[observability.LoggerKey] {
				case "gateway":
					calls++
				case "gateway.token":
					tokens++
				}
			}
			assert.Equal(t, tc.wantCalls, calls > 0)
			assert.Equal(t, tc.wantToken, tokens > 0)
		})
	}
}

// A failed call logs the error it reports and the adapter's own.
func TestGatewayLogsFailure(t *testing.T) {
	g := newFakeGateway(t, [2]any{http.StatusBadRequest, `{"error":{"message":"no such model"}}`})
	script, _ := tokenScript(t)
	_, lines := gatewayLogLines(t, map[string]string{"gateway": "debug"}, func() {
		_, _, err := callAPI(context.Background(), apiTestModel(t, g, script, 0), helloRequest())
		require.Error(t, err)
	})
	var failed map[string]any
	for _, l := range lines {
		if l["msg"] == "gateway call failed" {
			failed = l
		}
	}
	require.NotNil(t, failed)
	assert.Contains(t, failed["error"], "[llm.api] HTTP 400 Bad Request: no such model")
	assert.NotEmpty(t, failed["cause"])
}
