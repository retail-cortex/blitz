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
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackgroundRuns(t *testing.T) {
	create := call("create_file", map[string]any{"path": "made.txt", "content": "hi\n"})
	r, url := attachURL(t, func(c *config.Config) { c.Blitz.AutoApprove, c.Blitz.PermissionMode = false, "default" }, create, text("made it"))
	b := AttachBackgroundHTTP(http.DefaultClient, url)
	ctx := context.Background()

	run, err := r.StartBackground(ctx, api.Turn{Text: "make a file", Timeout: time.Minute})
	require.NoError(t, err)
	assert.Equal(t, "bg-1", run.ID)
	assert.True(t, run.Active())

	require.Eventually(t, func() bool {
		got, err := b.Find(ctx, run.ID)
		return err == nil && got.State == "waiting" && got.Waiting == 1
	}, 10*time.Second, 10*time.Millisecond)

	// Logs show it so far, without answering.
	_, ended, err := b.Logs(ctx, run.ID, false, func(api.Event) {})
	require.NoError(t, err)
	assert.False(t, ended)

	// Following answers its request and shows it to the end.
	asked := 0
	r.SetUI(func(context.Context, api.ApprovalRequest) (api.Decision, error) {
		asked++
		return api.DecisionOnce, nil
	}, nil)
	var texts []string
	_, err = r.FollowBackground(ctx, run.ID, func(e api.Event) {
		if e.Text != nil && !e.Text.Partial {
			texts = append(texts, e.Text.Text)
		}
	})
	require.NoError(t, err)
	assert.Equal(t, 1, asked)
	assert.Contains(t, texts, "made it")

	// Logs of an ended run end with it.
	_, ended, err = b.Logs(ctx, run.ID, true, func(api.Event) {})
	require.NoError(t, err)
	assert.True(t, ended)
	list, err := b.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "done", list[0].State)
	assert.False(t, list[0].Ended.IsZero())

	_, err = b.Find(ctx, "bg-9")
	assert.ErrorIs(t, err, api.ErrUnknownRun)
	_, err = b.Stop(ctx, "bg-9")
	assert.ErrorIs(t, err, api.ErrUnknownRun)
	_, _, err = b.Logs(ctx, "bg-9", false, func(api.Event) {})
	assert.ErrorIs(t, err, api.ErrUnknownRun)
	_, err = r.FollowBackground(ctx, "bg-9", func(api.Event) {})
	assert.ErrorIs(t, err, api.ErrUnknownRun)

	stopped, err := b.Stop(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, "done", stopped.State, "stopping an ended run leaves it as it ended")
}

func TestUndoWorkerRunOverTheService(t *testing.T) {
	r, url := attachURL(t, nil)
	_, err := AttachWorkersHTTP(http.DefaultClient, url, r.Dir()).UndoWorkerRun("run-x", false)
	assert.ErrorIs(t, err, api.ErrNothingToUndo)
}

// A settings file edited by anyone reaches watching clients, so their views
// of the rules refresh (the engine has applied it already).
func TestSettingsChangedReachesWatchers(t *testing.T) {
	r, _ := attachURL(t, nil)
	sess, _, err := r.OpenSession("", false)
	require.NoError(t, err)
	ctx, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	stream, err := r.workspaces.WatchTasks(ctx, connect.NewRequest(&pb.WatchTasksRequest{Workspace: r.dir, SessionIds: []string{sess.ID}}))
	require.NoError(t, err)
	defer stream.Close()
	require.True(t, stream.Receive(), "no ready: %v", stream.Err())
	require.True(t, stream.Msg().Event.GetReady())

	file := filepath.Join(config.ConfigDir(""), ".env.toml")
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o700))
	require.NoError(t, os.WriteFile(file, []byte("[permissions]\ndeny = [\"write(secret/**)\"]\n"), 0o600))
	require.True(t, stream.Receive(), "no event: %v", stream.Err())
	assert.True(t, stream.Msg().Event.GetSettingsChanged())
	rules := r.ListPermissionRules()
	found := false
	for _, rule := range rules {
		found = found || rule.Rule == "write(secret/**)"
	}
	assert.True(t, found, "the rule isn't in force: %+v", rules)
}

// SetConfigValue sets a key in the global settings or a workspace's, and
// a key it doesn't set is refused (spec_067_cli_parity CLP-40).
func TestSetConfigValue(t *testing.T) {
	r, url := attachURL(t, nil)
	s := AttachSettingsHTTP(http.DefaultClient, url)
	c, err := s.SetConfigValue(context.Background(), "", "ui.theme", "dracula")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(config.ConfigDir(""), ".env.toml"), c.Path)
	c, err = s.SetConfigValue(context.Background(), r.dir, "ui.banner", "compact")
	require.NoError(t, err)
	assert.NotEmpty(t, c.Path)
	cfg, err := config.LoadWorkspace("", r.dir)
	require.NoError(t, err)
	assert.Equal(t, "dracula", cfg.UI.Theme)
	assert.Equal(t, "compact", cfg.UI.Banner)
	_, err = s.SetConfigValue(context.Background(), "", "sandbox.allow_network", "true")
	assert.Error(t, err)
}

// Reasoning comes back from the service with its turns and nodes
// (spec_066_reasoning; /reasoning, CLP-47).
func TestRemoteReasoning(t *testing.T) {
	r, _ := attachURL(t, nil, text("first answer"), text("second answer"))
	s, err := r.NewSession()
	require.NoError(t, err)
	for _, p := range []string{"one", "two"} {
		_, err = r.Run(context.Background(), s.ID, api.Turn{Text: p, Timeout: time.Minute}, func(api.Event) {})
		require.NoError(t, err)
	}
	got, err := r.Reasoning(s.ID, -1)
	require.NoError(t, err)
	require.Len(t, got.Turns, 2)
	assert.Equal(t, 1, got.Turn, "the last")
	assert.Equal(t, "two", got.Turns[1].Prompt)
	require.NotEmpty(t, got.Nodes)
	kinds := map[string]bool{}
	for _, n := range got.Nodes {
		kinds[n.Kind] = true
	}
	assert.True(t, kinds["turn"] && kinds["call"], "%v", kinds)
	first, err := r.Reasoning(s.ID, 0)
	require.NoError(t, err)
	assert.Equal(t, 0, first.Turn)
	none, err := r.Reasoning("nope", -1)
	require.NoError(t, err)
	assert.Empty(t, none.Turns, "a session without a log has no turns")
}
