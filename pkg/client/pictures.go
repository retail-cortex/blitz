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

package client

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// Pictures (spec_058_pictures) for `blitz picture` (PIC-80): the same
// WorkspaceService calls the desktop's panel makes, so the service checks
// the CLI's requests against the same catalog (PIC-62).

// PictureModels are the image models the workspace offers, the [images]
// model marked default, and the folder pictures are saved in.
func (r *Remote) PictureModels(ctx context.Context) (*pb.ListPictureModelsResponse, error) {
	res, err := r.workspaces.ListPictureModels(ctx, connect.NewRequest(&pb.ListPictureModelsRequest{Workspace: r.dir}))
	if err != nil {
		return nil, fromAPI(err)
	}
	return res.Msg, nil
}

// MakePicture makes req's turn in the workspace (its Workspace is set).
func (r *Remote) MakePicture(ctx context.Context, req *pb.MakePictureRequest) (*pb.MakePictureResponse, error) {
	req.Workspace = r.dir
	res, err := r.workspaces.MakePicture(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, fromAPI(err)
	}
	return res.Msg, nil
}

// SavePicture writes session's picture into the workspace at path (""
// for its name in the pictures folder), returning where it went.
func (r *Remote) SavePicture(ctx context.Context, session, picture, path string) (string, error) {
	res, err := r.workspaces.SavePicture(ctx, connect.NewRequest(&pb.SavePictureRequest{Workspace: r.dir, Session: session, Picture: picture, Path: path}))
	if err != nil {
		return "", fromAPI(err)
	}
	return res.Msg.Path, nil
}

// PictureSessions are the workspace's Pictures sessions, newest first.
func (r *Remote) PictureSessions(ctx context.Context) ([]*pb.PictureSession, error) {
	res, err := r.workspaces.ListPictureSessions(ctx, connect.NewRequest(&pb.ListPictureSessionsRequest{Workspace: r.dir}))
	if err != nil {
		return nil, fromAPI(err)
	}
	return res.Msg.Sessions, nil
}

// PictureSession is session id.
func (r *Remote) PictureSession(ctx context.Context, id string) (*pb.PictureSession, error) {
	res, err := r.workspaces.GetPictureSession(ctx, connect.NewRequest(&pb.GetPictureSessionRequest{Workspace: r.dir, Id: id}))
	if err != nil {
		return nil, fromAPI(err)
	}
	return res.Msg.Session, nil
}

// DeletePictureSession deletes session id and its pictures (not what was
// saved into the workspace).
func (r *Remote) DeletePictureSession(ctx context.Context, id string) error {
	_, err := r.workspaces.DeletePictureSession(ctx, connect.NewRequest(&pb.DeletePictureSessionRequest{Workspace: r.dir, Id: id}))
	return fromAPI(err)
}
