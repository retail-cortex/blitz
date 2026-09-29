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

package client

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A client accounts for the background processes its own turns started in
// the service, and no one else's.
func TestRemoteBackgroundProcesses(t *testing.T) {
	start := call("run_shell_command", map[string]any{"command": "sleep 30", "background": true})
	r := attach(t, func(c *config.Config) { c.Blitz.AutoApprove = true }, start, text("started"))
	pm := r.Processes()
	require.NotNil(t, pm)
	assert.Empty(t, pm.Running(), "no turn yet")

	sess, _, err := r.OpenSession("", false)
	require.NoError(t, err)
	_, err = r.Run(context.Background(), sess.ID, api.Turn{Text: "start it"}, func(api.Event) {})
	require.NoError(t, err)
	running := pm.Running()
	require.Len(t, running, 1)
	assert.Equal(t, "sleep 30", running[0].Command)

	// Another client of the same workspace, which ran no turn there.
	other := &Remote{dir: r.dir, sessions: r.sessions, workspaces: r.workspaces, warn: func(string) {}}
	assert.Empty(t, other.Processes().Running(), "another client's process listed")
	other.ranIn("session-20260101-000000-00000000")
	_, err = other.workspaces.KillProcess(context.Background(), killRequest(other, running[0].ID))
	assert.ErrorIs(t, fromAPI(err), api.ErrUnknownProcess, "another client's process killed: %v", err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, pm.WaitAll(ctx), context.Canceled, "waiting stops with the context")

	pm.Shutdown()
	assert.Empty(t, pm.Running(), "still running after Shutdown")
	assert.NoError(t, pm.WaitAll(context.Background()))
}

// !cmd run by an attached CLI lands in the service workspace's audit log.
func TestRemoteAuditShell(t *testing.T) {
	dir := t.TempDir()
	r := attach(t, func(c *config.Config) { c.Audit.Enabled, c.Audit.Dir = true, dir })
	r.AuditShell("ls -la", 0, nil)
	r.AuditShell("nosuchcmd", -1, errors.New("not found"))

	files, err := filepath.Glob(filepath.Join(dir, "audit-*.jsonl"))
	require.NoError(t, err)
	require.Len(t, files, 1)
	data, err := os.ReadFile(files[0])
	require.NoError(t, err)
	var shell []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, `"user_shell"`) {
			shell = append(shell, line)
		}
	}
	require.Len(t, shell, 2, "audit log:\n%s", data)
	assert.Contains(t, shell[0], "ls -la")
	assert.Contains(t, shell[1], "not found")
}

func killRequest(r *Remote, id int) *connect.Request[pb.KillProcessRequest] {
	return connect.NewRequest(&pb.KillProcessRequest{Workspace: r.dir, SessionIds: r.sessionsRan(), Id: int32(id)})
}
