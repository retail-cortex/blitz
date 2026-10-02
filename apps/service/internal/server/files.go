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
	"io/fs"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/engine"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// fileService implements FileService over the engine's files (spec_files_029).
type fileService struct{ s *Server }

// fileError maps the file operations' errors to Connect codes and reasons.
func fileError(err error) error {
	var changed *engine.FileChangedError
	switch {
	case errors.As(err, &changed):
		return apiError(connect.CodeFailedPrecondition, "FILE_CHANGED", err, "path", changed.Path, "current_version", changed.Current)
	case errors.Is(err, engine.ErrBadPath):
		return apiError(connect.CodeInvalidArgument, "BAD_PATH", err)
	case errors.Is(err, fs.ErrNotExist):
		return apiError(connect.CodeNotFound, "FILE_NOT_FOUND", err)
	case errors.Is(err, fs.ErrExist):
		return apiError(connect.CodeAlreadyExists, "FILE_EXISTS", err)
	case errors.Is(err, engine.ErrNoPreview):
		return apiError(connect.CodeInvalidArgument, "NO_PREVIEW", err)
	case errors.Is(err, engine.ErrNotARepository):
		return apiError(connect.CodeFailedPrecondition, "NOT_A_REPOSITORY", err)
	}
	return toAPI(err)
}

var fileKinds = map[engine.FileKind]pb.FileKind{
	engine.KindFile:    pb.FileKind_FILE_KIND_FILE,
	engine.KindFolder:  pb.FileKind_FILE_KIND_FOLDER,
	engine.KindSymlink: pb.FileKind_FILE_KIND_SYMLINK,
}

func (h fileService) ListDir(ctx context.Context, r req[pb.ListDirRequest]) (*connect.Response[pb.ListDirResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	l, err := w.ListDir(ctx, r.Msg.Path, r.Msg.ShowHidden)
	if err != nil {
		return nil, fileError(err)
	}
	res := &pb.ListDirResponse{Truncated: l.Truncated, Repo: l.Repo}
	for _, e := range l.Entries {
		res.Entries = append(res.Entries, &pb.FileEntry{
			Name: e.Name, Path: e.Path, Kind: fileKinds[e.Kind], Size: e.Size, Modified: timestamp(e.Modified),
			Git: e.Git, Hidden: e.Hidden, AgentRule: e.AgentRule,
		})
	}
	return ok(res)
}

func (h fileService) ReadFile(ctx context.Context, r req[pb.ReadFileRequest]) (*connect.Response[pb.ReadFileResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	f, err := w.ReadFile(r.Msg.Path)
	if err != nil {
		return nil, fileError(err)
	}
	return ok(&pb.ReadFileResponse{Path: f.Path, Text: f.Text, Version: f.Version, Size: f.Size, Binary: f.Binary, TooLarge: f.TooLarge, AgentRule: f.AgentRule})
}

func (h fileService) ReadPreview(ctx context.Context, r req[pb.ReadPreviewRequest]) (*connect.Response[pb.ReadPreviewResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	mime, data, err := w.ReadPreview(r.Msg.Path)
	if err != nil {
		return nil, fileError(err)
	}
	return ok(&pb.ReadPreviewResponse{Path: r.Msg.Path, Mime: mime, Data: data})
}

func (h fileService) WriteFile(ctx context.Context, r req[pb.WriteFileRequest]) (*connect.Response[pb.WriteFileResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	v, err := w.WriteFile(ctx, r.Msg.Path, r.Msg.Text, r.Msg.Version)
	if err != nil {
		return nil, fileError(err)
	}
	return ok(&pb.WriteFileResponse{Version: v})
}

func (h fileService) CreateFolder(ctx context.Context, r req[pb.CreateFolderRequest]) (*connect.Response[pb.CreateFolderResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.CreateFolder(r.Msg.Path); err != nil {
		return nil, fileError(err)
	}
	return ok(&pb.CreateFolderResponse{})
}

func (h fileService) RenameFile(ctx context.Context, r req[pb.RenameFileRequest]) (*connect.Response[pb.RenameFileResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.RenameFile(ctx, r.Msg.From, r.Msg.To); err != nil {
		return nil, fileError(err)
	}
	return ok(&pb.RenameFileResponse{})
}

func (h fileService) DeleteFile(ctx context.Context, r req[pb.DeleteFileRequest]) (*connect.Response[pb.DeleteFileResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.DeleteFile(ctx, r.Msg.Path); err != nil {
		return nil, fileError(err)
	}
	return ok(&pb.DeleteFileResponse{})
}

var gitActions = map[pb.GitAction]engine.GitAction{
	pb.GitAction_GIT_ACTION_STAGE:   engine.GitStage,
	pb.GitAction_GIT_ACTION_UNSTAGE: engine.GitUnstage,
	pb.GitAction_GIT_ACTION_DISCARD: engine.GitDiscard,
	pb.GitAction_GIT_ACTION_IGNORE:  engine.GitIgnore,
}

func (h fileService) GitFileAction(ctx context.Context, r req[pb.GitFileActionRequest]) (*connect.Response[pb.GitFileActionResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	action, known := gitActions[r.Msg.Action]
	if !known {
		return nil, invalid(errors.New("action is required"))
	}
	if err := w.GitFileAction(ctx, r.Msg.Path, action); err != nil {
		return nil, fileError(err)
	}
	return ok(&pb.GitFileActionResponse{})
}

func (h fileService) FindFiles(ctx context.Context, r req[pb.FindFilesRequest]) (*connect.Response[pb.FindFilesResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	paths, err := w.FindPaths(ctx, r.Msg.Query, int(r.Msg.Limit), r.Msg.Folders)
	if err != nil {
		return nil, fileError(err)
	}
	return ok(&pb.FindFilesResponse{Paths: paths})
}

func (h fileService) StatFiles(ctx context.Context, r req[pb.StatFilesRequest]) (*connect.Response[pb.StatFilesResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	return ok(&pb.StatFilesResponse{Versions: w.StatFiles(r.Msg.Paths)})
}
