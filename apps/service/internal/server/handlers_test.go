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
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/memory"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/workers"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
	"google.golang.org/protobuf/types/known/durationpb"
)

// every is a client for each of the server's services.
type every struct {
	sessions   pb.SessionServiceClient
	workspaces pb.WorkspaceServiceClient
	workers    pb.WorkerServiceClient
	config     pb.ConfigServiceClient
	files      pb.FileServiceClient
}

// serveEvery starts a server that also runs workers, whose workspaces run
// on mock models answering with replies, and returns clients for every
// service. mutate adjusts each workspace's configuration.
func serveEvery(t *testing.T, mutate func(*config.Config), replies ...*genai.Content) (every, *Server) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	t.Chdir(t.TempDir())
	store, err := workers.OpenStore(filepath.Join(t.TempDir(), "workers.json"))
	require.NoError(t, err)
	s := New(func(ctx context.Context, dir string) (*engine.Workspace, error) {
		cfg := config.DefaultConfig()
		cfg.Tools.WorkspaceDir = dir
		cfg.Session.StorageDir = t.TempDir()
		cfg.Images.Dir = t.TempDir()
		if mutate != nil {
			mutate(cfg)
		}
		return engine.Open(ctx, cfg, engine.Options{
			Model:   runtime.NewMockLLM("gemini-3.8-flash", replies...),
			Workers: store,
			NewModel: func(_ context.Context, _ *config.Config, ref string) (model.LLM, error) {
				_, name := runtime.ParseModelRef(ref, "")
				return runtime.NewMockLLM(name), nil
			},
		})
	}, WithScheduler(SchedulerConfig{Store: store, Runs: workers.OpenRunLog(filepath.Join(t.TempDir(), "worker-runs")), Rescan: time.Hour}))
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(func() { srv.Close(); s.Close() })
	hc := http.DefaultClient
	return every{
		sessions:   pb.NewSessionServiceClient(hc, srv.URL),
		workspaces: pb.NewWorkspaceServiceClient(hc, srv.URL),
		workers:    pb.NewWorkerServiceClient(hc, srv.URL),
		config:     pb.NewConfigServiceClient(hc, srv.URL),
		files:      pb.NewFileServiceClient(hc, srv.URL),
	}, s
}

// unary calls a unary RPC and returns its error.
func unary[Req, Res any](call func(context.Context, *connect.Request[Req]) (*connect.Response[Res], error), msg *Req) error {
	_, err := call(context.Background(), connect.NewRequest(msg))
	return err
}

// streamErr reads a server stream to its end and returns its error.
func streamErr[Res any](stream *connect.ServerStreamForClient[Res], err error) error {
	if err != nil {
		return err
	}
	defer stream.Close()
	for stream.Receive() {
	}
	return stream.Err()
}

// Every handler that works in a workspace refuses one that isn't an
// absolute directory, as INVALID_WORKSPACE, before doing anything.
func TestEveryHandlerRefusesARelativeWorkspace(t *testing.T) {
	c, _ := serveEvery(t, nil)
	ctx := context.Background()
	const ws = "relative/dir"
	s, w, wk, f := c.sessions, c.workspaces, c.workers, c.files
	for name, call := range map[string]func() error{
		"ListSessions":     func() error { return unary(s.ListSessions, &pb.ListSessionsRequest{Workspace: ws}) },
		"GetActiveSession": func() error { return unary(s.GetActiveSession, &pb.GetActiveSessionRequest{Workspace: ws}) },
		"NewSession":       func() error { return unary(s.NewSession, &pb.NewSessionRequest{Workspace: ws}) },
		"OpenSession":      func() error { return unary(s.OpenSession, &pb.OpenSessionRequest{Workspace: ws}) },
		"LoadSession":      func() error { return unary(s.LoadSession, &pb.LoadSessionRequest{Workspace: ws}) },
		"SaveSnapshot":     func() error { return unary(s.SaveSnapshot, &pb.SaveSnapshotRequest{Workspace: ws}) },
		"MoveSession":      func() error { return unary(s.MoveSession, &pb.MoveSessionRequest{Workspace: ws}) },
		"RenameSession":    func() error { return unary(s.RenameSession, &pb.RenameSessionRequest{Workspace: ws}) },
		"DeleteSession":    func() error { return unary(s.DeleteSession, &pb.DeleteSessionRequest{Workspace: ws}) },
		"ForkSession":      func() error { return unary(s.ForkSession, &pb.ForkSessionRequest{Workspace: ws}) },
		"ExportSession":    func() error { return unary(s.ExportSession, &pb.ExportSessionRequest{Workspace: ws}) },
		"Steer":            func() error { return unary(s.Steer, &pb.SteerRequest{Workspace: ws}) },
		"GetUsage":         func() error { return unary(s.GetUsage, &pb.GetUsageRequest{Workspace: ws}) },
		"Compact":          func() error { return unary(s.Compact, &pb.CompactRequest{Workspace: ws}) },
		"SetGoal":          func() error { return unary(s.SetGoal, &pb.SetGoalRequest{Workspace: ws}) },
		"GetGoal":          func() error { return unary(s.GetGoal, &pb.GetGoalRequest{Workspace: ws}) },
		"ClearGoal":        func() error { return unary(s.ClearGoal, &pb.ClearGoalRequest{Workspace: ws}) },
		"SearchSession":    func() error { return unary(s.SearchSession, &pb.SearchSessionRequest{Workspace: ws}) },
		"ListRewindPoints": func() error { return unary(s.ListRewindPoints, &pb.ListRewindPointsRequest{Workspace: ws}) },
		"Rewind":           func() error { return unary(s.Rewind, &pb.RewindRequest{Workspace: ws}) },
		"StartBackground":  func() error { return unary(s.StartBackground, &pb.StartBackgroundRequest{Workspace: ws}) },
		"RunTurn": func() error {
			return streamErr(s.RunTurn(ctx, connect.NewRequest(&pb.RunTurnRequest{Workspace: ws, Turn: &pb.Turn{Text: "hi"}})))
		},

		"GetSandbox":           func() error { return unary(w.GetSandbox, &pb.GetSandboxRequest{Workspace: ws}) },
		"SetAgent":             func() error { return unary(w.SetAgent, &pb.SetAgentRequest{Workspace: ws}) },
		"GetModel":             func() error { return unary(w.GetModel, &pb.GetModelRequest{Workspace: ws}) },
		"SetModel":             func() error { return unary(w.SetModel, &pb.SetModelRequest{Workspace: ws}) },
		"PinModel":             func() error { return unary(w.PinModel, &pb.PinModelRequest{Workspace: ws}) },
		"UnpinModel":           func() error { return unary(w.UnpinModel, &pb.UnpinModelRequest{Workspace: ws}) },
		"GetModelSettings":     func() error { return unary(w.GetModelSettings, &pb.GetModelSettingsRequest{Workspace: ws}) },
		"UpdateModelSettings":  func() error { return unary(w.UpdateModelSettings, &pb.UpdateModelSettingsRequest{Workspace: ws}) },
		"GetSettings":          func() error { return unary(w.GetSettings, &pb.GetSettingsRequest{Workspace: ws}) },
		"ListCommands":         func() error { return unary(w.ListCommands, &pb.ListCommandsRequest{Workspace: ws}) },
		"ListPermissionRules":  func() error { return unary(w.ListPermissionRules, &pb.ListPermissionRulesRequest{Workspace: ws}) },
		"AddPermissionRule":    func() error { return unary(w.AddPermissionRule, &pb.AddPermissionRuleRequest{Workspace: ws}) },
		"RemovePermissionRule": func() error { return unary(w.RemovePermissionRule, &pb.RemovePermissionRuleRequest{Workspace: ws}) },
		"SetPermissionMode":    func() error { return unary(w.SetPermissionMode, &pb.SetPermissionModeRequest{Workspace: ws}) },
		"SetSetting":           func() error { return unary(w.SetSetting, &pb.SetSettingRequest{Workspace: ws}) },
		"ListSkills":           func() error { return unary(w.ListSkills, &pb.ListSkillsRequest{Workspace: ws}) },
		"GetSkill":             func() error { return unary(w.GetSkill, &pb.GetSkillRequest{Workspace: ws}) },
		"ListEnvs":             func() error { return unary(w.ListEnvs, &pb.ListEnvsRequest{Workspace: ws}) },
		"RemoveEnv":            func() error { return unary(w.RemoveEnv, &pb.RemoveEnvRequest{Workspace: ws}) },
		"PruneEnvs":            func() error { return unary(w.PruneEnvs, &pb.PruneEnvsRequest{Workspace: ws}) },
		"ListMCPServers":       func() error { return unary(w.ListMCPServers, &pb.ListMCPServersRequest{Workspace: ws}) },
		"ListTools":            func() error { return unary(w.ListTools, &pb.ListToolsRequest{Workspace: ws}) },
		"ReloadMemory":         func() error { return unary(w.ReloadMemory, &pb.ReloadMemoryRequest{Workspace: ws}) },
		"AddMemory":            func() error { return unary(w.AddMemory, &pb.AddMemoryRequest{Workspace: ws}) },
		"ListLocales":          func() error { return unary(w.ListLocales, &pb.ListLocalesRequest{Workspace: ws}) },
		"SetLocale":            func() error { return unary(w.SetLocale, &pb.SetLocaleRequest{Workspace: ws}) },
		"ListCheckpoints":      func() error { return unary(w.ListCheckpoints, &pb.ListCheckpointsRequest{Workspace: ws}) },
		"Undo":                 func() error { return unary(w.Undo, &pb.UndoRequest{Workspace: ws}) },
		"GetDiff":              func() error { return unary(w.GetDiff, &pb.GetDiffRequest{Workspace: ws}) },
		"ListApprovals":        func() error { return unary(w.ListApprovals, &pb.ListApprovalsRequest{Workspace: ws}) },
		"RevokeApprovals":      func() error { return unary(w.RevokeApprovals, &pb.RevokeApprovalsRequest{Workspace: ws}) },
		"ListProcesses":        func() error { return unary(w.ListProcesses, &pb.ListProcessesRequest{Workspace: ws}) },
		"GetProcessOutput":     func() error { return unary(w.GetProcessOutput, &pb.GetProcessOutputRequest{Workspace: ws}) },
		"KillProcess":          func() error { return unary(w.KillProcess, &pb.KillProcessRequest{Workspace: ws}) },
		"AuditShell":           func() error { return unary(w.AuditShell, &pb.AuditShellRequest{Workspace: ws}) },
		"LoadImage":            func() error { return unary(w.LoadImage, &pb.LoadImageRequest{Workspace: ws}) },
		"AddImage":             func() error { return unary(w.AddImage, &pb.AddImageRequest{Workspace: ws}) },
		"GetSearchProvider":    func() error { return unary(w.GetSearchProvider, &pb.GetSearchProviderRequest{Workspace: ws}) },
		"SearchWeb":            func() error { return unary(w.SearchWeb, &pb.SearchWebRequest{Workspace: ws}) },
		"GetProjectSettings":   func() error { return unary(w.GetProjectSettings, &pb.GetProjectSettingsRequest{Workspace: ws}) },
		"TrustProject":         func() error { return unary(w.TrustProject, &pb.TrustProjectRequest{Workspace: ws}) },
		"TakeProcessNotices":   func() error { return unary(w.TakeProcessNotices, &pb.TakeProcessNoticesRequest{Workspace: ws}) },
		"ListStyles":           func() error { return unary(w.ListStyles, &pb.ListStylesRequest{Workspace: ws}) },
		"ListHooks":            func() error { return unary(w.ListHooks, &pb.ListHooksRequest{Workspace: ws}) },
		"ListNotes":            func() error { return unary(w.ListNotes, &pb.ListNotesRequest{Workspace: ws}) },
		"ForgetNote":           func() error { return unary(w.ForgetNote, &pb.ForgetNoteRequest{Workspace: ws}) },
		"ListTasks":            func() error { return unary(w.ListTasks, &pb.ListTasksRequest{Workspace: ws}) },
		"GetTask":              func() error { return unary(w.GetTask, &pb.GetTaskRequest{Workspace: ws}) },
		"StopTask":             func() error { return unary(w.StopTask, &pb.StopTaskRequest{Workspace: ws}) },
		"ListTaskRequests":     func() error { return unary(w.ListTaskRequests, &pb.ListTaskRequestsRequest{Workspace: ws}) },
		"GetGitStatus":         func() error { return unary(w.GetGitStatus, &pb.GetGitStatusRequest{Workspace: ws}) },
		"InitGitRepo":          func() error { return unary(w.InitGitRepo, &pb.InitGitRepoRequest{Workspace: ws}) },
		"CloseWorkspace":       func() error { return unary(w.CloseWorkspace, &pb.CloseWorkspaceRequest{Workspace: ws}) },
		"WatchTasks": func() error {
			return streamErr(w.WatchTasks(ctx, connect.NewRequest(&pb.WatchTasksRequest{Workspace: ws})))
		},

		"ListWorkers":    func() error { return unary(wk.ListWorkers, &pb.ListWorkersRequest{Workspace: ws}) },
		"CreateWorker":   func() error { return unary(wk.CreateWorker, &pb.CreateWorkerRequest{Workspace: ws}) },
		"GetWorkerSpec":  func() error { return unary(wk.GetWorkerSpec, &pb.GetWorkerSpecRequest{Workspace: ws}) },
		"UpdateWorker":   func() error { return unary(wk.UpdateWorker, &pb.UpdateWorkerRequest{Workspace: ws}) },
		"EnableWorker":   func() error { return unary(wk.EnableWorker, &pb.EnableWorkerRequest{Workspace: ws}) },
		"DisableWorker":  func() error { return unary(wk.DisableWorker, &pb.DisableWorkerRequest{Workspace: ws}) },
		"DeleteWorker":   func() error { return unary(wk.DeleteWorker, &pb.DeleteWorkerRequest{Workspace: ws}) },
		"RunWorker":      func() error { return unary(wk.RunWorker, &pb.RunWorkerRequest{Workspace: ws}) },
		"ListWorkerRuns": func() error { return unary(wk.ListWorkerRuns, &pb.ListWorkerRunsRequest{Workspace: ws}) },
		"UndoWorkerRun":  func() error { return unary(wk.UndoWorkerRun, &pb.UndoWorkerRunRequest{Workspace: ws}) },

		"ListAgentFiles": func() error {
			return unary(w.ListAgentFiles, &pb.ListAgentFilesRequest{Workspace: ws, Scope: pb.AgentScope_AGENT_SCOPE_WORKSPACE})
		},
		"SaveAgentFile": func() error {
			return unary(w.SaveAgentFile, &pb.SaveAgentFileRequest{Workspace: ws, Scope: pb.AgentScope_AGENT_SCOPE_WORKSPACE})
		},
		"DeleteAgentFile": func() error {
			return unary(w.DeleteAgentFile, &pb.DeleteAgentFileRequest{Workspace: ws, Scope: pb.AgentScope_AGENT_SCOPE_WORKSPACE})
		},

		"ListDir":     func() error { return unary(f.ListDir, &pb.ListDirRequest{Workspace: ws}) },
		"ReadFile":    func() error { return unary(f.ReadFile, &pb.ReadFileRequest{Workspace: ws}) },
		"ReadPreview": func() error { return unary(f.ReadPreview, &pb.ReadPreviewRequest{Workspace: ws}) },
		"WriteFile":   func() error { return unary(f.WriteFile, &pb.WriteFileRequest{Workspace: ws}) },
		"WriteBinaryFile": func() error {
			return unary(f.WriteBinaryFile, &pb.WriteBinaryFileRequest{Workspace: ws})
		},
		"CreateFolder": func() error { return unary(f.CreateFolder, &pb.CreateFolderRequest{Workspace: ws}) },
		"RenameFile":   func() error { return unary(f.RenameFile, &pb.RenameFileRequest{Workspace: ws}) },
		"DeleteFile":   func() error { return unary(f.DeleteFile, &pb.DeleteFileRequest{Workspace: ws}) },
		"GitFileAction": func() error {
			return unary(f.GitFileAction, &pb.GitFileActionRequest{Workspace: ws, Path: "x", Action: pb.GitAction_GIT_ACTION_STAGE})
		},
		"FindFiles": func() error { return unary(f.FindFiles, &pb.FindFilesRequest{Workspace: ws}) },
		"StatFiles": func() error { return unary(f.StatFiles, &pb.StatFilesRequest{Workspace: ws}) },
	} {
		t.Run(name, func(t *testing.T) {
			code, info := errorReason(t, call())
			assert.Equal(t, connect.CodeInvalidArgument, code)
			assert.Equal(t, "INVALID_WORKSPACE", info.Reason)
			assert.Equal(t, ws, info.Metadata["workspace"])
		})
	}
}

// The settings handlers refuse a workspace that isn't an absolute
// directory as an invalid argument.
func TestConfigHandlersRefuseARelativeWorkspace(t *testing.T) {
	c, _ := serveEvery(t, nil)
	const ws = "relative/dir"
	cf := c.config
	for name, call := range map[string]func() error{
		"DescribeConfig":      func() error { return unary(cf.DescribeConfig, &pb.DescribeConfigRequest{Workspace: ws}) },
		"SetApiKey":           func() error { return unary(cf.SetApiKey, &pb.SetApiKeyRequest{Workspace: ws}) },
		"RemoveApiKey":        func() error { return unary(cf.RemoveApiKey, &pb.RemoveApiKeyRequest{Workspace: ws}) },
		"SecureApiKey":        func() error { return unary(cf.SecureApiKey, &pb.SecureApiKeyRequest{Workspace: ws}) },
		"SetConfigValue":      func() error { return unary(cf.SetConfigValue, &pb.SetConfigValueRequest{Workspace: ws}) },
		"SetProvider":         func() error { return unary(cf.SetProvider, &pb.SetProviderRequest{Workspace: ws}) },
		"DescribePermissions": func() error { return unary(cf.DescribePermissions, &pb.DescribePermissionsRequest{Workspace: ws}) },
		"AddPermission":       func() error { return unary(cf.AddPermission, &pb.AddPermissionRequest{Workspace: ws}) },
		"RemovePermission":    func() error { return unary(cf.RemovePermission, &pb.RemovePermissionRequest{Workspace: ws}) },
		"SetReadOnlyDefaults": func() error { return unary(cf.SetReadOnlyDefaults, &pb.SetReadOnlyDefaultsRequest{Workspace: ws}) },
		"GetConfigFile":       func() error { return unary(cf.GetConfigFile, &pb.GetConfigFileRequest{Workspace: ws}) },
		"SaveConfigFile":      func() error { return unary(cf.SaveConfigFile, &pb.SaveConfigFileRequest{Workspace: ws}) },
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(call()))
		})
	}
}

// Without a scheduler, the worker handlers that need one refuse with
// NO_SCHEDULER.
func TestWorkerHandlersNeedAScheduler(t *testing.T) {
	_, s := serve(t, nil)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	wk := pb.NewWorkerServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()
	dir := t.TempDir()
	for name, call := range map[string]func() error{
		"ListWorkers":    func() error { return unary(wk.ListWorkers, &pb.ListWorkersRequest{Workspace: dir}) },
		"EnableWorker":   func() error { return unary(wk.EnableWorker, &pb.EnableWorkerRequest{Workspace: dir}) },
		"DisableWorker":  func() error { return unary(wk.DisableWorker, &pb.DisableWorkerRequest{Workspace: dir}) },
		"RunWorker":      func() error { return unary(wk.RunWorker, &pb.RunWorkerRequest{Workspace: dir}) },
		"ListWorkerRuns": func() error { return unary(wk.ListWorkerRuns, &pb.ListWorkerRunsRequest{Workspace: dir}) },
		"GetWorkerRun":   func() error { return unary(wk.GetWorkerRun, &pb.GetWorkerRunRequest{RunId: "x"}) },
		"WatchWorkerRun": func() error {
			return streamErr(wk.WatchWorkerRun(ctx, connect.NewRequest(&pb.WatchWorkerRunRequest{RunId: "x"})))
		},
	} {
		t.Run(name, func(t *testing.T) {
			code, info := errorReason(t, call())
			assert.Equal(t, connect.CodeFailedPrecondition, code)
			assert.Equal(t, "NO_SCHEDULER", info.Reason)
		})
	}
}

// reason is a failed call's ErrorInfo reason.
func reason(t *testing.T, err error) string {
	t.Helper()
	_, info := errorReason(t, err)
	return info.Reason
}

// The session handlers over a workspace with no session yet, then with
// one: each answers, and each documented failure arrives as its reason.
func TestSessionHandlers(t *testing.T) {
	store := t.TempDir() // one for every workspace, as ~/.blitz/sessions is
	c, _ := serveEvery(t, func(cfg *config.Config) { cfg.Session.StorageDir = store }, text("first answer"), text("second answer"))
	s := c.sessions
	ctx := context.Background()
	dir := t.TempDir()

	// Without a session.
	for name, call := range map[string]func() error{
		"GetUsage":      func() error { return unary(s.GetUsage, &pb.GetUsageRequest{Workspace: dir}) },
		"Compact":       func() error { return unary(s.Compact, &pb.CompactRequest{Workspace: dir}) },
		"SetGoal":       func() error { return unary(s.SetGoal, &pb.SetGoalRequest{Workspace: dir, Condition: "tests pass"}) },
		"GetGoal":       func() error { return unary(s.GetGoal, &pb.GetGoalRequest{Workspace: dir}) },
		"ClearGoal":     func() error { return unary(s.ClearGoal, &pb.ClearGoalRequest{Workspace: dir}) },
		"RenameSession": func() error { return unary(s.RenameSession, &pb.RenameSessionRequest{Workspace: dir, Title: "t"}) },
		"ForkSession":   func() error { return unary(s.ForkSession, &pb.ForkSessionRequest{Workspace: dir}) },
		"ExportSession": func() error { return unary(s.ExportSession, &pb.ExportSessionRequest{Workspace: dir}) },
		"Rewinds":       func() error { return unary(s.ListRewindPoints, &pb.ListRewindPointsRequest{Workspace: dir}) },
		"Rewind":        func() error { return unary(s.Rewind, &pb.RewindRequest{Workspace: dir, Mode: "conversation"}) },
	} {
		t.Run("no session/"+name, func(t *testing.T) {
			assert.Equal(t, "NO_ACTIVE_SESSION", reason(t, call()))
		})
	}
	active, err := s.GetActiveSession(ctx, connect.NewRequest(&pb.GetActiveSessionRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.Nil(t, active.Msg.Session, "a session before any was opened")
	assert.Equal(t, "SESSION_NOT_FOUND", reason(t, unary(s.LoadSession, &pb.LoadSessionRequest{Workspace: dir, Ref: "nope"})))
	assert.Equal(t, "RESUME_FAILED", reason(t, unary(s.OpenSession, &pb.OpenSessionRequest{Workspace: dir, Resume: "nope"})))
	assert.Equal(t, "INVALID_TURN", reason(t, streamErr(s.RunTurn(ctx, connect.NewRequest(&pb.RunTurnRequest{Workspace: dir})))))

	// With one, and two turns in it.
	made, err := s.NewSession(ctx, connect.NewRequest(&pb.NewSessionRequest{Workspace: dir}))
	require.NoError(t, err)
	id := made.Msg.Session.Id
	for _, prompt := range []string{"one", "two"} {
		require.NoError(t, streamErr(s.RunTurn(ctx, connect.NewRequest(&pb.RunTurnRequest{Workspace: dir, SessionId: id, Turn: &pb.Turn{Text: prompt, Timeout: durationpb.New(time.Minute)}}))))
	}
	list, err := s.ListSessions(ctx, connect.NewRequest(&pb.ListSessionsRequest{Workspace: dir, All: true}))
	require.NoError(t, err)
	assert.Len(t, list.Msg.Sessions, 1)
	loaded, err := s.LoadSession(ctx, connect.NewRequest(&pb.LoadSessionRequest{Workspace: dir, Ref: id}))
	require.NoError(t, err)
	assert.Equal(t, id, loaded.Msg.Session.Id)
	renamed, err := s.RenameSession(ctx, connect.NewRequest(&pb.RenameSessionRequest{Workspace: dir, Title: "Renamed"}))
	require.NoError(t, err)
	assert.Equal(t, "Renamed", renamed.Msg.Session.Title)
	exported, err := s.ExportSession(ctx, connect.NewRequest(&pb.ExportSessionRequest{Workspace: dir, SessionId: id}))
	require.NoError(t, err)
	assert.Contains(t, exported.Msg.Markdown, "first answer")
	usage, err := s.GetUsage(ctx, connect.NewRequest(&pb.GetUsageRequest{Workspace: dir, Parts: true}))
	require.NoError(t, err)
	assert.NotEmpty(t, usage.Msg.Parts, "context parts")
	found, err := s.SearchSession(ctx, connect.NewRequest(&pb.SearchSessionRequest{Workspace: dir, Terms: "second"}))
	require.NoError(t, err)
	assert.Positive(t, found.Msg.Found, "search: %v", found.Msg)

	goal, err := s.SetGoal(ctx, connect.NewRequest(&pb.SetGoalRequest{Workspace: dir, Condition: "tests pass"}))
	require.NoError(t, err)
	assert.Equal(t, "tests pass", goal.Msg.Goal.Condition)
	got, err := s.GetGoal(ctx, connect.NewRequest(&pb.GetGoalRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.Equal(t, "tests pass", got.Msg.Goal.Condition)
	require.NoError(t, unary(s.ClearGoal, &pb.ClearGoalRequest{Workspace: dir}))
	assert.Equal(t, "NO_GOAL", reason(t, unary(s.GetGoal, &pb.GetGoalRequest{Workspace: dir})))

	assert.Equal(t, "STEER_TOO_LATE", reason(t, unary(s.Steer, &pb.SteerRequest{Workspace: dir, SessionId: id, Text: "also"})))
	points, err := s.ListRewindPoints(ctx, connect.NewRequest(&pb.ListRewindPointsRequest{Workspace: dir}))
	require.NoError(t, err)
	require.Len(t, points.Msg.Points, 2)
	assert.Equal(t, "UNKNOWN_REWIND_MODE", reason(t, unary(s.Rewind, &pb.RewindRequest{Workspace: dir, Index: points.Msg.Points[1].Index, Mode: "sideways"})))
	rewound, err := s.Rewind(ctx, connect.NewRequest(&pb.RewindRequest{Workspace: dir, Index: points.Msg.Points[1].Index, Mode: "conversation"}))
	require.NoError(t, err)
	assert.Equal(t, "two", rewound.Msg.Prompt)
	assert.NotNil(t, rewound.Msg.Compacted)

	forked, err := s.ForkSession(ctx, connect.NewRequest(&pb.ForkSessionRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.NotEqual(t, id, forked.Msg.Session.Id, "a fork is a new session")
	moved, err := s.MoveSession(ctx, connect.NewRequest(&pb.MoveSessionRequest{Workspace: t.TempDir(), SessionId: id}))
	require.NoError(t, err)
	assert.Equal(t, id, moved.Msg.Session.Id)
	assert.NotEmpty(t, reason(t, unary(s.MoveSession, &pb.MoveSessionRequest{Workspace: dir, SessionId: "nope"})))
	compacted := unary(s.Compact, &pb.CompactRequest{Workspace: dir})
	assert.Equal(t, "NOTHING_TO_COMPACT", reason(t, compacted))
	opened, err := s.OpenSession(ctx, connect.NewRequest(&pb.OpenSessionRequest{Workspace: dir, ContinueLatest: true}))
	require.NoError(t, err)
	assert.True(t, opened.Msg.Resumed)

	// Deleting: not the active session, nor one open in another workspace
	// (the one moved away); a past one, once.
	assert.Equal(t, "SESSION_OPEN", reason(t, unary(s.DeleteSession, &pb.DeleteSessionRequest{Workspace: dir, SessionId: opened.Msg.Session.Id})))
	assert.Equal(t, "SESSION_OPEN", reason(t, unary(s.DeleteSession, &pb.DeleteSessionRequest{Workspace: dir, SessionId: id})))
	_, err = s.NewSession(ctx, connect.NewRequest(&pb.NewSessionRequest{Workspace: dir}))
	require.NoError(t, err)
	require.NoError(t, unary(s.DeleteSession, &pb.DeleteSessionRequest{Workspace: dir, SessionId: forked.Msg.Session.Id}))
	list, err = s.ListSessions(ctx, connect.NewRequest(&pb.ListSessionsRequest{Workspace: dir, All: true}))
	require.NoError(t, err)
	for _, l := range list.Msg.Sessions {
		assert.NotEqual(t, forked.Msg.Session.Id, l.Id, "the deleted session is still listed")
	}
	assert.Equal(t, "SESSION_NOT_FOUND", reason(t, unary(s.DeleteSession, &pb.DeleteSessionRequest{Workspace: dir, SessionId: forked.Msg.Session.Id})))

	// Replies: a decision is required, and an unknown request is refused,
	// in a workspace or none.
	assert.Equal(t, "INVALID_DECISION", reason(t, unary(s.Approve, &pb.ApproveRequest{Workspace: dir, RequestId: "r"})))
	for _, ws := range []string{dir, "", "relative/dir"} {
		t.Run("unknown request in "+ws, func(t *testing.T) {
			assert.Equal(t, "UNKNOWN_REQUEST", reason(t, unary(s.Approve, &pb.ApproveRequest{Workspace: ws, RequestId: "r", Decision: pb.Decision_DECISION_ONCE})))
			assert.Equal(t, "UNKNOWN_REQUEST", reason(t, unary(s.Answer, &pb.AnswerRequest{Workspace: ws, RequestId: "r", Answer: "yes"})))
		})
	}
}

// approveAll runs a turn in a new session, approving every request for
// good, and returns the session's ID.
func approveAll(t *testing.T, c every, dir, prompt string) string {
	t.Helper()
	ctx := context.Background()
	s, err := c.sessions.NewSession(ctx, connect.NewRequest(&pb.NewSessionRequest{Workspace: dir}))
	require.NoError(t, err)
	stream, err := c.sessions.RunTurn(ctx, connect.NewRequest(&pb.RunTurnRequest{Workspace: dir, SessionId: s.Msg.Session.Id, Turn: &pb.Turn{Text: prompt}}))
	require.NoError(t, err)
	for stream.Receive() {
		if ar := stream.Msg().Event.GetApprovalRequest(); ar != nil {
			_, err := c.sessions.Approve(ctx, connect.NewRequest(&pb.ApproveRequest{Workspace: dir, RequestId: ar.RequestId, Decision: pb.Decision_DECISION_ALWAYS}))
			assert.NoError(t, err, "approve")
		}
		if f := stream.Msg().Event.GetFinished(); f != nil {
			assert.Nil(t, f.Error, "turn %q", prompt)
		}
	}
	require.NoError(t, stream.Err())
	return s.Msg.Session.Id
}

// A turn's file changes over the API: checkpoints, the session's diff,
// standing approvals, and undoing them, with UNDO_CONFLICT when a file
// changed since and NOTHING_TO_UNDO reported once there's nothing left.
func TestChangesOverTheAPI(t *testing.T) {
	create := call("create_file", map[string]any{"path": "made.txt", "content": "hi\n"})
	c, _ := serveEvery(t, func(cfg *config.Config) { cfg.Blitz.AutoApprove = false }, create, text("made"), create, text("made again"))
	w := c.workspaces
	ctx := context.Background()
	dir := t.TempDir()

	approveAll(t, c, dir, "make a file")
	cps, err := w.ListCheckpoints(ctx, connect.NewRequest(&pb.ListCheckpointsRequest{Workspace: dir}))
	require.NoError(t, err)
	require.Len(t, cps.Msg.Checkpoints, 1)
	assert.Equal(t, []string{"made.txt"}, cps.Msg.Checkpoints[0].Files)
	diff, err := w.GetDiff(ctx, connect.NewRequest(&pb.GetDiffRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.Contains(t, diff.Msg.Diff, "made.txt")
	approvals, err := w.ListApprovals(ctx, connect.NewRequest(&pb.ListApprovalsRequest{Workspace: dir}))
	require.NoError(t, err)
	require.NotEmpty(t, approvals.Msg.Approvals, "an approval for good is kept")
	cleared, err := w.RevokeApprovals(ctx, connect.NewRequest(&pb.RevokeApprovalsRequest{Workspace: dir, All: true}))
	require.NoError(t, err)
	assert.Equal(t, int32(len(approvals.Msg.Approvals)), cleared.Msg.Revoked, "every approval")
	revoked, err := w.RevokeApprovals(ctx, connect.NewRequest(&pb.RevokeApprovalsRequest{Workspace: dir, Keys: []string{approvals.Msg.Approvals[0].Key}}))
	require.NoError(t, err)
	assert.Equal(t, int32(1), revoked.Msg.Revoked, "the keys given")

	undone, err := w.Undo(ctx, connect.NewRequest(&pb.UndoRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.Equal(t, []string{"made.txt"}, undone.Msg.Restored)
	assert.Nil(t, undone.Msg.Error)
	assert.NoFileExists(t, filepath.Join(dir, "made.txt"))
	again, err := w.Undo(ctx, connect.NewRequest(&pb.UndoRequest{Workspace: dir}))
	require.NoError(t, err, "nothing to undo is reported in the response")
	assert.Equal(t, "NOTHING_TO_UNDO", again.Msg.Error.GetReason())

	approveAll(t, c, dir, "make it again")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "made.txt"), []byte("edited since\n"), 0o644))
	assert.Equal(t, "UNDO_CONFLICT", reason(t, unary(w.Undo, &pb.UndoRequest{Workspace: dir})))
	forced, err := w.Undo(ctx, connect.NewRequest(&pb.UndoRequest{Workspace: dir, Force: true}))
	require.NoError(t, err)
	assert.Equal(t, []string{"made.txt"}, forced.Msg.Restored)
}

// A background process started by a turn is listed, read and killed for
// the sessions that name it, and UNKNOWN_PROCESS for any other.
// Choosing an agent that doesn't exist is UNKNOWN_AGENT, naming it, not
// an internal error.
func TestUnknownAgentOverTheAPI(t *testing.T) {
	c, _ := serveEvery(t, nil)
	_, err := c.workspaces.SetAgent(context.Background(), connect.NewRequest(&pb.SetAgentRequest{Workspace: t.TempDir(), Name: "ghost"}))
	code, info := errorReason(t, err)
	assert.Equal(t, connect.CodeNotFound, code)
	assert.Equal(t, "UNKNOWN_AGENT", info.Reason)
	assert.Equal(t, "ghost", info.Metadata["name"])
}

func TestProcessesOverTheAPI(t *testing.T) {
	start := call("run_shell_command", map[string]any{"command": "echo started; sleep 30", "background": true})
	c, _ := serveEvery(t, func(cfg *config.Config) { cfg.Blitz.AutoApprove = false }, start, text("started"))
	w := c.workspaces
	ctx := context.Background()
	dir := t.TempDir()

	id := approveAll(t, c, dir, "start it")
	mine := []string{id}
	list, err := w.ListProcesses(ctx, connect.NewRequest(&pb.ListProcessesRequest{Workspace: dir, SessionIds: mine}))
	require.NoError(t, err)
	require.Len(t, list.Msg.Processes, 1)
	p := list.Msg.Processes[0]
	assert.True(t, p.Running)
	var out *pb.GetProcessOutputResponse
	require.Eventually(t, func() bool {
		res, err := w.GetProcessOutput(ctx, connect.NewRequest(&pb.GetProcessOutputRequest{Workspace: dir, SessionIds: mine, Id: p.Id}))
		out = res.Msg
		return err == nil && out.Output != ""
	}, 10*time.Second, 20*time.Millisecond)
	assert.Contains(t, out.Output, "started")
	for name, call := range map[string]func() error{
		"output": func() error {
			return unary(w.GetProcessOutput, &pb.GetProcessOutputRequest{Workspace: dir, SessionIds: []string{"other"}, Id: p.Id})
		},
		"kill": func() error {
			return unary(w.KillProcess, &pb.KillProcessRequest{Workspace: dir, SessionIds: []string{"other"}, Id: p.Id})
		},
	} {
		t.Run("another session's "+name, func(t *testing.T) {
			assert.Equal(t, "UNKNOWN_PROCESS", reason(t, call()))
		})
	}
	killed, err := w.KillProcess(ctx, connect.NewRequest(&pb.KillProcessRequest{Workspace: dir, SessionIds: mine, Id: p.Id}))
	require.NoError(t, err)
	assert.Equal(t, p.Id, killed.Msg.Process.Id)
	_, err = w.TakeProcessNotices(ctx, connect.NewRequest(&pb.TakeProcessNoticesRequest{Workspace: dir, SessionId: id}))
	assert.NoError(t, err)
}

// searxng is a SearXNG instance answering every search with one page.
func searxng(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results":[{"title":"Go","url":"https://go.dev/doc","content":"docs"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// The workspace handlers answer from the workspace, and their documented
// failures arrive as reasons.
// The agent editors save, list and delete agent files in each scope, and
// a workspace offers its new agent at once.
func TestAgentFilesOverTheAPI(t *testing.T) {
	c, _ := serveEvery(t, nil) // with a home of its own
	home := os.Getenv("HOME")
	w := c.workspaces
	ctx := context.Background()
	dir := t.TempDir()
	const ws, user = pb.AgentScope_AGENT_SCOPE_WORKSPACE, pb.AgentScope_AGENT_SCOPE_USER

	quote := &pb.AgentDefinition{Name: "quote", Description: "Quotes", Tools: []string{"read_file"}, Temperature: new(0.4), MaxTokens: new(int32(512)), Prompt: "You write quotes."}
	saved, err := w.SaveAgentFile(ctx, connect.NewRequest(&pb.SaveAgentFileRequest{Workspace: dir, Scope: ws, Agent: quote}))
	require.NoError(t, err)
	require.Empty(t, saved.Msg.Problems)
	real, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(real, ".agents", "agents", "quote.md"), saved.Msg.File.Path)

	agentsNow, err := w.ListAgents(ctx, connect.NewRequest(&pb.ListAgentsRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.True(t, slices.ContainsFunc(agentsNow.Msg.Agents, func(a *pb.AgentInfo) bool { return a.Name == "quote" }), "offered at once")

	listed, err := w.ListAgentFiles(ctx, connect.NewRequest(&pb.ListAgentFilesRequest{Workspace: dir, Scope: ws}))
	require.NoError(t, err)
	require.Len(t, listed.Msg.Files, 1)
	got := listed.Msg.Files[0].Agent
	assert.Equal(t, 0.4, got.GetTemperature())
	assert.Equal(t, int32(512), got.GetMaxTokens())
	assert.Nil(t, got.TopP, "unset stays unset")

	bad, err := w.SaveAgentFile(ctx, connect.NewRequest(&pb.SaveAgentFileRequest{Workspace: dir, Scope: ws, Agent: &pb.AgentDefinition{Name: "blitz", Description: "d"}}))
	require.NoError(t, err)
	assert.NotEmpty(t, bad.Msg.Problems, "a built-in's name")
	assert.Nil(t, bad.Msg.File)

	// The user's agents need no workspace.
	_, err = w.SaveAgentFile(ctx, connect.NewRequest(&pb.SaveAgentFileRequest{Scope: user, Agent: &pb.AgentDefinition{Name: "mine", Description: "Mine"}}))
	require.NoError(t, err)
	mine, err := w.ListAgentFiles(ctx, connect.NewRequest(&pb.ListAgentFilesRequest{Scope: user}))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".blitz", "agents"), mine.Msg.Dir)
	require.Len(t, mine.Msg.Files, 1)

	_, err = w.DeleteAgentFile(ctx, connect.NewRequest(&pb.DeleteAgentFileRequest{Workspace: dir, Scope: ws, Name: "quote"}))
	require.NoError(t, err)
	code, info := errorReason(t, unary(w.DeleteAgentFile, &pb.DeleteAgentFileRequest{Workspace: dir, Scope: ws, Name: "quote"}))
	assert.Equal(t, connect.CodeNotFound, code)
	assert.Equal(t, "UNKNOWN_AGENT", info.Reason)
	assert.Error(t, unary(w.ListAgentFiles, &pb.ListAgentFilesRequest{}), "a scope is required")

	broken := filepath.Join(mine.Msg.Dir, "broken.md")
	require.NoError(t, os.WriteFile(broken, []byte("not an agent"), 0o644))
	_, err = w.DeleteAgentFile(ctx, connect.NewRequest(&pb.DeleteAgentFileRequest{Scope: user, Path: broken}))
	require.NoError(t, err, "a file that names no agent goes by its path")
	assert.NoFileExists(t, broken)
	assert.Error(t, unary(w.DeleteAgentFile, &pb.DeleteAgentFileRequest{Scope: user, Path: filepath.Join(dir, "x.md")}), "only the scope's files")

	all, err := w.ListTools(ctx, connect.NewRequest(&pb.ListToolsRequest{Workspace: dir, All: true}))
	require.NoError(t, err)
	active, err := w.ListTools(ctx, connect.NewRequest(&pb.ListToolsRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(all.Msg.Tools), len(active.Msg.Tools))
	assert.True(t, slices.ContainsFunc(all.Msg.Tools, func(t *pb.ToolInfo) bool { return t.Name == "universal_constructor" }), "tools the active agent lacks")
}

func TestWorkspaceHandlers(t *testing.T) {
	search := searxng(t)
	c, _ := serveEvery(t, func(cfg *config.Config) {
		cfg.Web.Enabled, cfg.Web.SearchProvider, cfg.Web.SearchURL, cfg.Web.AllowPrivate = true, "searxng", search, true
		cfg.Hooks.PromptSubmit = []config.HookConfig{{Command: "true"}}
	})
	w := c.workspaces
	ctx := context.Background()
	dir := t.TempDir()

	t.Run("agents and models", func(t *testing.T) {
		// The engine reports an unknown agent untyped, so it arrives as
		// INTERNAL rather than UNKNOWN_AGENT.
		assert.Error(t, unary(w.SetAgent, &pb.SetAgentRequest{Workspace: dir, Name: "nobody"}))
		a, err := w.SetAgent(ctx, connect.NewRequest(&pb.SetAgentRequest{Workspace: dir, Name: "qa"}))
		require.NoError(t, err)
		assert.True(t, a.Msg.Agent.Active)
		pin, err := w.SetModel(ctx, connect.NewRequest(&pb.SetModelRequest{Workspace: dir, Ref: "gemini/gemini-2.5-pro"}))
		require.NoError(t, err)
		assert.Empty(t, pin.Msg.ActivePin)
		_, err = w.PinModel(ctx, connect.NewRequest(&pb.PinModelRequest{Workspace: dir, Agent: "qa", Ref: "anthropic/claude-haiku-4-5"}))
		require.NoError(t, err)
		un, err := w.UnpinModel(ctx, connect.NewRequest(&pb.UnpinModelRequest{Workspace: dir, Agent: "qa"}))
		require.NoError(t, err)
		assert.Equal(t, "qa", un.Msg.Agent)
		assert.Equal(t, "UNKNOWN_AGENT", reason(t, unary(w.UnpinModel, &pb.UnpinModelRequest{Workspace: dir, Agent: "nobody"})))

		_, err = w.UpdateModelSettings(ctx, connect.NewRequest(&pb.UpdateModelSettingsRequest{Workspace: dir, Ref: "gpt-5", Changes: []*pb.Setting{
			{Key: "temperature", Value: "0.5"}, {Key: "max_tokens", Value: "100"}, {Key: "seed", Value: "7"}, {Key: "thinking_budget", Value: "64"},
		}}))
		require.NoError(t, err)
		all, err := w.GetModelSettings(ctx, connect.NewRequest(&pb.GetModelSettingsRequest{Workspace: dir}))
		require.NoError(t, err)
		require.Contains(t, all.Msg.All, "gpt-5")
		assert.Equal(t, int32(100), all.Msg.All["gpt-5"].GetMaxTokens())
		assert.Equal(t, int32(7), all.Msg.All["gpt-5"].GetSeed())
		one, err := w.GetModelSettings(ctx, connect.NewRequest(&pb.GetModelSettingsRequest{Workspace: dir, Ref: "openai/gpt-5"}))
		require.NoError(t, err)
		assert.Equal(t, int32(64), one.Msg.Model.Settings.GetThinkingBudget())
		assert.Equal(t, "BAD_MODEL_REF", reason(t, unary(w.GetModelSettings, &pb.GetModelSettingsRequest{Workspace: dir, Ref: "a=b"})))
		assert.Equal(t, "INVALID_SETTING", reason(t, unary(w.UpdateModelSettings, &pb.UpdateModelSettingsRequest{Workspace: dir, Ref: "gpt-5", Changes: []*pb.Setting{{Key: "nope", Value: "1"}}})))
	})

	t.Run("settings and rules", func(t *testing.T) {
		st, err := w.GetSettings(ctx, connect.NewRequest(&pb.GetSettingsRequest{Workspace: dir}))
		require.NoError(t, err)
		assert.NotEmpty(t, st.Msg.Model)
		key, err := w.SetSetting(ctx, connect.NewRequest(&pb.SetSettingRequest{Workspace: dir, Key: "Effort", Value: "low"}))
		require.NoError(t, err)
		assert.Equal(t, "effort", key.Msg.Key)
		assert.Equal(t, "UNKNOWN_SETTING", reason(t, unary(w.SetSetting, &pb.SetSettingRequest{Workspace: dir, Key: "nope", Value: "1"})))
		mode, err := w.SetPermissionMode(ctx, connect.NewRequest(&pb.SetPermissionModeRequest{Workspace: dir, Mode: "plan"}))
		require.NoError(t, err)
		assert.Equal(t, "plan", mode.Msg.Mode)
		assert.Equal(t, "UNKNOWN_MODE", reason(t, unary(w.SetPermissionMode, &pb.SetPermissionModeRequest{Workspace: dir, Mode: "yolo"})))
		for _, scope := range []string{"workspace", "global"} {
			t.Run(scope, func(t *testing.T) {
				added, err := w.AddPermissionRule(ctx, connect.NewRequest(&pb.AddPermissionRuleRequest{Workspace: dir, Effect: "ask", Rule: "shell(git push)", Save: true, Scope: scope}))
				require.NoError(t, err)
				assert.NotEmpty(t, added.Msg.Saved.Path, "saved to a file")
				removed, err := w.RemovePermissionRule(ctx, connect.NewRequest(&pb.RemovePermissionRuleRequest{Workspace: dir, Rule: "shell(git push)", Save: true, Scope: scope}))
				require.NoError(t, err)
				assert.Equal(t, int32(1), removed.Msg.Removed)
			})
		}
		assert.Equal(t, "BAD_RULE", reason(t, unary(w.AddPermissionRule, &pb.AddPermissionRuleRequest{Workspace: dir, Effect: "deny", Rule: "nope("})))
		rules, err := w.ListPermissionRules(ctx, connect.NewRequest(&pb.ListPermissionRulesRequest{Workspace: dir}))
		require.NoError(t, err)
		assert.NotEmpty(t, rules.Msg.Rules, "the built-in rules")
		cmds, err := w.ListCommands(ctx, connect.NewRequest(&pb.ListCommandsRequest{Workspace: dir}))
		require.NoError(t, err)
		assert.NotEmpty(t, cmds.Msg.Commands)
		styles, err := w.ListStyles(ctx, connect.NewRequest(&pb.ListStylesRequest{Workspace: dir}))
		require.NoError(t, err)
		assert.NotEmpty(t, styles.Msg.Styles)
		hooks, err := w.ListHooks(ctx, connect.NewRequest(&pb.ListHooksRequest{Workspace: dir}))
		require.NoError(t, err)
		assert.NotEmpty(t, hooks.Msg.Hooks, "the configured hook")
		sandbox, err := w.GetSandbox(ctx, connect.NewRequest(&pb.GetSandboxRequest{Workspace: dir}))
		require.NoError(t, err)
		assert.NotEmpty(t, sandbox.Msg.Summary)
	})

	t.Run("skills, tools and environments", func(t *testing.T) {
		skills, err := w.ListSkills(ctx, connect.NewRequest(&pb.ListSkillsRequest{Workspace: dir}))
		require.NoError(t, err)
		require.NotEmpty(t, skills.Msg.Skills, "the built-in skills")
		name := skills.Msg.Skills[0].Name
		found, err := w.ListSkills(ctx, connect.NewRequest(&pb.ListSkillsRequest{Workspace: dir, Query: name}))
		require.NoError(t, err)
		assert.NotEmpty(t, found.Msg.Skills)
		sk, err := w.GetSkill(ctx, connect.NewRequest(&pb.GetSkillRequest{Workspace: dir, Name: name}))
		require.NoError(t, err)
		assert.Equal(t, name, sk.Msg.Skill.Name)
		assert.Equal(t, "SKILL_NOT_FOUND", reason(t, unary(w.GetSkill, &pb.GetSkillRequest{Workspace: dir, Name: "nope"})))
		toolList, err := w.ListTools(ctx, connect.NewRequest(&pb.ListToolsRequest{Workspace: dir}))
		require.NoError(t, err)
		assert.NotEmpty(t, toolList.Msg.Tools)
		mcp, err := w.ListMCPServers(ctx, connect.NewRequest(&pb.ListMCPServersRequest{Workspace: dir}))
		require.NoError(t, err)
		assert.Empty(t, mcp.Msg.Servers)
		envs, err := w.ListEnvs(ctx, connect.NewRequest(&pb.ListEnvsRequest{Workspace: dir}))
		require.NoError(t, err)
		assert.Empty(t, envs.Msg.Envs)
		assert.NoError(t, unary(w.RemoveEnv, &pb.RemoveEnvRequest{Workspace: dir, Key: "nope"}), "nothing to remove")
		pruned, err := w.PruneEnvs(ctx, connect.NewRequest(&pb.PruneEnvsRequest{Workspace: dir}))
		require.NoError(t, err)
		assert.Zero(t, pruned.Msg.Removed)
	})

	t.Run("memory, notes and languages", func(t *testing.T) {
		added, err := w.AddMemory(ctx, connect.NewRequest(&pb.AddMemoryRequest{Workspace: dir, Text: "Run make test."}))
		require.NoError(t, err)
		assert.FileExists(t, added.Msg.Path)
		mem, err := w.ReloadMemory(ctx, connect.NewRequest(&pb.ReloadMemoryRequest{Workspace: dir}))
		require.NoError(t, err)
		assert.Contains(t, mem.Msg.Paths, added.Msg.Path)
		note, err := memory.SaveNote(memory.NotesDir(dir), memory.NoteFact, "The tests need Docker.")
		require.NoError(t, err)
		notes, err := w.ListNotes(ctx, connect.NewRequest(&pb.ListNotesRequest{Workspace: dir}))
		require.NoError(t, err)
		require.Len(t, notes.Msg.Notes, 1)
		assert.Equal(t, "The tests need Docker.", notes.Msg.Notes[0].Text)
		require.NoError(t, unary(w.ForgetNote, &pb.ForgetNoteRequest{Workspace: dir, Name: note.Name}))
		assert.Equal(t, "NO_NOTE", reason(t, unary(w.ForgetNote, &pb.ForgetNoteRequest{Workspace: dir, Name: note.Name})))

		locales, err := w.ListLocales(ctx, connect.NewRequest(&pb.ListLocalesRequest{Workspace: dir}))
		require.NoError(t, err)
		assert.NotEmpty(t, locales.Msg.Locales)
		set, err := w.SetLocale(ctx, connect.NewRequest(&pb.SetLocaleRequest{Workspace: dir, Input: "fr-CA"}))
		require.NoError(t, err)
		assert.Equal(t, "fr-CA", set.Msg.Tag)
		assert.Equal(t, "UNKNOWN_LOCALE", reason(t, unary(w.SetLocale, &pb.SetLocaleRequest{Workspace: dir, Input: "not a language at all"})))
	})

	t.Run("images", func(t *testing.T) {
		var buf bytes.Buffer
		require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 3))))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "a.png"), buf.Bytes(), 0o644))
		loaded, err := w.LoadImage(ctx, connect.NewRequest(&pb.LoadImageRequest{Workspace: dir, Path: "a.png"}))
		require.NoError(t, err)
		assert.Equal(t, int32(4), loaded.Msg.Image.Width)
		added, err := w.AddImage(ctx, connect.NewRequest(&pb.AddImageRequest{Workspace: dir, Name: "paste.png", Data: buf.Bytes()}))
		require.NoError(t, err)
		assert.Equal(t, loaded.Msg.Image.Id, added.Msg.Image.Id, "the same image")
		assert.Error(t, unary(w.LoadImage, &pb.LoadImageRequest{Workspace: dir, Path: "missing.png"}))
		assert.Error(t, unary(w.AddImage, &pb.AddImageRequest{Workspace: dir, Name: "x.png", Data: []byte("not an image")}))
	})

	t.Run("web search", func(t *testing.T) {
		p, err := w.GetSearchProvider(ctx, connect.NewRequest(&pb.GetSearchProviderRequest{Workspace: dir}))
		require.NoError(t, err)
		assert.Equal(t, "searxng", p.Msg.Provider)
		res, err := w.SearchWeb(ctx, connect.NewRequest(&pb.SearchWebRequest{Workspace: dir, Terms: "go docs"}))
		require.NoError(t, err)
		require.Len(t, res.Msg.Links, 1)
		assert.Equal(t, "https://go.dev/doc", res.Msg.Links[0].Url)
		assert.NotEmpty(t, res.Msg.Prompt)
	})

	t.Run("tasks", func(t *testing.T) {
		tasks, err := w.ListTasks(ctx, connect.NewRequest(&pb.ListTasksRequest{Workspace: dir}))
		require.NoError(t, err)
		assert.Empty(t, tasks.Msg.Tasks)
		reqs, err := w.ListTaskRequests(ctx, connect.NewRequest(&pb.ListTaskRequestsRequest{Workspace: dir}))
		require.NoError(t, err)
		assert.Empty(t, reqs.Msg.Requests)
		assert.Equal(t, "UNKNOWN_TASK", reason(t, unary(w.GetTask, &pb.GetTaskRequest{Workspace: dir, Id: "task-9"})))
		assert.Equal(t, "UNKNOWN_TASK", reason(t, unary(w.StopTask, &pb.StopTaskRequest{Workspace: dir, Id: "task-9"})))
		_, err = w.AuditShell(ctx, connect.NewRequest(&pb.AuditShellRequest{Workspace: dir, Command: "nosuch", ExitCode: -1, StartError: "not found"}))
		assert.NoError(t, err)
	})

	t.Run("git", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("no git")
		}
		assert.Equal(t, "GIT_FAILED", reason(t, unary(w.GetDiff, &pb.GetDiffRequest{Workspace: dir, Git: true})), "not a repository")
	})
}

// Without web access, the search handlers refuse with NO_FETCH.
func TestWebSearchNeedsFetch(t *testing.T) {
	c, _ := serveEvery(t, func(cfg *config.Config) { cfg.Web.Enabled = false })
	dir := t.TempDir()
	w := c.workspaces
	assert.Equal(t, "NO_FETCH", reason(t, unary(w.GetSearchProvider, &pb.GetSearchProviderRequest{Workspace: dir})))
	assert.Equal(t, "NO_FETCH", reason(t, unary(w.SearchWeb, &pb.SearchWebRequest{Workspace: dir, Terms: "x"})))
}

// A background task's life over WatchTasks: a watcher that joins late gets
// the active task and its waiting request before Ready, then the task's
// events as an Approve naming the workspace answers it, and a settings
// change; the task is read and listed, and stopping it once ended is
// harmless.
func TestWatchingBackgroundTasks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	start := call("invoke_agent", map[string]any{"agent_name": "qa", "prompt": "make a file", "background": true})
	create := call("create_file", map[string]any{"path": "made.txt", "content": "hi\n"})
	s := New(func(ctx context.Context, dir string) (*engine.Workspace, error) {
		cfg := config.DefaultConfig()
		cfg.Tools.WorkspaceDir = dir
		cfg.Session.StorageDir = t.TempDir()
		cfg.AgentModels = map[string]string{"qa": "gemini/qa-model"}
		return engine.Open(ctx, cfg, engine.Options{
			Model: runtime.NewMockLLM("gemini-3.8-flash", start, text("started")),
			NewModel: func(context.Context, *config.Config, string) (model.LLM, error) {
				return runtime.NewMockLLM("qa-model", create, text("made it")), nil
			},
		})
	})
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(func() { srv.Close(); s.Close() })
	sessions := pb.NewSessionServiceClient(http.DefaultClient, srv.URL)
	w := pb.NewWorkspaceServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()
	dir := t.TempDir()

	sess, err := sessions.NewSession(ctx, connect.NewRequest(&pb.NewSessionRequest{Workspace: dir}))
	require.NoError(t, err)
	id := sess.Msg.Session.Id
	require.NoError(t, streamErr(sessions.RunTurn(ctx, connect.NewRequest(&pb.RunTurnRequest{Workspace: dir, SessionId: id, Turn: &pb.Turn{Text: "go"}}))))
	mine := []string{id}
	require.Eventually(t, func() bool {
		res, err := w.ListTaskRequests(ctx, connect.NewRequest(&pb.ListTaskRequestsRequest{Workspace: dir, SessionIds: mine}))
		return err == nil && len(res.Msg.Requests) == 1
	}, 10*time.Second, 20*time.Millisecond, "the task never asked")

	watchCtx, stop := context.WithTimeout(ctx, 30*time.Second)
	defer stop()
	stream, err := w.WatchTasks(watchCtx, connect.NewRequest(&pb.WatchTasksRequest{Workspace: dir, SessionIds: mine}))
	require.NoError(t, err)
	defer stream.Close()
	var before []string
	var asked *pb.ApprovalRequest
	for stream.Receive() {
		ev := stream.Msg().Event
		if ev.GetReady() {
			break
		}
		switch {
		case ev.GetTask() != nil:
			before = append(before, "task:"+ev.GetTask().State)
		case ev.GetApprovalRequest() != nil:
			before = append(before, "request")
			asked = ev.GetApprovalRequest()
		}
	}
	require.NotNil(t, asked, "before ready: %v", before)
	assert.Equal(t, "task-1", asked.TaskId)
	assert.Contains(t, before, "request")

	_, err = sessions.Approve(ctx, connect.NewRequest(&pb.ApproveRequest{Workspace: dir, RequestId: asked.RequestId, Decision: pb.Decision_DECISION_ONCE}))
	require.NoError(t, err, "a task's request answered by the workspace")
	_, err = w.AddPermissionRule(ctx, connect.NewRequest(&pb.AddPermissionRuleRequest{Workspace: dir, Effect: "deny", Rule: "shell(rm)", Save: true, Scope: "workspace"}))
	require.NoError(t, err)
	resolved, done, changed := false, false, false
	for !(resolved && done && changed) && stream.Receive() {
		ev := stream.Msg().Event
		switch {
		case ev.GetResolved() == asked.RequestId:
			resolved = true
		case ev.GetTask().GetState() == "done":
			done = true
		case ev.GetSettingsChanged():
			changed = true
		}
	}
	assert.True(t, resolved, "resolved: %v", stream.Err())
	assert.True(t, done, "done: %v", stream.Err())
	assert.True(t, changed, "settings changed: %v", stream.Err())

	task, err := w.GetTask(ctx, connect.NewRequest(&pb.GetTaskRequest{Workspace: dir, SessionIds: mine, Id: "task-1"}))
	require.NoError(t, err)
	assert.Equal(t, "made it", task.Msg.Task.Result)
	list, err := w.ListTasks(ctx, connect.NewRequest(&pb.ListTasksRequest{Workspace: dir, SessionIds: mine}))
	require.NoError(t, err)
	assert.Len(t, list.Msg.Tasks, 1)
	stopped, err := w.StopTask(ctx, connect.NewRequest(&pb.StopTaskRequest{Workspace: dir, SessionIds: mine, Id: "task-1"}))
	require.NoError(t, err)
	assert.Equal(t, "done", stopped.Msg.Task.State)
	assert.Equal(t, "UNKNOWN_TASK", reason(t, unary(w.GetTask, &pb.GetTaskRequest{Workspace: dir, Id: "task-1"})), "no sessions named: none, not every one")
}
