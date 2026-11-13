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
	"path/filepath"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/terminal"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// terminalService implements TerminalService over the workspace's
// terminals (spec_062_terminal).
type terminalService struct{ s *Server }

// terminalHeartbeat is how often a terminal's stream says it's alive
// (TRM-12a); a variable for tests.
var terminalHeartbeat = 10 * time.Second

// terminalError maps the terminals' errors to Connect codes and reasons
// (TRM-15).
func terminalError(err error) error {
	switch {
	case errors.Is(err, terminal.ErrUnknown):
		return apiError(connect.CodeNotFound, "UNKNOWN_TERMINAL", err)
	case errors.Is(err, terminal.ErrTooMany):
		return apiError(connect.CodeResourceExhausted, "TOO_MANY_TERMINALS", err)
	case errors.Is(err, terminal.ErrUnsupported):
		return apiError(connect.CodeUnimplemented, "TERMINAL_UNSUPPORTED", err)
	case errors.Is(err, engine.ErrBadPath):
		return apiError(connect.CodeInvalidArgument, "BAD_PATH", err)
	case errors.Is(err, terminal.ErrClosed):
		return apiError(connect.CodeUnavailable, "SHUTTING_DOWN", err)
	case errors.Is(err, terminal.ErrNotReading):
		return apiError(connect.CodeDeadlineExceeded, "TERMINAL_NOT_READING", err)
	case errors.Is(err, terminal.ErrExited):
		return apiError(connect.CodeFailedPrecondition, "TERMINAL_EXITED", err)
	}
	return toAPI(err)
}

// terminalInfo is t's info for the window, its folder relative to dir.
func terminalInfo(dir string, i terminal.Info) *pb.TerminalInfo {
	rel, err := filepath.Rel(dir, i.Dir)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		rel = ""
	}
	return &pb.TerminalInfo{Id: i.ID, Dir: filepath.ToSlash(rel), Shell: i.Shell, Started: timestamp(i.Started), Exited: i.Exited, ExitCode: int32(i.ExitCode)}
}

// size is a requested size as a terminal's, 0 for its default.
func size(n uint32) uint16 {
	if n > 0xffff {
		return 0xffff
	}
	return uint16(n)
}

func (h terminalService) OpenTerminal(ctx context.Context, r req[pb.OpenTerminalRequest]) (*connect.Response[pb.OpenTerminalResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	t, err := w.OpenTerminal(r.Msg.Dir, size(r.Msg.Cols), size(r.Msg.Rows), h.s.version)
	if err != nil {
		return nil, terminalError(err)
	}
	return ok(&pb.OpenTerminalResponse{Terminal: terminalInfo(w.Dir(), t.Info())})
}

// terminal is the workspace's terminal id.
func (h terminalService) terminal(ctx context.Context, workspace, id string) (*terminal.Terminal, error) {
	w, err := h.s.workspace(ctx, workspace)
	if err != nil {
		return nil, err
	}
	t, err := w.Terminals().Get(id)
	if err != nil {
		return nil, terminalError(err)
	}
	return t, nil
}

func (h terminalService) WatchTerminal(ctx context.Context, r req[pb.WatchTerminalRequest], stream *connect.ServerStream[pb.WatchTerminalResponse]) error {
	t, err := h.terminal(ctx, r.Msg.Workspace, r.Msg.Id)
	if err != nil {
		return err
	}
	// An empty message at once and every terminalHeartbeat while nothing
	// else is sent, so the window knows the stream is alive and can tell
	// a broken one from a quiet shell (TRM-12a).
	var mu sync.Mutex
	send := func(m *pb.WatchTerminalResponse) error {
		mu.Lock()
		defer mu.Unlock()
		return stream.Send(m)
	}
	if err := send(&pb.WatchTerminalResponse{}); err != nil {
		return err
	}
	beat, stop := context.WithCancel(ctx)
	defer stop()
	go func() {
		tick := time.NewTicker(terminalHeartbeat)
		defer tick.Stop()
		for {
			select {
			case <-beat.Done():
				return
			case <-tick.C:
				if send(&pb.WatchTerminalResponse{}) != nil {
					stop()
					return
				}
			}
		}
	}()
	err = t.Watch(beat, r.Msg.From, func(c terminal.Chunk) error {
		return send(&pb.WatchTerminalResponse{Data: c.Data, Offset: c.Offset, Skipped: c.Skipped, Exited: c.Exited, ExitCode: int32(c.ExitCode)})
	})
	if errors.Is(err, context.Canceled) {
		return nil // the window went away; the terminal goes on (TRM-03)
	}
	return err
}

func (h terminalService) WriteTerminal(ctx context.Context, r req[pb.WriteTerminalRequest]) (*connect.Response[pb.WriteTerminalResponse], error) {
	t, err := h.terminal(ctx, r.Msg.Workspace, r.Msg.Id)
	if err != nil {
		return nil, err
	}
	if err := t.Write(r.Msg.Data); err != nil {
		if errors.Is(err, terminal.ErrNotReading) || errors.Is(err, terminal.ErrExited) {
			return nil, terminalError(err)
		}
		return nil, apiError(connect.CodeInvalidArgument, "INVALID_ARGUMENT", err)
	}
	return ok(&pb.WriteTerminalResponse{})
}

func (h terminalService) ResizeTerminal(ctx context.Context, r req[pb.ResizeTerminalRequest]) (*connect.Response[pb.ResizeTerminalResponse], error) {
	t, err := h.terminal(ctx, r.Msg.Workspace, r.Msg.Id)
	if err != nil {
		return nil, err
	}
	if err := t.Resize(size(r.Msg.Cols), size(r.Msg.Rows)); err != nil {
		return nil, apiError(connect.CodeInvalidArgument, "INVALID_ARGUMENT", err)
	}
	return ok(&pb.ResizeTerminalResponse{})
}

func (h terminalService) ListTerminals(ctx context.Context, r req[pb.ListTerminalsRequest]) (*connect.Response[pb.ListTerminalsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	res := &pb.ListTerminalsResponse{}
	for _, i := range w.Terminals().List() {
		res.Terminals = append(res.Terminals, terminalInfo(w.Dir(), i))
	}
	return ok(res)
}

func (h terminalService) CloseTerminal(ctx context.Context, r req[pb.CloseTerminalRequest]) (*connect.Response[pb.CloseTerminalResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.Terminals().Close(r.Msg.Id); err != nil {
		return nil, terminalError(err)
	}
	return ok(&pb.CloseTerminalResponse{})
}
