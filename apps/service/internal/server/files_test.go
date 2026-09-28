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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileService(t *testing.T) {
	_, s := serve(t, nil, text("hi"))
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	files := pb.NewFileServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()
	dir := t.TempDir()

	w, err := files.WriteFile(ctx, connect.NewRequest(&pb.WriteFileRequest{Workspace: dir, Path: "src/a.go", Text: "package a\n"}))
	require.NoError(t, err)
	l, err := files.ListDir(ctx, connect.NewRequest(&pb.ListDirRequest{Workspace: dir}))
	require.NoError(t, err, "list: %v", l)
	require.Len(t, l.Msg.Entries, 1, "list: %v %v", l, err)
	require.Equal(t, pb.FileKind_FILE_KIND_FOLDER, l.Msg.Entries[0].Kind, "list: %v %v", l, err)
	require.Equal(t, "src", l.Msg.Entries[0].Path, "list: %v %v", l, err)
	f, err := files.ReadFile(ctx, connect.NewRequest(&pb.ReadFileRequest{Workspace: dir, Path: "src/a.go"}))
	require.NoError(t, err, "read: %v", f)
	require.Equal(t, "package a\n", f.Msg.Text, "read: %v %v", f, err)
	require.Equal(t, w.Msg.Version, f.Msg.Version, "read: %v %v", f, err)

	// Changed elsewhere: FILE_CHANGED, with the current version.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src", "a.go"), []byte("package b\n"), 0o644))
	st, _ := files.StatFiles(ctx, connect.NewRequest(&pb.StatFilesRequest{Workspace: dir, Paths: []string{"src/a.go"}}))
	_, err = files.WriteFile(ctx, connect.NewRequest(&pb.WriteFileRequest{Workspace: dir, Path: "src/a.go", Text: "mine", Version: w.Msg.Version}))
	code, info := errorReason(t, err)
	assert.Equal(t, connect.CodeFailedPrecondition, code, "stale write: %v %v", code, info)
	assert.Equal(t, "FILE_CHANGED", info.Reason, "stale write: %v %v", code, info)
	assert.Equal(t, st.Msg.Versions["src/a.go"], info.Metadata["current_version"], "stale write: %v %v", code, info)

	for name, call := range map[string]struct {
		err    error
		reason string
	}{
		"outside": {func() error {
			_, err := files.ReadFile(ctx, connect.NewRequest(&pb.ReadFileRequest{Workspace: dir, Path: "../x"}))
			return err
		}(), "BAD_PATH"},
		"missing": {func() error {
			_, err := files.ReadFile(ctx, connect.NewRequest(&pb.ReadFileRequest{Workspace: dir, Path: "nope.txt"}))
			return err
		}(), "FILE_NOT_FOUND"},
		"exists": {func() error {
			_, err := files.CreateFolder(ctx, connect.NewRequest(&pb.CreateFolderRequest{Workspace: dir, Path: "src"}))
			return err
		}(), "FILE_EXISTS"},
	} {
		t.Run(name, func(t *testing.T) {
			_, info := errorReason(t, call.err)
			assert.Equal(t, call.reason, info.Reason, "%s: %v", name, info)
		})
	}

	_, err = files.RenameFile(ctx, connect.NewRequest(&pb.RenameFileRequest{Workspace: dir, From: "src/a.go", To: "lib/a.go"}))
	require.NoError(t, err)
	found, _ := files.FindFiles(ctx, connect.NewRequest(&pb.FindFilesRequest{Workspace: dir, Query: "la"}))
	assert.Len(t, found.Msg.Paths, 1, "find: %v", found.Msg.Paths)
	assert.Equal(t, "lib/a.go", found.Msg.Paths[0], "find: %v", found.Msg.Paths)
	_, err = files.DeleteFile(ctx, connect.NewRequest(&pb.DeleteFileRequest{Workspace: dir, Path: "lib"}))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(dir, "lib"))
	assert.ErrorIs(t, err, fs.ErrNotExist, "not deleted")
}
