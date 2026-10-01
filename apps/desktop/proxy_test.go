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

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/apps/service/servicetest"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/socket"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// The page's API calls, streamed turns and approvals included, reach the
// service through the proxy.
func TestProxyReachesTheService(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	dir, err := os.MkdirTemp("/tmp", "cpd") // socket paths must be short
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "s.sock")

	create := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "create_file", Args: map[string]any{"path": "a.txt", "content": "x"}}}}}
	s := servicetest.New(func(ctx context.Context, d string) (*engine.Workspace, error) {
		cfg := config.DefaultConfig()
		cfg.Tools.WorkspaceDir = d
		cfg.Session.StorageDir = t.TempDir()
		return engine.Open(ctx, cfg, engine.Options{Model: runtime.NewMockLLM("m", create, genai.NewContentFromText("done", genai.RoleModel))})
	})
	l, err := socket.Listen(path)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	go servicetest.Serve(ctx, l, s.Handler(), time.Second)
	t.Cleanup(func() { cancel(); s.Close() })

	page := httptest.NewServer(serviceProxy(path))
	defer page.Close()
	sessions := pb.NewSessionServiceClient(http.DefaultClient, page.URL)
	ws := t.TempDir()
	sess, err := sessions.NewSession(context.Background(), connect.NewRequest(&pb.NewSessionRequest{Workspace: ws}))
	require.NoError(t, err, "through the proxy")
	stream, err := sessions.RunTurn(context.Background(), connect.NewRequest(&pb.RunTurnRequest{Workspace: ws, SessionId: sess.Msg.Session.Id, Turn: &pb.Turn{Text: "go"}}))
	require.NoError(t, err)
	var output string
	for stream.Receive() {
		ev := stream.Msg().Event
		if ar := ev.GetApprovalRequest(); ar != nil {
			_, err := sessions.Approve(context.Background(), connect.NewRequest(&pb.ApproveRequest{Workspace: ws, RequestId: ar.RequestId, Decision: pb.Decision_DECISION_ONCE}))
			assert.NoError(t, err, "approve")
		}
		if f := ev.GetFinished(); f != nil {
			output = f.Output
		}
	}
	err = stream.Err()
	require.NoError(t, err, "streamed turn: %q", output)
	require.Equal(t, "done", output, "streamed turn: %q %v", output, err)
	_, err = os.Stat(filepath.Join(ws, "a.txt"))
	assert.NoError(t, err, "approved write")

	// Only the API is forwarded.
	res, err := http.Get(page.URL + "/index.html")
	assert.NoError(t, err, "non-API path: %v", res)
	assert.Equal(t, http.StatusNotFound, res.StatusCode, "non-API path: %v %v", res, err)
}

// With no service, the page gets an error its client can read.
func TestProxyWithoutAService(t *testing.T) {
	page := httptest.NewServer(serviceProxy(filepath.Join(t.TempDir(), "none.sock")))
	defer page.Close()
	c := pb.NewWorkspaceServiceClient(http.DefaultClient, page.URL)
	_, err := c.GetModel(context.Background(), connect.NewRequest(&pb.GetModelRequest{Workspace: "/x"}))
	assert.Equal(t, connect.CodeUnavailable, connect.CodeOf(err), "err %v (code %v)", err, connect.CodeOf(err))
}

// frame is a Connect stream message: flags, length, payload.
func frame(flags byte, payload string) []byte {
	b := make([]byte, 5, 5+len(payload))
	b[0] = flags
	binary.BigEndian.PutUint32(b[1:], uint32(len(payload)))
	return append(b, payload...)
}

// messages splits a Connect stream into its messages' payloads, "(end)"
// marking the end-of-stream message.
func messages(t *testing.T, stream []byte) []string {
	t.Helper()
	var out []string
	for len(stream) > 0 {
		require.GreaterOrEqual(t, len(stream), 5, "a message cut short")
		n := int(binary.BigEndian.Uint32(stream[1:5]))
		payload := string(stream[5 : 5+n])
		if stream[0]&2 != 0 {
			payload = "(end)" + payload
		}
		out = append(out, payload)
		stream = stream[5+n:]
	}
	return out
}

// A quiet Connect stream gets empty messages, never inside a message or
// after the end, so a web view that holds back data before a pause gets it.
func TestKeepalive(t *testing.T) {
	const interval = 20 * time.Millisecond
	for _, tc := range []struct {
		name  string
		write func(w io.Writer)
		want  []string
	}{
		{
			name: "a pause between messages",
			write: func(w io.Writer) {
				w.Write(frame(0, `{"event":1}`))
				time.Sleep(3 * interval)
				w.Write(frame(2, `{}`))
			},
			want: []string{`{"event":1}`, "{}", "(end){}"},
		},
		{
			name: "a message arriving in pieces",
			write: func(w io.Writer) {
				m := frame(0, `{"event":"long"}`)
				w.Write(m[:3])
				time.Sleep(3 * interval)
				w.Write(m[3:9])
				time.Sleep(3 * interval)
				w.Write(m[9:])
				w.Write(frame(2, `{}`))
			},
			want: []string{"{}", `{"event":"long"}`, "(end){}"},
		},
		{
			name: "quiet after the end",
			write: func(w io.Writer) {
				w.Write(frame(0, `{"event":1}`))
				w.Write(frame(2, `{}`))
				time.Sleep(5 * interval)
			},
			want: []string{`{"event":1}`, "(end){}"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, w := io.Pipe()
			go func() { tc.write(w); w.Close() }()
			empty, ok := emptyMessage("application/connect+json")
			require.True(t, ok)
			got, err := io.ReadAll(newKeepalive(r, interval, empty))
			require.NoError(t, err)
			msgs := messages(t, got)
			// Timing decides how many empty messages a pause gets; one or more.
			var squashed []string
			for _, m := range msgs {
				if m == "{}" && len(squashed) > 0 && squashed[len(squashed)-1] == "{}" {
					continue
				}
				squashed = append(squashed, m)
			}
			assert.Equal(t, tc.want, squashed)
		})
	}
}

// Only Connect streams get empty messages, in their own codec.
func TestEmptyMessage(t *testing.T) {
	for _, tc := range []struct {
		contentType string
		want        []byte
		ok          bool
	}{
		{"application/connect+json", frame(0, "{}"), true},
		{"application/connect+json; charset=utf-8", frame(0, "{}"), true},
		{"application/connect+proto", frame(0, ""), true},
		{"application/json", nil, false},
		{"text/html", nil, false},
	} {
		t.Run(tc.contentType, func(t *testing.T) {
			got, ok := emptyMessage(tc.contentType)
			assert.Equal(t, tc.ok, ok)
			assert.True(t, bytes.Equal(tc.want, got), "%q", got)
		})
	}
}

// A stream cut in the middle of a message ends with an error, not a
// partial message.
func TestKeepaliveCutMessage(t *testing.T) {
	m := frame(0, `{"event":"long"}`)
	empty, ok := emptyMessage("application/connect+json")
	require.True(t, ok)
	got, err := io.ReadAll(newKeepalive(io.NopCloser(bytes.NewReader(m[:9])), time.Hour, empty))
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
	assert.Empty(t, got)
}
