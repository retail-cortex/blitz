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
	"io/fs"
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
