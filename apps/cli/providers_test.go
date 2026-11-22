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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/client"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/secrets"
)

// runBlitzIn runs the CLI with stdin in.
func runBlitzIn(t *testing.T, in string, args ...string) (string, error) {
	t.Helper()
	root := newRootCommand()
	var out bytes.Buffer
	root.SetIn(strings.NewReader(in))
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

// fakeProviders answers the key check and the listing, never a provider.
func fakeProviders(t *testing.T, works bool) *[]string {
	t.Helper()
	secrets.SetDefault(&secrets.Memory{}) // never the machine's keychain
	var keys []string
	oldCheck, oldList := checkKey, listProvider
	t.Cleanup(func() { checkKey, listProvider = oldCheck, oldList })
	checkKey = func(_ context.Context, provider, key string) (client.KeyCheck, error) {
		keys = append(keys, provider+":"+key)
		if !works {
			return client.KeyCheck{Error: "API key not valid"}, nil
		}
		return client.KeyCheck{Works: true, Models: []string{"m1", "m2"}}, nil
	}
	listProvider = func(_ context.Context, _, provider string) (client.ProviderModels, error) {
		if !works {
			return client.ProviderModels{Provider: provider, Err: "401 unauthorized"}, nil
		}
		return client.ProviderModels{Provider: provider, Models: []client.ListedModel{{ID: "m1"}}}, nil
	}
	return &keys
}

// blitz providers add checks the key before it's saved, and makes the
// provider the one in use; use, check, remove and list (CLP-41).
func TestProviders(t *testing.T) {
	keys := fakeProviders(t, true)
	cfg := t.TempDir()
	out, err := runBlitzIn(t, "AIza-good\n", "-c", cfg, "providers", "add", "gemini", "--model", "gemini-flash")
	require.NoError(t, err, out)
	assert.Equal(t, []string{"gemini:AIza-good"}, *keys, "checked before it's saved")
	assert.Contains(t, out, "The key works: it reaches 2 models.")
	c, err := config.Load(cfg)
	require.NoError(t, err)
	assert.Equal(t, "gemini", c.LLM.Provider)

	out, err = runBlitz(t, "-c", cfg, "providers", "list", "--json")
	require.NoError(t, err)
	var info config.ScopeInfo
	require.NoError(t, json.Unmarshal([]byte(out), &info))
	assert.Equal(t, "gemini", info.Provider)
	out, err = runBlitz(t, "-c", cfg, "providers", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "* gemini", "the one in use is marked")
	assert.NotContains(t, out, "AIza-good", "never the key")

	out, err = runBlitz(t, "-c", cfg, "providers", "check", "gemini")
	require.NoError(t, err)
	assert.Contains(t, out, "gemini answers: 1 model.")

	_, err = runBlitz(t, "-c", cfg, "providers", "use", "ollama", "qwen3")
	require.NoError(t, err)
	c, _ = config.Load(cfg)
	assert.Equal(t, "ollama", c.LLM.Provider)
	assert.Equal(t, "ollama/qwen3", c.Blitz.DefaultModel)

	_, err = runBlitz(t, "-c", cfg, "providers", "remove", "gemini")
	require.NoError(t, err, "a script removes without being asked")

	_, err = runBlitz(t, "-c", cfg, "providers", "add", "nope")
	assert.Equal(t, exitUsage, exitCodeFor(err))
}

func TestProvidersRefuseABadKey(t *testing.T) {
	fakeProviders(t, false)
	cfg := t.TempDir()
	_, err := runBlitzIn(t, "bad\n", "-c", cfg, "providers", "add", "anthropic")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "didn't take the key (API key not valid): nothing was saved")
	_, statErr := os.Stat(filepath.Join(cfg, ".env.toml"))
	assert.True(t, errors.Is(statErr, os.ErrNotExist), "nothing written")
	_, err = runBlitzIn(t, "\n", "-c", cfg, "providers", "add", "anthropic")
	assert.ErrorContains(t, err, "no key on stdin")
	_, err = runBlitz(t, "-c", cfg, "providers", "check", "anthropic")
	assert.ErrorContains(t, err, "401 unauthorized")
	assert.Equal(t, exitFailure, exitCodeFor(err))
}

// asTerminal makes stdin count as a terminal, so removes ask.
func asTerminal(t *testing.T) {
	t.Helper()
	old := stdinIsTerminal
	stdinIsTerminal = func(*cobra.Command) bool { return true }
	t.Cleanup(func() { stdinIsTerminal = old })
}

// A remove asks on a terminal; no keeps it, yes or -y removes it; rm and
// delete name it too (CLP-52).
func TestRemovesAsk(t *testing.T) {
	cfg := t.TempDir()
	write := func() {
		require.NoError(t, os.WriteFile(filepath.Join(cfg, ".env.toml"), []byte("[[mcp.servers]]\nname = \"gh\"\nurl = \"https://example.com/mcp\"\n"), 0o600))
	}
	servers := func() int {
		list, err := mcpServers(&globalFlags{config: cfg}, "")
		require.NoError(t, err)
		return len(list)
	}
	asTerminal(t)
	write()
	out, err := runBlitzIn(t, "n\n", "-c", cfg, "mcp", "remove", "gh")
	require.Error(t, err)
	assert.Contains(t, out, "Remove the MCP server gh? [y/N]")
	assert.ErrorIs(t, err, errNotRemoved)
	assert.Equal(t, 1, servers(), "no keeps it")
	_, err = runBlitzIn(t, "y\n", "-c", cfg, "mcp", "rm", "gh")
	require.NoError(t, err)
	assert.Equal(t, 0, servers())
	write()
	out, err = runBlitzIn(t, "", "-c", cfg, "mcp", "delete", "gh", "-y")
	require.NoError(t, err)
	assert.NotContains(t, out, "[y/N]", "-y doesn't ask")
	assert.Equal(t, 0, servers())
}

// blitz service logs reads the log files, so it works without the service
// (CLP-46): the newest day oldest first, filtered, and as JSON.
func TestServiceLogs(t *testing.T) {
	cfg, logs := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cfg, ".env.toml"), []byte("[log]\nlevel = \"info\"\ndir = \""+logs+"\"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(logs, "blitz-2026-11-21.jsonl"), []byte(`{"time":"2026-11-21T10:00:00Z","level":"INFO","msg":"older day"}`+"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(logs, "blitz-2026-11-22.jsonl"), []byte(
		`{"time":"2026-11-22T10:00:01Z","level":"INFO","msg":"workspace opened","workspace":"/w"}`+"\n"+
			`{"time":"2026-11-22T10:00:02Z","level":"WARN","msg":"mcp server failed","server":"gh"}`+"\n"), 0o600))

	out, err := runBlitz(t, "-c", cfg, "service", "logs")
	require.NoError(t, err)
	assert.Less(t, strings.Index(out, "workspace opened"), strings.Index(out, "mcp server failed"), "oldest first")
	assert.Contains(t, out, "server=gh")
	assert.NotContains(t, out, "older day", "the newest day")
	out, err = runBlitz(t, "-c", cfg, "service", "logs", "--level", "warn")
	require.NoError(t, err)
	assert.NotContains(t, out, "workspace opened")
	out, err = runBlitz(t, "-c", cfg, "service", "logs", "--day", "2026-11-21")
	require.NoError(t, err)
	assert.Contains(t, out, "older day")
	out, err = runBlitz(t, "-c", cfg, "service", "logs", "--days")
	require.NoError(t, err)
	assert.Equal(t, "2026-11-22\n2026-11-21\n", out)
	out, err = runBlitz(t, "-c", cfg, "service", "logs", "--grep", "nothing-like-it")
	require.NoError(t, err)
	assert.Contains(t, out, "No records match")

	off := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(off, ".env.toml"), []byte("[log]\nlevel = \"off\"\n"), 0o600))
	_, err = runBlitz(t, "-c", off, "service", "logs")
	assert.ErrorContains(t, err, "keeps no log")
}

// -f prints the records written after it started, until it's stopped.
func TestServiceLogsFollow(t *testing.T) {
	cfg, logs := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cfg, ".env.toml"), []byte("[log]\nlevel = \"info\"\ndir = \""+logs+"\"\n"), 0o600))
	file := filepath.Join(logs, "blitz-2026-11-22.jsonl")
	require.NoError(t, os.WriteFile(file, []byte(`{"time":"2026-11-22T10:00:01Z","level":"INFO","msg":"first"}`+"\n"), 0o600))
	old := logFollowEvery
	logFollowEvery = 10 * time.Millisecond
	t.Cleanup(func() { logFollowEvery = old })
	ctx, cancel := context.WithCancel(context.Background())
	root := newRootCommand()
	var out syncBuffer
	root.SetOut(&out)
	root.SetArgs([]string{"-c", cfg, "service", "logs", "-f"})
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()
	require.Eventually(t, func() bool { return strings.Contains(out.String(), "first") }, 5*time.Second, 10*time.Millisecond)
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString(`{"time":"2026-11-22T10:00:02Z","level":"ERROR","msg":"second"}` + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.Eventually(t, func() bool { return strings.Contains(out.String(), "second") }, 5*time.Second, 10*time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	assert.Equal(t, 1, strings.Count(out.String(), "first"), "printed once")
}

// Signing in another way reads no key; --keep-default leaves the provider
// in use; -w is the workspace's own; --no-check doesn't ask the provider.
func TestProvidersAddOptions(t *testing.T) {
	keys := fakeProviders(t, true)
	cfg, ws := t.TempDir(), t.TempDir()
	_, err := runBlitzIn(t, "", "-c", cfg, "providers", "add", "gemini", "--auth", "adc", "--project", "p1", "--location", "us-central1")
	require.NoError(t, err)
	assert.Empty(t, *keys, "no key read or checked")
	c, err := config.Load(cfg)
	require.NoError(t, err)
	assert.Equal(t, config.AuthADC, c.LLM.Gemini.Auth)
	assert.Equal(t, "p1", c.LLM.Gemini.ProjectID)

	_, err = runBlitzIn(t, "sk-ant\n", "-c", cfg, "providers", "add", "anthropic", "--keep-default", "--no-check")
	require.NoError(t, err)
	assert.Empty(t, *keys, "--no-check: not asked")
	c, _ = config.Load(cfg)
	assert.Equal(t, "gemini", c.LLM.Provider, "still the one in use")

	_, err = runBlitz(t, "-c", cfg, "-d", ws, "providers", "use", "-w", "anthropic")
	require.NoError(t, err)
	wc, err := config.LoadWorkspace(cfg, ws)
	require.NoError(t, err)
	assert.Equal(t, "anthropic", wc.LLM.Provider, "this workspace's own")
	c, _ = config.Load(cfg)
	assert.Equal(t, "gemini", c.LLM.Provider, "the global one stays")

	assert.Contains(t, shellComplete(t, "providers", "add", ""), "anthropic")
}

// blitz jobs remove removes a job through the service; a script needn't
// say yes (CLP-34, CLP-52).
func TestJobsRemove(t *testing.T) {
	isolate(t)
	ws := t.TempDir()
	addWorker(t, ws, "hello", "---\nschedule: daily at noon\n---\nSay hello.\n")
	out, err := runCLI(t, "-d", ws, "jobs", "remove", "hello")
	require.NoError(t, err, out)
	assert.Contains(t, out, "Removed the job hello.")
	assert.NoDirExists(t, filepath.Join(ws, "workers", "hello"))
	_, err = runCLI(t, "-d", ws, "jobs", "rm", "hello")
	assert.Error(t, err, "gone")
}

// The log as JSON, and a note when -n cuts it.
func TestServiceLogsJSONAndLimit(t *testing.T) {
	cfg, logs := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cfg, ".env.toml"), []byte("[log]\nlevel = \"info\"\ndir = \""+logs+"\"\n"), 0o600))
	var lines strings.Builder
	for i := range 5 {
		lines.WriteString(`{"time":"2026-11-22T10:00:0` + string(rune('0'+i)) + `Z","level":"INFO","msg":"tick"}` + "\n")
	}
	require.NoError(t, os.WriteFile(filepath.Join(logs, "blitz-2026-11-22.jsonl"), []byte(lines.String()), 0o600))
	out, err := runBlitz(t, "-c", cfg, "service", "logs", "-n", "2")
	require.NoError(t, err)
	assert.Equal(t, 2, strings.Count(out, "tick"))
	assert.Contains(t, out, "Showing the newest 2 of 5")
	out, err = runBlitz(t, "-c", cfg, "service", "logs", "--json")
	require.NoError(t, err)
	var page map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &page))
	assert.EqualValues(t, 5, page["Matched"])
	out, err = runBlitz(t, "-c", cfg, "service", "logs", "--days", "--json")
	require.NoError(t, err)
	assert.JSONEq(t, `["2026-11-22"]`, out)
	_, err = runBlitz(t, "-c", cfg, "service", "logs", "--day", "not-a-day")
	assert.Equal(t, exitUsage, exitCodeFor(err))
}
