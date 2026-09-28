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
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
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
