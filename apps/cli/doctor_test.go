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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wantCheck is a doctor line a case expects: its name, status and a part
// of its detail.
type wantCheck struct {
	name   string
	status checkStatus
	text   string
}

// doctorHome isolates a home whose settings are settings, with the
// variables doctor reads cleared, and returns it with a workspace.
func doctorHome(t *testing.T, settings string) (home, ws string) {
	t.Helper()
	home = isolate(t)
	for _, k := range []string{"GOOGLE_CLOUD_PROJECT", "GOOGLE_CLOUD_LOCATION", "GOOGLE_APPLICATION_CREDENTIALS", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_PROFILE",
		"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "BLITZ_LOG_LEVEL", "BLITZ_TELEMETRY"} {
		t.Setenv(k, "")
	}
	t.Setenv("CLOUDSDK_CONFIG", filepath.Join(home, "no-gcloud"))
	t.Setenv("GCE_METADATA_HOST", "127.0.0.1:1") // no metadata server probe
	t.Setenv("ANTHROPIC_CONFIG_DIR", filepath.Join(home, "ant"))
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("BLITZ_SOCKET", filepath.Join(t.TempDir(), "none.sock"))
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".blitz"), 0o700))
	if settings != "" {
		require.NoError(t, os.WriteFile(filepath.Join(home, ".blitz", ".env.toml"), []byte(settings), 0o600))
	}
	return home, t.TempDir()
}

// anyStatus matches a check of any status.
const anyStatus checkStatus = -1

// mcpTestServer is an MCP server over HTTP with the one tool.
func mcpTestServer(t *testing.T, tool string) string {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: tool, Description: "A tool"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{}, nil, nil
	})
	ts := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil))
	t.Cleanup(ts.Close)
	t.Cleanup(func() {
		for ss := range srv.Sessions() {
			ss.Close()
		}
	})
	return ts.URL
}

// assertChecks finds each wanted check among checks.
func assertChecks(t *testing.T, checks []check, want []wantCheck) {
	t.Helper()
	for _, w := range want {
		found := false
		for _, c := range checks {
			if c.name == w.name && (c.status == w.status || w.status == anyStatus) && strings.Contains(c.detail, w.text) {
				found = true
			}
		}
		assert.True(t, found, "no %q check with status %d and %q in:\n%s", w.name, w.status, w.text, describeChecks(checks))
	}
}

func describeChecks(checks []check) string {
	var b strings.Builder
	printChecks(&b, checks)
	return b.String()
}

// searxng answers searches with two results, or fails with HTTP 500.
func searxng(t *testing.T, fail bool) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		io.WriteString(w, `{"results":[{"title":"Go","url":"https://go.dev","content":"x"},{"title":"Go2","url":"https://go.dev/doc","content":"y"}]}`)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// failingModel is an OpenAI-compatible server that refuses every request.
func failingModel(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"nope"}}`, http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/v1"
}

// doctor reports each setting it checks: credentials for every provider,
// the log, telemetry, models, the sandbox, permissions, web search, the
// browser, MCP servers, hooks, sessions, the audit log and memory.
func TestDoctorChecks(t *testing.T) {
	tests := []struct {
		name     string
		settings func(t *testing.T) string
		env      map[string]string
		online   bool
		want     []wantCheck
	}{
		{name: "anthropic without a key", settings: lit("[llm]\nprovider = \"anthropic\"\n"),
			want: []wantCheck{{"credentials", statusWarn, "no api_key"}}},
		{name: "anthropic with a token", settings: lit("[llm]\nprovider = \"anthropic\"\n"), env: map[string]string{"ANTHROPIC_AUTH_TOKEN": "tok"},
			want: []wantCheck{{"credentials", statusOK, "ANTHROPIC_AUTH_TOKEN is set"}}},
		{name: "bedrock", settings: lit("[llm]\nprovider = \"bedrock\"\n[llm.bedrock]\nregion = \"us-east-1\"\n"),
			want: []wantCheck{{"credentials", statusOK, `AWS's credential chain, region "us-east-1"`}}},
		{name: "azure", settings: lit("[llm]\nprovider = \"azure\"\n[llm.azure]\nresource = \"res\"\n"),
			want: []wantCheck{{"credentials", statusOK, `Azure resource "res", auth "api_key"`}}},
		{name: "claude on vertex", settings: lit("[llm]\nprovider = \"vertex-anthropic\"\n"),
			want: []wantCheck{{"credentials", statusFail, "Claude on Vertex AI, Google Cloud ADC needs a project"}}},
		{name: "key command", settings: lit("[llm]\nprovider = \"openai\"\n[llm.openai]\napi_key_command = \"echo k\"\n"),
			want: []wantCheck{{"credentials", statusOK, "api_key_command"}}},
		{name: "no key", settings: lit("[llm]\nprovider = \"openai\"\n"),
			want: []wantCheck{{"credentials", statusFail, `no API key for provider "openai"`}}},
		{name: "a key", settings: lit("[llm]\nprovider = \"openai\"\n[llm.openai]\napi_key = \"sk-test-1234567890wxyz\"\n"),
			want: []wantCheck{{"credentials", statusOK, "API key for openai: sk-…wxyz"}}},
		{name: "log level unknown", settings: lit("[log]\nlevel = \"loud\"\n"),
			want: []wantCheck{{"log", statusWarn, "loud"}}},
		{name: "log off", settings: lit("[log]\nlevel = \"off\"\n"),
			want: []wantCheck{{"log", statusOK, "off"}}},
		{name: "log on", settings: lit("[log]\nlevel = \"debug\"\n"),
			want: []wantCheck{{"log", statusOK, "debug level to"}}},
		{name: "telemetry without endpoint", settings: lit("[telemetry]\nenabled = true\n"),
			want: []wantCheck{{"telemetry", statusWarn, "no endpoint set"}}},
		{name: "telemetry with content", settings: lit("[telemetry]\nenabled = true\nendpoint = \"http://127.0.0.1:4318\"\ncapture_content = true\n"),
			want: []wantCheck{{"telemetry", statusOK, "OTLP/HTTP to http://127.0.0.1:4318; including prompts"}}},
		{name: "telemetry from the environment", settings: lit("[telemetry]\nenabled = true\n"), env: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://127.0.0.1:4318"},
			want: []wantCheck{{"telemetry", statusOK, "OTEL_EXPORTER_OTLP_* endpoint; names, timings"}}},
		{name: "fallback and pin that don't build", settings: lit("[llm]\nfallback_models = [\"nowhere/x\"]\n[agent_models]\nqa = \"nowhere/y\"\n"),
			want: []wantCheck{{"fallback 1", statusWarn, "nowhere/x: "}, {"pin qa", statusWarn, "nowhere/y: "}}},
		{name: "online model", online: true, settings: func(t *testing.T) string {
			home := os.Getenv("HOME")
			fakeModel(t, home)
			data, err := os.ReadFile(filepath.Join(home, ".blitz", ".env.toml"))
			require.NoError(t, err)
			return string(data)
		}, want: []wantCheck{{"model", statusOK, "initialised"}, {"model request", statusOK, "model responded"}, {"credentials", statusOK, "ollama needs no API key"}}},
		{name: "online model refusing", online: true, settings: func(t *testing.T) string {
			return fmt.Sprintf("[llm]\nprovider = \"ollama\"\n[llm.openai]\nbase_url = %q\nmodel = \"fake\"\n", failingModel(t))
		}, want: []wantCheck{{"model request", statusFail, ""}}},
		{name: "bad permission rule", settings: lit("[permissions]\nallow = [\"nonsense(\"]\n"),
			want: []wantCheck{{"sandbox", statusFail, "[permissions]"}}},
		{name: "auto approve", settings: lit("[blitz]\nauto_approve = true\n"),
			want: []wantCheck{{"permissions", anyStatus, "bypass"}}}, // OK with the OS sandbox, else a warning
		{name: "rules", settings: lit("[permissions]\nallow = [\"shell(ls)\"]\ndeny = [\"shell(rm)\", \"web(*.internal)\"]\n"),
			want: []wantCheck{{"permission rules", statusOK, "2 deny, "}}},
		{name: "unknown search provider", settings: lit("[web]\nsearch_provider = \"nope\"\n"),
			want: []wantCheck{{"sandbox", statusFail, "unknown web.search_provider"}}},
		{name: "search offline", settings: func(t *testing.T) string {
			return fmt.Sprintf("[web]\nsearch_provider = \"searxng\"\nsearch_url = %q\n", searxng(t, false))
		}, want: []wantCheck{{"web search", statusOK, "searxng (use --online"}}},
		{name: "search online", online: true, settings: func(t *testing.T) string {
			return fmt.Sprintf("[web]\nsearch_provider = \"searxng\"\nsearch_url = %q\nallow_private = true\n", searxng(t, false))
		}, want: []wantCheck{{"web search", statusOK, "searxng: 2 results"}}},
		{name: "search online failing", online: true, settings: func(t *testing.T) string {
			return fmt.Sprintf("[web]\nsearch_provider = \"searxng\"\nsearch_url = %q\nallow_private = true\n", searxng(t, true))
		}, want: []wantCheck{{"web search", statusFail, "searxng: "}}},
		{name: "browser missing", settings: lit("[web]\nenabled = true\n[browser]\nenabled = true\npath = \"/no/such/chrome\"\n"),
			want: []wantCheck{{"browser", statusWarn, "the browser tool can't run"}}},
		{name: "mcp servers", settings: lit("[[mcp.servers]]\nname = \"gone\"\ncommand = \"no-such-mcp-server\"\n\n[[mcp.servers]]\nname = \"web\"\nurl = \"http://127.0.0.1:1/mcp\"\n\n[[mcp.servers]]\nname = \"off\"\nurl = \"http://127.0.0.1:1/mcp\"\ndisabled = true\n"),
			want: []wantCheck{{"mcp gone", statusFail, `command "no-such-mcp-server" not found`}, {"mcp web", statusOK, "configured (use --online to connect)"}, {"mcp off", statusOK, "disabled"}}},
		// Online, a server that can't be reached fails, and a server for
		// another agent is connected to too.
		{name: "mcp online", online: true, settings: func(t *testing.T) string {
			return fmt.Sprintf("[[mcp.servers]]\nname = \"dead\"\nurl = \"http://127.0.0.1:1/mcp\"\n\n[[mcp.servers]]\nname = \"live\"\nurl = %q\n\n[[mcp.servers]]\nname = \"qa-only\"\nurl = %q\nagents = [\"qa\"]\n\n[[mcp.servers]]\nname = \"off\"\nurl = \"http://127.0.0.1:1/mcp\"\ndisabled = true\n", mcpTestServer(t, "echo"), mcpTestServer(t, "qa_echo"))
		}, want: []wantCheck{{"mcp dead", statusFail, "unavailable"}, {"mcp live", statusOK, "1 tools"}, {"mcp qa-only", statusOK, "1 tools"}, {"mcp off", statusOK, "disabled"}}},
		{name: "hooks", settings: lit("[[hooks.stop]]\ncommand = \"no-such-hook-tool --flag\"\n\n[[hooks.stop]]\ncommand = \"true\"\n"),
			want: []wantCheck{{"hook", statusWarn, "no-such-hook-tool not found"}, {"hook", statusOK, "true"}}},
		{name: "open sessions folder, no audit", settings: func(t *testing.T) string {
			dir := filepath.Join(t.TempDir(), "sessions")
			require.NoError(t, os.MkdirAll(dir, 0o755))
			require.NoError(t, os.Chmod(dir, 0o755))
			return fmt.Sprintf("[session]\nstorage_dir = %q\n[audit]\nenabled = false\n", dir)
		}, want: []wantCheck{{"sessions", statusWarn, "accessible by others"}, {"audit log", statusWarn, "disabled"}}},
		{name: "no skills", settings: lit("[skills]\nenabled = false\n"),
			want: []wantCheck{{"config parse", statusOK, ""}}},
		{name: "script sandbox os", settings: lit("[skills.policy]\nsandbox = \"os\"\n"),
			want: []wantCheck{{"skills", statusOK, "loaded"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ws := doctorHome(t, "")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			if tt.settings != nil {
				settings := tt.settings(t)
				require.NoError(t, os.WriteFile(filepath.Join(os.Getenv("HOME"), ".blitz", ".env.toml"), []byte(settings), 0o600))
			}
			checks := runDoctor(context.Background(), &globalFlags{dir: ws}, tt.online)
			assertChecks(t, checks, tt.want)
		})
	}
}

func lit(s string) func(*testing.T) string { return func(*testing.T) string { return s } }

// The settings file's own problems: readable by others, or not parsing
// (which ends the checks).
func TestDoctorSettingsFile(t *testing.T) {
	home, ws := doctorHome(t, "[blitz]\n")
	path := filepath.Join(home, ".blitz", ".env.toml")
	require.NoError(t, os.Chmod(path, 0o644))
	assertChecks(t, runDoctor(context.Background(), &globalFlags{dir: ws}, false), []wantCheck{{"config file", statusWarn, "readable by others"}})

	require.NoError(t, os.WriteFile(path, []byte("[llm\n"), 0o600))
	checks := runDoctor(context.Background(), &globalFlags{dir: ws}, false)
	assertChecks(t, checks, []wantCheck{{"config parse", statusFail, ""}})
	assert.Len(t, checks, 2, "nothing is checked past settings that don't parse")
}

// Without bash on PATH, doctor fails; without git, it warns.
func TestDoctorNeedsBash(t *testing.T) {
	_, ws := doctorHome(t, "")
	t.Setenv("PATH", t.TempDir())
	assertChecks(t, runDoctor(context.Background(), &globalFlags{dir: ws}, false), []wantCheck{
		{"bash", statusFail, "not found on PATH"}, {"git", statusWarn, "not found on PATH"},
	})
}

// Installed plugins: enabled, disabled, and changed since installed.
func TestDoctorPlugins(t *testing.T) {
	_, ws := doctorHome(t, "")
	src := pluginDir(t)
	_, err := runCLI(t, "plugin", "install", "--yes", src)
	require.NoError(t, err)
	assertChecks(t, runDoctor(context.Background(), &globalFlags{dir: ws}, false), []wantCheck{{"plugin kit", statusOK, "1.0.0"}})
	_, err = runCLI(t, "plugin", "disable", "kit")
	require.NoError(t, err)
	assertChecks(t, runDoctor(context.Background(), &globalFlags{dir: ws}, false), []wantCheck{{"plugin kit", statusOK, "1.0.0, disabled"}})
	home := os.Getenv("HOME")
	var changed bool
	filepath.WalkDir(filepath.Join(home, ".blitz"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !changed && d.Name() == "hi.md" {
			changed = os.WriteFile(p, []byte("changed"), 0o644) == nil
		}
		return nil
	})
	require.True(t, changed, "the installed plugin's command wasn't found")
	assertChecks(t, runDoctor(context.Background(), &globalFlags{dir: ws}, false), []wantCheck{{"plugin kit", statusWarn, "its files changed"}})
	out, _ := runCLI(t, "plugin", "list")
	assert.Contains(t, out, "changed since installed: not loaded")
}

// The workspace's memory files and rules, and its project settings as
// they wait for trust, are trusted or declined.
func TestDoctorWorkspace(t *testing.T) {
	doctorHome(t, "")
	ws := projectDir(t)
	require.NoError(t, os.WriteFile(filepath.Join(ws, "BLITZ.md"), []byte("Use tabs."), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(ws, ".blitz", "rules"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ws, ".blitz", "rules", "go.md"), []byte("---\npaths: [\"**/*.go\"]\n---\nGo rules"), 0o644))
	// A project may not set credentials, nor what isn't a setting.
	settings := filepath.Join(ws, ".blitz", "settings.toml")
	data, err := os.ReadFile(settings)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(settings, append(data, "\n[llm.openai]\napi_key = \"sk-x\"\n[nonsense]\nkey = 1\n"...), 0o644))
	g := &globalFlags{dir: ws}
	assertChecks(t, runDoctor(context.Background(), g, false), []wantCheck{
		{"project memory", statusOK, "BLITZ.md (+1 path-scoped rules)"},
		{"project settings", statusWarn, "new, not loaded"},
		{"project setting", statusWarn, "llm.openai.api_key ignored"},
		{"project setting", statusWarn, "nonsense.key ignored"},
	})

	cfg := mustConfig(t, ws)
	p, err := engine.ReviewProject(cfg)
	require.NoError(t, err)
	require.NoError(t, engine.TrustProject(cfg, p.Hash, false))
	assertChecks(t, runDoctor(context.Background(), g, false), []wantCheck{{"project settings", statusOK, "declined"}})
	require.NoError(t, engine.TrustProject(cfg, p.Hash, true))
	assertChecks(t, runDoctor(context.Background(), g, false), []wantCheck{{"project settings", statusOK, "trusted"}})
}

// blitz doctor prints the checks and fails when one does.
func TestDoctorCommand(t *testing.T) {
	doctorHome(t, "[llm]\nprovider = \"openai\"\n")
	out, err := runCLI(t, "doctor")
	assert.Equal(t, exitFailure, exitCodeFor(err), "no key: %v", err)
	assert.Contains(t, out, "✗")

	home, _ := doctorHome(t, "")
	fakeModel(t, home)
	out, err = runCLI(t, "doctor", "--online")
	assert.NoError(t, err, out)
	assert.Contains(t, out, "model responded")
}
