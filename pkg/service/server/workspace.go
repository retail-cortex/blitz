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
	"errors"
	"log/slog"
	"os"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/observability"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// workspaceService implements WorkspaceService.
type workspaceService struct{ s *Server }

type req[T any] = *connect.Request[T]

func ok[T any](msg *T) (*connect.Response[T], error) { return connect.NewResponse(msg), nil }

func (h workspaceService) GetServiceInfo(context.Context, req[pb.GetServiceInfoRequest]) (*connect.Response[pb.GetServiceInfoResponse], error) {
	exe, _ := os.Executable()
	return ok(&pb.GetServiceInfoResponse{Version: h.s.version, Executable: exe, Started: timestamppb.New(h.s.started), Pid: int32(os.Getpid()), Replaced: replaced(h.s.program, exe),
		SettingsChanged: config.SettingsStamp(config.GlobalSettingsFiles(h.s.configDir)) != h.s.settings})
}

// WithShutdown is what Shutdown calls to stop the service; without it the
// service can't be stopped that way.
func WithShutdown(stop func()) Option { return func(s *Server) { s.shutdown = stop } }

func (h workspaceService) Shutdown(context.Context, req[pb.ShutdownRequest]) (*connect.Response[pb.ShutdownResponse], error) {
	if h.s.shutdown == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("this service can't be stopped by a request"))
	}
	slog.Info("exit: a client asked")
	// After the response is on its way: the stop drains requests in progress.
	time.AfterFunc(shutdownDelay, h.s.shutdown)
	return ok(&pb.ShutdownResponse{})
}

// shutdownDelay lets Shutdown's response go out before the service stops.
var shutdownDelay = 50 * time.Millisecond

// WithLogDir is the directory of the service's diagnostic log, which
// ListLogDays and ReadLog read ("": none).
func WithLogDir(dir string) Option { return func(s *Server) { s.logDir = dir } }

func (h workspaceService) ListLogDays(context.Context, req[pb.ListLogDaysRequest]) (*connect.Response[pb.ListLogDaysResponse], error) {
	if h.s.logDir == "" {
		return ok(&pb.ListLogDaysResponse{})
	}
	days, err := observability.LogDays(h.s.logDir)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return ok(&pb.ListLogDaysResponse{Days: days, Dir: h.s.logDir})
}

func (h workspaceService) ReadLog(_ context.Context, r req[pb.ReadLogRequest]) (*connect.Response[pb.ReadLogResponse], error) {
	if h.s.logDir == "" {
		return ok(&pb.ReadLogResponse{})
	}
	page, err := observability.ReadLog(h.s.logDir, observability.LogQuery{Day: r.Msg.Day, MinLevel: r.Msg.MinLevel, Text: r.Msg.Text, Limit: int(r.Msg.Limit)})
	if err != nil {
		return nil, invalid(err)
	}
	out := &pb.ReadLogResponse{Day: page.Day, Path: page.Path, Matched: int32(page.Matched)}
	for _, e := range page.Entries {
		entry := &pb.LogEntry{Time: timestamppb.New(e.Time), Level: e.Level, Message: e.Msg}
		for _, a := range e.Attrs {
			entry.Attrs = append(entry.Attrs, &pb.LogAttr{Key: a.Key, Value: a.Value})
		}
		out.Entries = append(out.Entries, entry)
	}
	return ok(out)
}

func (h workspaceService) DeleteLogDay(_ context.Context, r req[pb.DeleteLogDayRequest]) (*connect.Response[pb.DeleteLogDayResponse], error) {
	if h.s.logDir == "" {
		return nil, apiError(connect.CodeNotFound, "LOG_NOT_FOUND", observability.ErrNoLogDay, "day", r.Msg.Day)
	}
	switch err := observability.DeleteLogDay(h.s.logDir, r.Msg.Day, time.Now()); {
	case errors.Is(err, observability.ErrLogInUse):
		return nil, apiError(connect.CodeFailedPrecondition, "LOG_IN_USE", err, "day", r.Msg.Day)
	case errors.Is(err, observability.ErrNoLogDay):
		return nil, apiError(connect.CodeNotFound, "LOG_NOT_FOUND", err, "day", r.Msg.Day)
	case errors.Is(err, observability.ErrBadLogDay):
		return nil, invalid(err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return ok(&pb.DeleteLogDayResponse{})
}

// programFile is the service's executable file, as it is now (nil if it
// can't be found).
func programFile() os.FileInfo {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	fi, _ := os.Stat(exe)
	return fi
}

// replaced reports whether the file at exe is no longer the program as it
// was (was, from programFile): installing a package or a new build puts a
// new file there, or removes it. Unknown (was nil) isn't replaced.
func replaced(was os.FileInfo, exe string) bool {
	if was == nil {
		return false
	}
	now, err := os.Stat(exe)
	return err != nil || !os.SameFile(was, now)
}

func (h workspaceService) ListWorkspaces(context.Context, req[pb.ListWorkspacesRequest]) (*connect.Response[pb.ListWorkspacesResponse], error) {
	return ok(&pb.ListWorkspacesResponse{Workspaces: h.s.openDirs()})
}

func (h workspaceService) CloseWorkspace(_ context.Context, r req[pb.CloseWorkspaceRequest]) (*connect.Response[pb.CloseWorkspaceResponse], error) {
	if err := h.s.closeWorkspace(r.Msg.Workspace); err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.CloseWorkspaceResponse{})
}

func (h workspaceService) GetSandbox(ctx context.Context, r req[pb.GetSandboxRequest]) (*connect.Response[pb.GetSandboxResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	return ok(&pb.GetSandboxResponse{Summary: w.SandboxSummary()})
}

func (h workspaceService) GetProjectSettings(ctx context.Context, r req[pb.GetProjectSettingsRequest]) (*connect.Response[pb.GetProjectSettingsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	return ok(&pb.GetProjectSettingsResponse{Settings: projectMsg(w.ProjectSettings())})
}

func (h workspaceService) TrustProject(ctx context.Context, r req[pb.TrustProjectRequest]) (*connect.Response[pb.TrustProjectResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.TrustProject(r.Msg.Hash, r.Msg.Trusted); err != nil {
		return nil, toAPI(err)
	}
	// Reopened on the next call, with the decision.
	reopened := h.s.closeWorkspace(r.Msg.Workspace) == nil
	return ok(&pb.TrustProjectResponse{Reopened: reopened})
}

func (h workspaceService) ForgetProjectTrust(ctx context.Context, r req[pb.ForgetProjectTrustRequest]) (*connect.Response[pb.ForgetProjectTrustResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.ForgetProjectTrust(); err != nil {
		return nil, toAPI(err)
	}
	reopened := h.s.closeWorkspace(r.Msg.Workspace) == nil
	return ok(&pb.ForgetProjectTrustResponse{Reopened: reopened})
}
