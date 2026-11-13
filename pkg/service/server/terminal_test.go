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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/terminal"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readTerminal reads a terminal's stream from from until its output holds
// want (or, with want "", until the shell exits), at most ten seconds.
func readTerminal(t *testing.T, c pb.TerminalServiceClient, dir, id string, from int64, want string) (string, *pb.WatchTerminalResponse) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := c.WatchTerminal(ctx, connect.NewRequest(&pb.WatchTerminalRequest{Workspace: dir, Id: id, From: from}))
	require.NoError(t, err)
	defer stream.Close()
	var b strings.Builder
	var last *pb.WatchTerminalResponse
	for stream.Receive() {
		last = stream.Msg()
		b.Write(last.Data)
		if (want != "" && strings.Contains(b.String(), want)) || last.Exited {
			return b.String(), last
		}
	}
	require.NoError(t, stream.Err(), "waited for %q, got %q", want, b.String())
	return b.String(), last
}

func TestTerminalService(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no PTYs here yet (TRM-50)")
	}
	t.Setenv("SHELL", "/bin/sh") // the login shell, not the machine's
	_, s := serve(t, nil, text("hi"))
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	c := pb.NewTerminalServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.md"), nil, 0o644))

	open, err := c.OpenTerminal(ctx, connect.NewRequest(&pb.OpenTerminalRequest{Workspace: dir, Dir: "src", Cols: 80, Rows: 24}))
	require.NoError(t, err)
	term := open.Msg.Terminal
	assert.Equal(t, "src", term.Dir)
	assert.Equal(t, "/bin/sh", term.Shell)
	assert.False(t, term.Exited)

	_, err = c.WriteTerminal(ctx, connect.NewRequest(&pb.WriteTerminalRequest{Workspace: dir, Id: term.Id, Data: []byte("echo hello-$((40+2)); pwd\n")}))
	require.NoError(t, err)
	out, _ := readTerminal(t, c, dir, term.Id, 0, "hello-42")
	assert.Contains(t, out, "hello-42")

	_, err = c.ResizeTerminal(ctx, connect.NewRequest(&pb.ResizeTerminalRequest{Workspace: dir, Id: term.Id, Cols: 100, Rows: 40}))
	require.NoError(t, err)
	list, err := c.ListTerminals(ctx, connect.NewRequest(&pb.ListTerminalsRequest{Workspace: dir}))
	require.NoError(t, err)
	require.Len(t, list.Msg.Terminals, 1)
	assert.Equal(t, term.Id, list.Msg.Terminals[0].Id)

	_, err = c.WriteTerminal(ctx, connect.NewRequest(&pb.WriteTerminalRequest{Workspace: dir, Id: term.Id, Data: []byte("stty size; exit 5\n")}))
	require.NoError(t, err)
	out, last := readTerminal(t, c, dir, term.Id, 0, "")
	assert.Contains(t, out, "40 100")
	assert.Contains(t, out, filepath.Join("src"), "it started in its folder")
	require.NotNil(t, last)
	assert.True(t, last.Exited)
	assert.Equal(t, int32(5), last.ExitCode)

	_, err = c.CloseTerminal(ctx, connect.NewRequest(&pb.CloseTerminalRequest{Workspace: dir, Id: term.Id}))
	require.NoError(t, err)

	other, err := c.OpenTerminal(ctx, connect.NewRequest(&pb.OpenTerminalRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.Empty(t, other.Msg.Terminal.Dir, "the workspace itself")

	for name, tc := range map[string]struct {
		call   func() error
		reason string
	}{
		"outside the workspace": {func() error {
			_, err := c.OpenTerminal(ctx, connect.NewRequest(&pb.OpenTerminalRequest{Workspace: dir, Dir: "../elsewhere"}))
			return err
		}, "BAD_PATH"},
		"a folder that isn't there": {func() error {
			_, err := c.OpenTerminal(ctx, connect.NewRequest(&pb.OpenTerminalRequest{Workspace: dir, Dir: "missing"}))
			return err
		}, "BAD_PATH"},
		"a file": {func() error {
			_, err := c.OpenTerminal(ctx, connect.NewRequest(&pb.OpenTerminalRequest{Workspace: dir, Dir: "notes.md"}))
			return err
		}, "BAD_PATH"},
		"closed": {func() error {
			_, err := c.CloseTerminal(ctx, connect.NewRequest(&pb.CloseTerminalRequest{Workspace: dir, Id: term.Id}))
			return err
		}, "UNKNOWN_TERMINAL"},
		"write to none": {func() error {
			_, err := c.WriteTerminal(ctx, connect.NewRequest(&pb.WriteTerminalRequest{Workspace: dir, Id: "term-x", Data: []byte("x")}))
			return err
		}, "UNKNOWN_TERMINAL"},
		"resize none": {func() error {
			_, err := c.ResizeTerminal(ctx, connect.NewRequest(&pb.ResizeTerminalRequest{Workspace: dir, Id: "term-x", Cols: 1, Rows: 1}))
			return err
		}, "UNKNOWN_TERMINAL"},
		"too small": {func() error {
			_, err := c.ResizeTerminal(ctx, connect.NewRequest(&pb.ResizeTerminalRequest{Workspace: dir, Id: other.Msg.Terminal.Id}))
			return err
		}, "INVALID_ARGUMENT"},
		"too much at once": {func() error {
			_, err := c.WriteTerminal(ctx, connect.NewRequest(&pb.WriteTerminalRequest{Workspace: dir, Id: other.Msg.Terminal.Id, Data: make([]byte, terminal.MaxWrite+1)}))
			return err
		}, "INVALID_ARGUMENT"},
		"watch none": {func() error {
			stream, err := c.WatchTerminal(ctx, connect.NewRequest(&pb.WatchTerminalRequest{Workspace: dir, Id: "term-x"}))
			if err != nil {
				return err
			}
			defer stream.Close()
			for stream.Receive() {
			}
			return stream.Err()
		}, "UNKNOWN_TERMINAL"},
	} {
		t.Run(name, func(t *testing.T) {
			_, info := errorReason(t, tc.call())
			assert.Equal(t, tc.reason, info.Reason)
		})
	}

	// A window that goes away leaves the terminal running.
	wctx, cancel := context.WithCancel(ctx)
	stream, err := c.WatchTerminal(wctx, connect.NewRequest(&pb.WatchTerminalRequest{Workspace: dir, Id: other.Msg.Terminal.Id}))
	require.NoError(t, err)
	cancel()
	for stream.Receive() {
	}
	_ = stream.Close()
	list, err = c.ListTerminals(ctx, connect.NewRequest(&pb.ListTerminalsRequest{Workspace: dir}))
	require.NoError(t, err)
	require.Len(t, list.Msg.Terminals, 1)
	assert.False(t, list.Msg.Terminals[0].Exited)
}

func TestTerminalErrors(t *testing.T) {
	for _, tc := range []struct {
		err    error
		code   connect.Code
		reason string
	}{
		{terminal.ErrUnknown, connect.CodeNotFound, "UNKNOWN_TERMINAL"},
		{terminal.ErrTooMany, connect.CodeResourceExhausted, "TOO_MANY_TERMINALS"},
		{terminal.ErrUnsupported, connect.CodeUnimplemented, "TERMINAL_UNSUPPORTED"},
		{engine.ErrBadPath, connect.CodeInvalidArgument, "BAD_PATH"},
		{terminal.ErrClosed, connect.CodeUnavailable, "SHUTTING_DOWN"},
		{terminal.ErrNotReading, connect.CodeDeadlineExceeded, "TERMINAL_NOT_READING"},
		{terminal.ErrExited, connect.CodeFailedPrecondition, "TERMINAL_EXITED"},
	} {
		code, info := errorReason(t, terminalError(tc.err))
		assert.Equal(t, tc.code, code, "%v", tc.err)
		assert.Equal(t, tc.reason, info.Reason)
	}
	assert.Error(t, terminalError(errors.New("other")))
	assert.Equal(t, uint16(0xffff), size(1<<20))
	assert.Empty(t, terminalInfo("/w", terminal.Info{Dir: "/elsewhere"}).Dir, "a folder outside reads as the workspace")
}

// A terminal's stream says it's alive at once and every terminalHeartbeat
// while the shell is quiet, with empty messages, so the window can tell a
// broken stream from a quiet shell (TRM-12a).
func TestTerminalHeartbeat(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no PTYs here yet (TRM-50)")
	}
	old := terminalHeartbeat
	terminalHeartbeat = 30 * time.Millisecond
	t.Cleanup(func() { terminalHeartbeat = old })
	t.Setenv("SHELL", "/bin/sh")
	_, s := serve(t, nil, text("hi"))
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	c := pb.NewTerminalServiceClient(http.DefaultClient, srv.URL)
	dir := t.TempDir()
	open, err := c.OpenTerminal(context.Background(), connect.NewRequest(&pb.OpenTerminalRequest{Workspace: dir, Cols: 80, Rows: 24}))
	require.NoError(t, err)
	id := open.Msg.Terminal.Id
	// Past the prompt: from where the output is now, the shell is quiet.
	out, last := readTerminal(t, c, dir, id, 0, "$")
	require.NotEmpty(t, out)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := c.WatchTerminal(ctx, connect.NewRequest(&pb.WatchTerminalRequest{Workspace: dir, Id: id, From: last.Offset}))
	require.NoError(t, err)
	defer stream.Close()
	beats := 0
	for beats < 3 && stream.Receive() {
		m := stream.Msg()
		if len(m.Data) == 0 && !m.Exited && m.Skipped == 0 {
			beats++
		}
	}
	require.NoError(t, stream.Err())
	assert.Equal(t, 3, beats, "the first at once, then one a beat")

	// Output still arrives between the beats.
	_, err = c.WriteTerminal(context.Background(), connect.NewRequest(&pb.WriteTerminalRequest{Workspace: dir, Id: id, Data: []byte("echo beat-$((1+1))\n")}))
	require.NoError(t, err)
	var got strings.Builder
	for stream.Receive() && !strings.Contains(got.String(), "beat-2") {
		got.Write(stream.Msg().Data)
	}
	assert.Contains(t, got.String(), "beat-2")
}
