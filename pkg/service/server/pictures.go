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

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/engine"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Pictures (spec_058_pictures): the image models offered, turns, sessions
// and saving into the workspace.

// pictureError is a Pictures call's error on the wire.
func pictureError(err error) error {
	switch {
	case errors.Is(err, engine.ErrNoPictureSession), errors.Is(err, engine.ErrNoPicture):
		return apiError(connect.CodeNotFound, "PICTURE_NOT_FOUND", err)
	case errors.Is(err, engine.ErrPictureRequest):
		return apiError(connect.CodeInvalidArgument, "PICTURE_NOT_TAKEN", err)
	}
	return fileError(err)
}

// ListPictureModels is the image models Pictures offers.
func (h workspaceService) ListPictureModels(ctx context.Context, r req[pb.ListPictureModelsRequest]) (*connect.Response[pb.ListPictureModelsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	out := &pb.ListPictureModelsResponse{Folder: w.PicturesFolder()}
	for _, m := range w.PictureModels(ctx) {
		i := m.Info
		sizes := map[string]int32{}
		for k, v := range i.SizeTokens {
			sizes[k] = int32(v)
		}
		out.Models = append(out.Models, &pb.PictureModel{
			Ref: m.Ref, Id: i.ID, Name: i.Name, Kind: i.Kind, Default: m.Default, Organization: m.Organization, Assumed: i.Assumed,
			MaxReferences: int32(i.MaxReferences), MaxCharacters: int32(i.MaxCharacters), MaxObjects: int32(i.MaxObjects), MaxPictures: int32(i.MaxPictures),
			Sizes: i.Sizes, Ratios: i.Ratios, Thinking: i.Thinking, ThinkingAlways: i.ThinkingAlways, Search: i.Search,
			NegativePrompt: i.NegativePrompt, People: i.People, Seed: i.Seed, Enhance: i.Enhance, Formats: i.Formats,
			InputPerMtok: m.InputPerMTok, OutputPerMtok: m.OutputPerMTok, PerPicture: i.PerPicture, SizeTokens: sizes,
		})
	}
	return ok(out)
}

// pictureSession is a session on the wire.
func pictureSession(s engine.PictureSession) *pb.PictureSession {
	out := &pb.PictureSession{Id: s.ID, Name: s.Name, Created: timestamppb.New(s.Created), Updated: timestamppb.New(s.Updated)}
	for _, t := range s.Turns {
		m := &pb.PictureTurn{Id: t.ID, At: timestamppb.New(t.At), Model: t.Model, Kind: t.Kind, Prompt: t.Prompt, Count: int32(t.Count), AspectRatio: t.AspectRatio,
			Size: t.Size, Thinking: t.Thinking, Search: t.Search, Avoid: t.Avoid, People: t.People, Format: t.Format, StartFrom: t.StartFrom,
			Text: t.Text, Thoughts: t.Thoughts, Problem: t.Problem, CostUsd: t.CostUSD}
		if t.Seed != nil {
			m.Seed, m.HasSeed = *t.Seed, true
		}
		for _, ref := range t.References {
			m.References = append(m.References, &pb.PictureTurnRef{File: ref.File, Mime: ref.MIME, Name: ref.Name, Path: ref.Path, Role: ref.Role})
		}
		for _, p := range t.Pictures {
			if !p.Deleted {
				m.Pictures = append(m.Pictures, &pb.PictureFile{Id: p.ID, Mime: p.MIME, Saved: p.Saved})
			}
		}
		for _, src := range t.Sources {
			m.Sources = append(m.Sources, &pb.PictureSource{Title: src.Title, Url: src.URL})
		}
		out.Turns = append(out.Turns, m)
	}
	return out
}

// MakePicture makes (or estimates) a turn.
func (h workspaceService) MakePicture(ctx context.Context, r req[pb.MakePictureRequest]) (*connect.Response[pb.MakePictureResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	m := r.Msg
	pr := engine.PictureRequest{Session: m.Session, Model: m.Model, Prompt: m.Prompt, Count: int(m.Count), AspectRatio: m.AspectRatio, Size: m.Size, Thinking: m.Thinking,
		Search: m.Search, SearchImages: m.SearchImages, Avoid: m.Avoid, People: m.People, Seed: m.Seed, UseSeed: m.UseSeed, Enhance: m.Enhance,
		Format: m.Format, Quality: int(m.Quality), StartFrom: m.StartFrom}
	for _, ref := range m.References {
		pr.References = append(pr.References, engine.PictureRef{Path: ref.Path, Picture: ref.Picture, Data: ref.Data, MIME: ref.Mime, Name: ref.Name, Role: ref.Role})
	}
	s, cost, err := w.MakePicture(ctx, pr, m.Estimate)
	if err != nil {
		return nil, pictureError(err)
	}
	out := &pb.MakePictureResponse{CostUsd: cost.CostUSD, Priced: cost.Priced}
	if !m.Estimate {
		out.Session = pictureSession(s)
	}
	return ok(out)
}

// ListPictureSessions is the workspace's sessions, newest first.
func (h workspaceService) ListPictureSessions(ctx context.Context, r req[pb.ListPictureSessionsRequest]) (*connect.Response[pb.ListPictureSessionsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	list, err := w.PictureSessions()
	if err != nil {
		return nil, pictureError(err)
	}
	out := &pb.ListPictureSessionsResponse{}
	for _, s := range list {
		out.Sessions = append(out.Sessions, pictureSession(s))
	}
	return ok(out)
}

// GetPictureSession is one session.
func (h workspaceService) GetPictureSession(ctx context.Context, r req[pb.GetPictureSessionRequest]) (*connect.Response[pb.GetPictureSessionResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	s, err := w.PictureSession(r.Msg.Id)
	if err != nil {
		return nil, pictureError(err)
	}
	return ok(&pb.GetPictureSessionResponse{Session: pictureSession(s)})
}

// RenamePictureSession names a session.
func (h workspaceService) RenamePictureSession(ctx context.Context, r req[pb.RenamePictureSessionRequest]) (*connect.Response[pb.RenamePictureSessionResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	s, err := w.RenamePictureSession(r.Msg.Id, r.Msg.Name)
	if err != nil {
		return nil, pictureError(err)
	}
	return ok(&pb.RenamePictureSessionResponse{Session: pictureSession(s)})
}

// DeletePictureSession deletes a session and its pictures.
func (h workspaceService) DeletePictureSession(ctx context.Context, r req[pb.DeletePictureSessionRequest]) (*connect.Response[pb.DeletePictureSessionResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.DeletePictureSession(r.Msg.Id); err != nil {
		return nil, pictureError(err)
	}
	return ok(&pb.DeletePictureSessionResponse{})
}

// ReadPicture is a session's picture.
func (h workspaceService) ReadPicture(ctx context.Context, r req[pb.ReadPictureRequest]) (*connect.Response[pb.ReadPictureResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	data, mime, err := w.ReadPicture(r.Msg.Session, r.Msg.Picture)
	if err != nil {
		return nil, pictureError(err)
	}
	return ok(&pb.ReadPictureResponse{Data: data, Mime: mime})
}

// SavePicture writes a picture into the workspace.
func (h workspaceService) SavePicture(ctx context.Context, r req[pb.SavePictureRequest]) (*connect.Response[pb.SavePictureResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	p, err := w.SavePicture(ctx, r.Msg.Session, r.Msg.Picture, r.Msg.Path)
	if err != nil {
		return nil, pictureError(err)
	}
	return ok(&pb.SavePictureResponse{Path: p})
}

// DeletePicture removes a picture from its session.
func (h workspaceService) DeletePicture(ctx context.Context, r req[pb.DeletePictureRequest]) (*connect.Response[pb.DeletePictureResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	s, err := w.DeletePicture(r.Msg.Session, r.Msg.Picture)
	if err != nil {
		return nil, pictureError(err)
	}
	return ok(&pb.DeletePictureResponse{Session: pictureSession(s)})
}
