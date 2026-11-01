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
	"bufio"
	"context"
	"io"
	"io/fs"
	"net"
	"net/http"
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

// socketDir returns a short directory: Unix socket paths are limited to
// about 104 bytes, and macOS temp directories are long.
func socketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "cp")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestListenIsPrivateAndSingle(t *testing.T) {
	path := filepath.Join(socketDir(t), "run", "s.sock")
	l, err := socket.Listen(path)
	require.NoError(t, err)
	fi, _ := os.Stat(path)
	assert.Equal(t, fs.FileMode(0o600), fi.Mode().Perm(), "socket mode %v", fi.Mode().Perm())
	fi, _ = os.Stat(filepath.Dir(path))
	assert.Equal(t, fs.FileMode(0o700), fi.Mode().Perm(), "directory mode %v", fi.Mode().Perm())
	_, secondErr := socket.Listen(path)
	assert.ErrorIs(t, secondErr, socket.ErrRunning, "a second service on the socket")
	l.Close()

	// A socket left behind by a service that died is replaced.
	os.WriteFile(path, nil, 0o600)
	l, err = socket.Listen(path)
	require.NoError(t, err, "stale socket")
	l.Close()
}

// A connection a client leaves open with nothing to ask is closed, so
// clients that drop theirs can't pile them up in the service.
func TestServeClosesIdleConnections(t *testing.T) {
	old := idleTimeout
	idleTimeout = 100 * time.Millisecond
	t.Cleanup(func() { idleTimeout = old })
	path := filepath.Join(socketDir(t), "s.sock")
	l, err := socket.Listen(path)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = Serve(ctx, l, New(nil).Handler(), time.Second) }()

	conn, err := net.Dial("unix", path)
	require.NoError(t, err)
	defer conn.Close()
	_, err = io.WriteString(conn, "POST /blitz.v1.WorkspaceService/GetServiceInfo HTTP/1.1\r\nHost: blitz\r\nContent-Type: application/json\r\nContent-Length: 2\r\n\r\n{}")
	require.NoError(t, err)
	res, err := http.ReadResponse(bufio.NewReader(conn), nil)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, res.Body)
	res.Body.Close()
	assert.Equal(t, http.StatusOK, res.StatusCode)

	// Kept open after the answer, the connection is closed once idle.
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, err = conn.Read(make([]byte, 1))
	assert.ErrorIs(t, err, io.EOF, "the service closed the idle connection")
}

func TestServeOverTheSocket(t *testing.T) {
	_, s := serve(t, nil)
	path := filepath.Join(socketDir(t), "s.sock")
	l, err := socket.Listen(path)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, l, s.Handler(), time.Second) }()

	c := pb.NewWorkspaceServiceClient(socket.Client(path), socket.BaseURL)
	res, err := c.GetModel(context.Background(), connect.NewRequest(&pb.GetModelRequest{Workspace: t.TempDir()}))
	require.NoError(t, err, "over the socket: %v", res)
	require.NotEqual(t, "", res.Msg.Name, "over the socket: %v %v", res, err)
	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err, "serve")
	case <-time.After(5 * time.Second):
		t.Fatal("serve didn't stop")
	}
}

// ServePlain answers HTTP/1.1 and cleartext HTTP/2 (h2c), as a front end
// that ends TLS itself sends either, and stops with its context.
func TestServePlain(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ServePlain(ctx, l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, r.Proto)
		}), time.Second)
	}()
	url := "http://" + l.Addr().String() + "/"
	for name, protocols := range map[string]func(*http.Protocols){
		"HTTP/1.1": func(p *http.Protocols) { p.SetHTTP1(true) },
		"HTTP/2.0": func(p *http.Protocols) { p.SetUnencryptedHTTP2(true) },
	} {
		tr := &http.Transport{Protocols: new(http.Protocols)}
		protocols(tr.Protocols)
		res, err := (&http.Client{Transport: tr}).Get(url)
		require.NoError(t, err, name)
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		assert.Equal(t, name, string(body))
		tr.CloseIdleConnections()
	}
	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("ServePlain didn't stop")
	}
}
