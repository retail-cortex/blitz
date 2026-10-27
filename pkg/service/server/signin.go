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
	"sync"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/signin"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// signInConfig is the configuration the sign-ins read profiles from: the
// workspace's, else the user's.
func (h configService) signInConfig(workspace string) (*config.Config, error) {
	dir, err := scope(workspace)
	if err != nil {
		return nil, err
	}
	if dir == "" {
		return config.Load(h.s.configDir)
	}
	return config.LoadWorkspace(h.s.configDir, dir)
}

func statusMsg(s signin.Status) *pb.SignInStatus {
	return &pb.SignInStatus{Provider: s.Provider, Tool: s.Tool, ToolFound: s.ToolFound, Install: s.Install, SignedIn: s.SignedIn, Detail: s.Detail}
}

func (h configService) GetSignIn(ctx context.Context, r req[pb.GetSignInRequest]) (*connect.Response[pb.GetSignInResponse], error) {
	cfg, err := h.signInConfig(r.Msg.Workspace)
	if err != nil {
		return nil, invalid(err)
	}
	names := signin.Providers
	if r.Msg.Provider != "" {
		names = []string{r.Msg.Provider}
	}
	// At once: each check may wait on its cloud, briefly.
	statuses := make([]*pb.SignInStatus, len(names))
	errs := make([]error, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := signin.StatusOf(ctx, cfg, name, r.Msg.Profile)
			statuses[i], errs[i] = statusMsg(s), err
		}()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, signInError(err)
	}
	return ok(&pb.GetSignInResponse{Statuses: statuses})
}

func (h configService) SignIn(ctx context.Context, r req[pb.SignInRequest], stream *connect.ServerStream[pb.SignInResponse]) error {
	cfg, err := h.signInConfig(r.Msg.Workspace)
	if err != nil {
		return invalid(err)
	}
	err = h.s.signins.SignIn(ctx, r.Msg.Provider, r.Msg.Profile, func(e signin.Event) {
		_ = stream.Send(&pb.SignInResponse{Line: e.Line, Url: e.URL, Code: e.Code})
	})
	if err != nil {
		return signInError(err)
	}
	h.s.credentialsChanged(ctx)
	s, err := signin.StatusOf(ctx, cfg, r.Msg.Provider, r.Msg.Profile)
	if err != nil {
		return signInError(err)
	}
	return stream.Send(&pb.SignInResponse{Done: true, Status: statusMsg(s)})
}

func (h configService) SignOut(ctx context.Context, r req[pb.SignOutRequest]) (*connect.Response[pb.SignOutResponse], error) {
	cfg, err := h.signInConfig(r.Msg.Workspace)
	if err != nil {
		return nil, invalid(err)
	}
	if err := h.s.signins.SignOut(ctx, r.Msg.Provider, r.Msg.Profile); err != nil {
		return nil, signInError(err)
	}
	h.s.credentialsChanged(ctx)
	s, err := signin.StatusOf(ctx, cfg, r.Msg.Provider, r.Msg.Profile)
	if err != nil {
		return nil, signInError(err)
	}
	return ok(&pb.SignOutResponse{Status: statusMsg(s)})
}

// credentialsChanged forgets the model listings and builds again the
// models of open workspaces that couldn't build them, after a sign-in
// or sign-out.
func (s *Server) credentialsChanged(ctx context.Context) {
	s.models.forget()
	s.mu.Lock()
	open := make([]*workspace, 0, len(s.workspaces))
	for _, w := range s.workspaces {
		open = append(open, w)
	}
	s.mu.Unlock()
	for _, w := range open {
		if w.ModelErr() != nil {
			_ = w.RetryModel(ctx)
		}
	}
}

// signInError gives a sign-in's error its code.
func signInError(err error) error {
	switch {
	case errors.Is(err, signin.ErrUnknownProvider):
		return apiError(connect.CodeInvalidArgument, "UNKNOWN_PROVIDER", err)
	case errors.Is(err, signin.ErrBadProfile):
		return apiError(connect.CodeInvalidArgument, "INVALID_PROFILE", err)
	case errors.Is(err, signin.ErrNoTool):
		return apiError(connect.CodeFailedPrecondition, "NO_TOOL", err)
	case errors.Is(err, signin.ErrRunning):
		return apiError(connect.CodeAlreadyExists, "SIGN_IN_RUNNING", err)
	case errors.Is(err, context.Canceled):
		return connect.NewError(connect.CodeCanceled, err)
	default:
		return apiError(connect.CodeFailedPrecondition, "SIGN_IN_FAILED", err)
	}
}
