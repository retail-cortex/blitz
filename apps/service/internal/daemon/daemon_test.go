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

package daemon

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/socket"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The service answers over its socket, opens workspaces on demand,
// refuses a second service on the socket, and removes the socket when
// stopped.
func TestRun(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	for _, k := range []string{"GEMINI_API_KEY", "GOOGLE_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "LLM_PROVIDER"} {
		t.Setenv(k, "") // no real model
	}
	t.Chdir(t.TempDir())
	dir, err := os.MkdirTemp("/tmp", "bd") // socket paths must be short
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, Options{Socket: sock}) }()
	for deadline := time.Now().Add(10 * time.Second); !socket.Running(sock); time.Sleep(20 * time.Millisecond) {
		require.False(t, time.Now().After(deadline), "service didn't start")
	}

	c := pb.NewWorkspaceServiceClient(socket.Client(sock), socket.BaseURL)
	agents, err := c.ListAgents(context.Background(), connect.NewRequest(&pb.ListAgentsRequest{Workspace: t.TempDir()}))
	require.NoError(t, err, "list agents: %v", agents)
	require.NotEqual(t, 0, len(agents.Msg.Agents), "list agents: %v %v", agents, err)
	err = Run(context.Background(), Options{Socket: sock})
	assert.ErrorIs(t, err, socket.ErrRunning, "second service: %v", err)

	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err, "serve")
	case <-time.After(15 * time.Second):
		t.Fatal("service didn't stop")
	}
	_, err = os.Stat(sock)
	assert.ErrorIs(t, err, fs.ErrNotExist, "socket left behind")
}

// modelServer is an OpenAI-compatible server that answers "done" and
// records the model each request names.
type modelServer struct {
	*httptest.Server
	mu     sync.Mutex
	models []string
}

func newModelServer(t *testing.T) *modelServer {
	m := &modelServer{}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		m.mu.Lock()
		m.models = append(m.models, req.Model)
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"resp_1","model":"fake","output":[{"type":"message","content":[{"type":"output_text","text":"done"}]}],"usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":1,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":2}}`)
	}))
	t.Cleanup(m.Close)
	return m
}

func (m *modelServer) seen() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.models...)
}

// Each workspace the service opens keeps its own configuration, the
// global settings with its own laid over them, however the client
// switches between them: its model and provider endpoint, and its
// permission rules. A global change reaches every open workspace; a
// workspace's change reaches only it.
func TestWorkspacesKeepTheirOwnSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MODENV_PREFIX", "")
	for _, k := range []string{"GEMINI_API_KEY", "GOOGLE_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "LLM_PROVIDER", "OPENAI_BASE_URL"} {
		t.Setenv(k, "")
	}
	t.Chdir(t.TempDir())
	serverA, serverB := newModelServer(t), newModelServer(t)
	one, two := t.TempDir(), t.TempDir()
	write := func(dir, text string) {
		require.NoError(t, os.MkdirAll(dir, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".env.toml"), []byte(text), 0o600))
	}
	write(filepath.Join(home, ".blitz"), "[blitz]\ndefault_model = \"global-model\"\n\n[llm]\nprovider = \"ollama\"\n\n[llm.openai]\nbase_url = \""+serverA.URL+"/v1\"\n\n[permissions]\nread_only_defaults = false\ndeny = [\"shell(rm)\"]\n")
	write(config.WorkspaceSettingsDir("", two), "[blitz]\ndefault_model = \"b-model\"\n\n[llm.openai]\nbase_url = \""+serverB.URL+"/v1\"\n\n[permissions]\nallow = [\"shell(make)\"]\n")

	dir, err := os.MkdirTemp("/tmp", "bd") // socket paths must be short
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, Options{Socket: sock}) }()
	t.Cleanup(func() { cancel(); <-done })
	for deadline := time.Now().Add(10 * time.Second); !socket.Running(sock); time.Sleep(20 * time.Millisecond) {
		require.False(t, time.Now().After(deadline), "service didn't start")
	}
	hc := socket.Client(sock)
	workspaces := pb.NewWorkspaceServiceClient(hc, socket.BaseURL)
	sessions := pb.NewSessionServiceClient(hc, socket.BaseURL)
	settings := pb.NewConfigServiceClient(hc, socket.BaseURL)
	bg := context.Background()

	model := func(ws string) string {
		res, err := workspaces.GetModel(bg, connect.NewRequest(&pb.GetModelRequest{Workspace: ws}))
		require.NoError(t, err)
		require.Empty(t, res.Msg.Unavailable)
		return res.Msg.Name
	}
	rules := func(ws string) map[string]string {
		res, err := workspaces.ListPermissionRules(bg, connect.NewRequest(&pb.ListPermissionRulesRequest{Workspace: ws}))
		require.NoError(t, err)
		out := map[string]string{}
		for _, r := range res.Msg.Rules {
			out[r.Effect+" "+r.Rule] = r.Source
		}
		return out
	}
	turn := func(ws string) {
		t.Helper()
		sess, err := sessions.NewSession(bg, connect.NewRequest(&pb.NewSessionRequest{Workspace: ws}))
		require.NoError(t, err)
		stream, err := sessions.RunTurn(bg, connect.NewRequest(&pb.RunTurnRequest{Workspace: ws, SessionId: sess.Msg.Session.Id, Turn: &pb.Turn{Text: "hi"}}))
		require.NoError(t, err)
		output := ""
		for stream.Receive() {
			if f := stream.Msg().GetEvent().GetFinished(); f != nil {
				output = f.Output
				require.Nil(t, f.Error, "turn in %s: %v", ws, f.Error)
			}
		}
		require.NoError(t, stream.Err())
		assert.Equal(t, "done", output, "turn in %s", ws)
	}

	// Each has its own model and rules.
	assert.Equal(t, "global-model", model(one))
	assert.Equal(t, "b-model", model(two))
	assert.Equal(t, map[string]string{"deny shell(rm)": "global"}, rules(one))
	assert.Equal(t, map[string]string{"deny shell(rm)": "global", "allow shell(make)": "workspace"}, rules(two))

	// Switching back and forth, each turn goes to its own provider.
	turn(one)
	turn(two)
	turn(one)
	assert.Equal(t, []string{"global-model", "global-model"}, serverA.seen(), "server A")
	assert.Equal(t, []string{"b-model"}, serverB.seen(), "server B")

	// A global change reaches both; the workspace's own settings still win.
	_, err = settings.AddPermission(bg, connect.NewRequest(&pb.AddPermissionRequest{Effect: "ask", Rule: "shell(git push)"}))
	require.NoError(t, err)
	_, err = settings.SetConfigValue(bg, connect.NewRequest(&pb.SetConfigValueRequest{Key: "blitz.default_model", Value: "global-model-2"}))
	require.NoError(t, err)
	assert.Equal(t, "global", rules(one)["ask shell(git push)"])
	assert.Equal(t, "global", rules(two)["ask shell(git push)"])
	assert.Equal(t, "global-model-2", model(one))
	assert.Equal(t, "b-model", model(two), "the workspace's own model")

	// A workspace's change reaches only it.
	_, err = settings.AddPermission(bg, connect.NewRequest(&pb.AddPermissionRequest{Workspace: two, Effect: "deny", Rule: "shell(curl)"}))
	require.NoError(t, err)
	_, err = settings.SetConfigValue(bg, connect.NewRequest(&pb.SetConfigValueRequest{Workspace: two, Key: "blitz.default_model", Value: "b-model-2"}))
	require.NoError(t, err)
	assert.Equal(t, "workspace", rules(two)["deny shell(curl)"])
	assert.NotContains(t, rules(one), "deny shell(curl)")
	assert.Equal(t, "b-model-2", model(two))
	assert.Equal(t, "global-model-2", model(one))
	turn(one)
	turn(two)
	assert.Equal(t, []string{"global-model", "global-model", "global-model-2"}, serverA.seen(), "server A")
	assert.Equal(t, []string{"b-model", "b-model-2"}, serverB.seen(), "server B")

	// Sessions share a folder but each workspace lists only its own.
	count := func(ws string) int {
		res, err := sessions.ListSessions(bg, connect.NewRequest(&pb.ListSessionsRequest{Workspace: ws}))
		require.NoError(t, err)
		return len(res.Msg.Sessions)
	}
	assert.Equal(t, 3, count(one))
	assert.Equal(t, 2, count(two))
}
