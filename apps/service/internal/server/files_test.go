package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

func TestFileService(t *testing.T) {
	_, s := serve(t, nil, text("hi"))
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	files := pb.NewFileServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()
	dir := t.TempDir()

	w, err := files.WriteFile(ctx, connect.NewRequest(&pb.WriteFileRequest{Workspace: dir, Path: "src/a.go", Text: "package a\n"}))
	if err != nil {
		t.Fatal(err)
	}
	l, err := files.ListDir(ctx, connect.NewRequest(&pb.ListDirRequest{Workspace: dir}))
	if err != nil || len(l.Msg.Entries) != 1 || l.Msg.Entries[0].Kind != pb.FileKind_FILE_KIND_FOLDER || l.Msg.Entries[0].Path != "src" {
		t.Fatalf("list: %v %v", l, err)
	}
	f, err := files.ReadFile(ctx, connect.NewRequest(&pb.ReadFileRequest{Workspace: dir, Path: "src/a.go"}))
	if err != nil || f.Msg.Text != "package a\n" || f.Msg.Version != w.Msg.Version {
		t.Fatalf("read: %v %v", f, err)
	}

	// Changed elsewhere: FILE_CHANGED, with the current version.
	if err := os.WriteFile(filepath.Join(dir, "src", "a.go"), []byte("package b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, _ := files.StatFiles(ctx, connect.NewRequest(&pb.StatFilesRequest{Workspace: dir, Paths: []string{"src/a.go"}}))
	_, err = files.WriteFile(ctx, connect.NewRequest(&pb.WriteFileRequest{Workspace: dir, Path: "src/a.go", Text: "mine", Version: w.Msg.Version}))
	code, info := errorReason(t, err)
	if code != connect.CodeFailedPrecondition || info.Reason != "FILE_CHANGED" || info.Metadata["current_version"] != st.Msg.Versions["src/a.go"] {
		t.Errorf("stale write: %v %v", code, info)
	}

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
		if _, info := errorReason(t, call.err); info.Reason != call.reason {
			t.Errorf("%s: %v", name, info)
		}
	}

	if _, err := files.RenameFile(ctx, connect.NewRequest(&pb.RenameFileRequest{Workspace: dir, From: "src/a.go", To: "lib/a.go"})); err != nil {
		t.Fatal(err)
	}
	found, _ := files.FindFiles(ctx, connect.NewRequest(&pb.FindFilesRequest{Workspace: dir, Query: "la"}))
	if len(found.Msg.Paths) != 1 || found.Msg.Paths[0] != "lib/a.go" {
		t.Errorf("find: %v", found.Msg.Paths)
	}
	if _, err := files.DeleteFile(ctx, connect.NewRequest(&pb.DeleteFileRequest{Workspace: dir, Path: "lib"})); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "lib")); !os.IsNotExist(err) {
		t.Error("not deleted")
	}
}
