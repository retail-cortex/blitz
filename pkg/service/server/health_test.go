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

package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/config/configtest"
	"github.com/retail-cortex/blitz/pkg/engine"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The dashboard over the API: it's served and streamed with the
// workspace's items; a check is estimated, streams each item (here the
// judge has nothing to say) and ends with a summary; one check at a time;
// stopping without one is fine.
func TestWorkspaceHealthOverTheAPI(t *testing.T) {
	watchHealthEvery = 20 * time.Millisecond
	t.Cleanup(func() { watchHealthEvery = 5 * time.Second })
	c, _ := serve(t, configtest.RunTools)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".agents", "agents"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".agents", "agents", "writer.md"), []byte("---\nname: writer\ndescription: Writes\ntools: [read_file]\n---\nWrite.\n"), 0o644))

	got, err := c.workspaces.GetWorkspaceHealth(ctx, connect.NewRequest(&pb.GetWorkspaceHealthRequest{Workspace: dir}))
	require.NoError(t, err)
	h := got.Msg.Health
	var writer *pb.HealthItem
	for _, it := range h.Items {
		if it.Kind == "agent" && it.Name == "writer" {
			writer = it
		}
	}
	require.NotNil(t, writer)
	assert.Equal(t, []string{"uncovered"}, writer.Flags)
	assert.Len(t, writer.Daily, 30)
	assert.Len(t, h.Days, 30)
	assert.Equal(t, "every day at 02:00", h.Schedule)
	assert.Nil(t, h.Next, "no first check yet")
	assert.Empty(t, h.ReflectSchedule, "the reflection's schedule is off by default")
	assert.InDelta(t, 0.5, h.ReflectMaxCostUsd, 1e-9)
	assert.False(t, h.Checking)

	watch, err := c.workspaces.WatchWorkspaceHealth(ctx, connect.NewRequest(&pb.WatchWorkspaceHealthRequest{Workspace: dir}))
	require.NoError(t, err)
	require.True(t, watch.Receive(), "%v", watch.Err())
	assert.Len(t, watch.Msg().Health.Items, len(h.Items))
	watch.Close()

	only := &pb.HealthCheckScope{Items: []string{"agent/writer"}}
	est, err := c.workspaces.EstimateHealthCheck(ctx, connect.NewRequest(&pb.EstimateHealthCheckRequest{Workspace: dir, Scope: only}))
	require.NoError(t, err)
	assert.Equal(t, int32(1), est.Msg.Items)

	stream, err := c.workspaces.CheckWorkspaceHealth(ctx, connect.NewRequest(&pb.CheckWorkspaceHealthRequest{Workspace: dir, Scope: only}))
	require.NoError(t, err)
	var msgs []*pb.CheckWorkspaceHealthResponse
	for stream.Receive() {
		msgs = append(msgs, stream.Msg())
	}
	require.NoError(t, stream.Err())
	require.Len(t, msgs, 2)
	assert.Equal(t, "agent/writer", msgs[0].Item)
	assert.NotEmpty(t, msgs[0].Error, "the judge had no answer")
	assert.True(t, msgs[1].Done)
	assert.Zero(t, msgs[1].Judged)

	got, err = c.workspaces.GetWorkspaceHealth(ctx, connect.NewRequest(&pb.GetWorkspaceHealthRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.NotNil(t, got.Msg.Health.LastCheck)
	assert.NotNil(t, got.Msg.Health.Next, "the schedule runs once there's been a check")

	// Started in the service, it goes on alone; the dashboard says it's
	// going, then how it ended.
	_, err = c.workspaces.StartHealthCheck(ctx, connect.NewRequest(&pb.StartHealthCheckRequest{Workspace: dir, Scope: &pb.HealthCheckScope{Items: []string{"agent/writer"}, Force: true}}))
	require.NoError(t, err)
	for {
		got, err = c.workspaces.GetWorkspaceHealth(ctx, connect.NewRequest(&pb.GetWorkspaceHealthRequest{Workspace: dir}))
		require.NoError(t, err)
		if !got.Msg.Health.Checking {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	assert.Zero(t, got.Msg.Health.LastJudged)
	require.Len(t, got.Msg.Health.LastErrors, 1, "the judge had no answer")
	assert.Contains(t, got.Msg.Health.LastErrors[0], "agent/writer")

	healthChecks.Store(dir, context.CancelFunc(func() {}))
	healthProgress.Store(dir, engine.HealthProgress{Item: "agent/writer", Index: 1, Total: 2})
	got, err = c.workspaces.GetWorkspaceHealth(ctx, connect.NewRequest(&pb.GetWorkspaceHealthRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.True(t, got.Msg.Health.Checking)
	require.NotNil(t, got.Msg.Health.Progress)
	assert.Equal(t, "agent/writer", got.Msg.Health.Progress.Item)
	_, err = c.workspaces.StartHealthCheck(ctx, connect.NewRequest(&pb.StartHealthCheckRequest{Workspace: dir}))
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "one at a time")
	healthProgress.Delete(dir)
	busy, err := c.workspaces.CheckWorkspaceHealth(ctx, connect.NewRequest(&pb.CheckWorkspaceHealthRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.False(t, busy.Receive())
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(busy.Err()))
	_, err = c.workspaces.StopHealthCheck(ctx, connect.NewRequest(&pb.StopHealthCheckRequest{Workspace: dir}))
	assert.NoError(t, err)
	healthChecks.Delete(dir)
	_, err = c.workspaces.StopHealthCheck(ctx, connect.NewRequest(&pb.StopHealthCheckRequest{Workspace: dir}))
	assert.NoError(t, err, "nothing to stop")

	for _, call := range []func() error{
		func() error {
			_, err := c.workspaces.GetWorkspaceHealth(ctx, connect.NewRequest(&pb.GetWorkspaceHealthRequest{Workspace: "relative"}))
			return err
		},
		func() error {
			_, err := c.workspaces.EstimateHealthCheck(ctx, connect.NewRequest(&pb.EstimateHealthCheckRequest{Workspace: "relative"}))
			return err
		},
		func() error {
			_, err := c.workspaces.StopHealthCheck(ctx, connect.NewRequest(&pb.StopHealthCheckRequest{Workspace: "relative"}))
			return err
		},
	} {
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(call()))
	}
}
