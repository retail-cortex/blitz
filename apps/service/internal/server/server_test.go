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
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/images"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

type clients struct {
	sessions   pb.SessionServiceClient
	workspaces pb.WorkspaceServiceClient
}

// serve starts a server whose workspaces run on mock models answering with
// replies, and returns clients for it. mutate adjusts each workspace's
// configuration.
func serve(t *testing.T, mutate func(*config.Config), replies ...*genai.Content) (clients, *Server) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	t.Chdir(t.TempDir())
	s := New(func(ctx context.Context, dir string) (*engine.Workspace, error) {
		cfg := config.DefaultConfig()
		cfg.Tools.WorkspaceDir = dir
		cfg.Session.StorageDir = t.TempDir()
		if mutate != nil {
			mutate(cfg)
		}
		return engine.Open(ctx, cfg, engine.Options{
			Model: runtime.NewMockLLM("gemini-3.8-flash", replies...),
			NewModel: func(_ context.Context, _ *config.Config, ref string) (model.LLM, error) {
				_, name := runtime.ParseModelRef(ref, "")
				return runtime.NewMockLLM(name), nil
			},
		})
	})
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(func() { srv.Close(); s.Close() })
	return clients{
		sessions:   pb.NewSessionServiceClient(http.DefaultClient, srv.URL),
		workspaces: pb.NewWorkspaceServiceClient(http.DefaultClient, srv.URL),
	}, s
}

// errorReason returns the ErrorInfo reason and metadata of a failed call.
func errorReason(t *testing.T, err error) (connect.Code, *pb.ErrorInfo) {
	t.Helper()
	var ce *connect.Error
	require.ErrorAs(t, err, &ce, "not a Connect error: %v", err)
	for _, d := range ce.Details() {
		if v, derr := d.Value(); derr == nil {
			if info, ok := v.(*pb.ErrorInfo); ok {
				return ce.Code(), info
			}
		}
	}
	t.Fatalf("no ErrorInfo on %v", err)
	return 0, nil
}

func text(s string) *genai.Content { return genai.NewContentFromText(s, genai.RoleModel) }

func TestWorkspaceOperationsOverTheAPI(t *testing.T) {
	c, _ := serve(t, nil)
	ctx := context.Background()
	dir := t.TempDir()

	agents, err := c.workspaces.ListAgents(ctx, connect.NewRequest(&pb.ListAgentsRequest{Workspace: dir}))
	require.NoError(t, err)
	i := slices.IndexFunc(agents.Msg.Agents, func(a *pb.AgentInfo) bool { return a.Active })
	assert.GreaterOrEqual(t, i, 0, "agents %v", agents.Msg.Agents)
	assert.Equal(t, "blitz", agents.Msg.Agents[i].Name, "agents %v", agents.Msg.Agents)
	_, setErr := c.workspaces.SetAgent(ctx, connect.NewRequest(&pb.SetAgentRequest{Workspace: dir, Name: "qa"}))
	require.NoError(t, setErr)
	pin, err := c.workspaces.PinModel(ctx, connect.NewRequest(&pb.PinModelRequest{Workspace: dir, Agent: "qa", Ref: "anthropic/claude-haiku-4-5"}))
	require.NoError(t, err, "pin %v", pin)
	require.Equal(t, "claude-haiku-4-5", pin.Msg.Model, "pin %v %v", pin, err)
	require.Equal(t, "", pin.Msg.Saved.Error, "pin %v %v", pin, err)
	m, _ := c.workspaces.GetModel(ctx, connect.NewRequest(&pb.GetModelRequest{Workspace: dir}))
	assert.Equal(t, "claude-haiku-4-5", m.Msg.Name, "model %v", m.Msg)

	// Typed errors arrive as codes with an ErrorInfo detail.
	_, err = c.workspaces.PinModel(ctx, connect.NewRequest(&pb.PinModelRequest{Workspace: dir, Agent: "nobody", Ref: "x"}))
	code, info := errorReason(t, err)
	assert.Equal(t, connect.CodeNotFound, code, "unknown agent: %v %v", code, info)
	assert.Equal(t, "UNKNOWN_AGENT", info.Reason, "unknown agent: %v %v", code, info)
	assert.Equal(t, "nobody", info.Metadata["name"], "unknown agent: %v %v", code, info)
	_, err = c.workspaces.UpdateModelSettings(ctx, connect.NewRequest(&pb.UpdateModelSettingsRequest{Workspace: dir, Ref: "gpt-5", Changes: []*pb.Setting{{Key: "temperature", Value: "9"}}}))
	code, info = errorReason(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, code, "invalid setting: %v %v", code, info)
	assert.Equal(t, "INVALID_SETTING", info.Reason, "invalid setting: %v %v", code, info)
	_, err = c.workspaces.ListAgents(ctx, connect.NewRequest(&pb.ListAgentsRequest{Workspace: "relative/dir"}))
	code, info = errorReason(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, code, "relative workspace: %v %v", code, info)
	assert.Equal(t, "INVALID_WORKSPACE", info.Reason, "relative workspace: %v %v", code, info)

	tools, err := c.workspaces.ListTools(ctx, connect.NewRequest(&pb.ListToolsRequest{Workspace: dir}))
	assert.NoError(t, err, "tools %v", tools)
	assert.Equal(t, "qa", tools.Msg.Agent, "tools %v %v", tools, err)
	assert.NotEqual(t, 0, len(tools.Msg.Tools), "tools %v %v", tools, err)
}

func TestTurnsAndSessionsOverTheAPI(t *testing.T) {
	c, _ := serve(t, nil, text("hello there"))
	ctx := context.Background()
	dir := t.TempDir()

	opened, err := c.sessions.OpenSession(ctx, connect.NewRequest(&pb.OpenSessionRequest{Workspace: dir}))
	require.NoError(t, err, "open %v", opened)
	require.False(t, opened.Msg.Resumed, "open %v %v", opened, err)
	stream, err := c.sessions.RunTurn(ctx, connect.NewRequest(&pb.RunTurnRequest{Workspace: dir, SessionId: opened.Msg.Session.Id, Turn: &pb.Turn{Text: "hi"}}))
	require.NoError(t, err)
	var kinds []string
	var finished *pb.TurnFinished
	for stream.Receive() {
		ev := stream.Msg().Event
		switch k := ev.Kind.(type) {
		case *pb.TurnEvent_Accepted:
			kinds = append(kinds, "accepted")
		case *pb.TurnEvent_Text:
			kinds = append(kinds, "text:"+k.Text.Text)
		case *pb.TurnEvent_Finished:
			kinds = append(kinds, "finished")
			finished = k.Finished
		}
	}
	require.NoError(t, stream.Err())
	assert.Equal(t, []string{"accepted", "text:hello there", "finished"}, kinds, "events %v, finished %v", kinds, finished)
	assert.Equal(t, "hello there", finished.Output, "events %v, finished %v", kinds, finished)
	assert.Nil(t, finished.Error, "events %v, finished %v", kinds, finished)

	active, err := c.sessions.GetActiveSession(ctx, connect.NewRequest(&pb.GetActiveSessionRequest{Workspace: dir}))
	assert.NoError(t, err, "active %v", active)
	assert.Len(t, active.Msg.Session.Messages, 2, "active %v %v", active, err)
	assert.Equal(t, "hi", active.Msg.Session.Title, "active %v %v", active, err)
	_, saveErr := c.sessions.SaveSnapshot(ctx, connect.NewRequest(&pb.SaveSnapshotRequest{Workspace: dir, Name: "s"}))
	require.NoError(t, saveErr)
	_, err = c.sessions.SaveSnapshot(ctx, connect.NewRequest(&pb.SaveSnapshotRequest{Workspace: dir, Name: "s"}))
	code, info := errorReason(t, err)
	assert.Equal(t, connect.CodeAlreadyExists, code, "taken: %v %v", code, info)
	assert.Equal(t, "SNAPSHOT_NAME_TAKEN", info.Reason, "taken: %v %v", code, info)

	// An image must be loaded or added before a turn can use it.
	stream, _ = c.sessions.RunTurn(ctx, connect.NewRequest(&pb.RunTurnRequest{Workspace: dir, SessionId: opened.Msg.Session.Id, Turn: &pb.Turn{Text: "see", ImageIds: []string{"nope"}}}))
	for stream.Receive() {
	}
	code, info = errorReason(t, stream.Err())
	assert.Equal(t, connect.CodeNotFound, code, "unknown image: %v %v", code, info)
	assert.Equal(t, "UNKNOWN_IMAGE", info.Reason, "unknown image: %v %v", code, info)
}

func TestBlockedTurnReportsTheReason(t *testing.T) {
	c, _ := serve(t, func(cfg *config.Config) {
		cfg.Hooks.PromptSubmit = []config.HookConfig{{Command: `echo "no secrets" >&2; exit 2`}}
	})
	ctx := context.Background()
	dir := t.TempDir()
	s, _ := c.sessions.NewSession(ctx, connect.NewRequest(&pb.NewSessionRequest{Workspace: dir}))
	stream, err := c.sessions.RunTurn(ctx, connect.NewRequest(&pb.RunTurnRequest{Workspace: dir, SessionId: s.Msg.Session.Id, Turn: &pb.Turn{Text: "my password"}}))
	require.NoError(t, err)
	var finished *pb.TurnFinished
	for stream.Receive() {
		if f := stream.Msg().Event.GetFinished(); f != nil {
			finished = f
		}
	}
	assert.NotNil(t, finished, "finished %v", finished)
	assert.Equal(t, "PROMPT_BLOCKED", finished.Error.GetReason(), "finished %v", finished)
	assert.NotEqual(t, "", finished.Error.Metadata["reason"], "finished %v", finished)
}

func TestWorkspacesAreKeptAndClosed(t *testing.T) {
	c, s := serve(t, nil)
	ctx := context.Background()
	a, b := t.TempDir(), t.TempDir()
	// Another spelling of a, through a symlink, is the same workspace.
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(a, link))
	for _, dir := range []string{a, b, link} {
		_, err := c.workspaces.GetModel(ctx, connect.NewRequest(&pb.GetModelRequest{Workspace: dir}))
		require.NoError(t, err)
	}
	list, _ := c.workspaces.ListWorkspaces(ctx, connect.NewRequest(&pb.ListWorkspacesRequest{}))
	require.Len(t, list.Msg.Workspaces, 2, "workspaces %v", list.Msg.Workspaces)
	_, err := c.workspaces.CloseWorkspace(ctx, connect.NewRequest(&pb.CloseWorkspaceRequest{Workspace: link}))
	require.NoError(t, err)
	got := s.openDirs()
	assert.Len(t, got, 1, "after close: %v", got)
}

// A slow open holds up only its own workspace; callers for it share one
// open; and nothing opens once the server is closed.
func TestWorkspacesOpenIndependently(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	slow, fast := t.TempDir(), t.TempDir()
	slow, _ = filepath.EvalSymlinks(slow)
	release := make(chan struct{})
	var opens atomic.Int32
	s := New(func(ctx context.Context, dir string) (*engine.Workspace, error) {
		if dir == slow {
			opens.Add(1)
			<-release
		}
		cfg := config.DefaultConfig()
		cfg.Tools.WorkspaceDir = dir
		cfg.Session.StorageDir = t.TempDir()
		return engine.Open(ctx, cfg, engine.Options{Model: runtime.NewMockLLM("m")})
	})
	ctx := context.Background()

	got := make(chan *workspace, 2)
	for range 2 {
		go func() {
			w, err := s.workspace(ctx, slow)
			assert.NoError(t, err)
			got <- w
		}()
	}
	_, err := s.workspace(ctx, fast)
	require.NoError(t, err, "another workspace waited on a slow open")
	gone, cancel := context.WithCancel(ctx)
	cancel()
	_, err = s.workspace(gone, slow)
	assert.Equal(t, connect.CodeCanceled, connect.CodeOf(err), "a caller that gives up: %v", err)
	close(release)
	a, b := <-got, <-got
	assert.NotNil(t, a, "shared open: %p %p, %d opens", a, b, opens.Load())
	assert.Same(t, b, a, "shared open: %p %p, %d opens", a, b, opens.Load())
	assert.Equal(t, int32(1), opens.Load(), "shared open: %p %p, %d opens", a, b, opens.Load())

	s.Close()
	code, info := errorReason(t, func() error { _, err := s.workspace(ctx, fast); return err }())
	assert.Equal(t, connect.CodeUnavailable, code, "after Close: %v %v", code, info)
	assert.Equal(t, "SHUTTING_DOWN", info.Reason, "after Close: %v %v", code, info)
}

// A workspace keeps only its most recently used images.
func TestKeptImagesAreBounded(t *testing.T) {
	w := &workspace{images: map[string]*images.Image{}}
	for i := range keptImages + 1 {
		w.keepImage(&images.Image{SHA256: fmt.Sprint(i)})
		if i == 0 {
			continue
		}
		_, err := w.image("0")
		require.NoError(t, err)
	}
	require.Len(t, w.images, keptImages, "kept %d images, %d in order", len(w.images), len(w.order))
	require.Len(t, w.order, keptImages, "kept %d images, %d in order", len(w.images), len(w.order))
	_, err := w.image("0")
	assert.NoError(t, err, "the image in use was forgotten")
	_, err = w.image("1")
	assert.Error(t, err, "the least recently used image was kept")
}
