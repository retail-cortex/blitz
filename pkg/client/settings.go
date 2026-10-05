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
	"errors"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/socket"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// Settings is the service's ConfigService for a front end that has no
// workspace open: the models the providers offer, and checking a
// permission rule.
type Settings struct {
	c pb.ConfigServiceClient
}

// AttachSettings reaches the settings of the service listening on sock.
func AttachSettings(sock string) *Settings {
	return AttachSettingsHTTP(socket.Client(sock), socket.BaseURL)
}

// AttachSettingsHTTP is AttachSettings over any HTTP client.
func AttachSettingsHTTP(hc connect.HTTPClient, baseURL string) *Settings {
	return &Settings{c: pb.NewConfigServiceClient(hc, baseURL)}
}

// ProviderModels are one provider's models, or why they couldn't be
// listed (Err), and what the list is when it isn't the provider's own.
type ProviderModels struct {
	Provider, Note, Err string
	Models              []ListedModel
}

// ListedModel is a model with the price Blitz knows for it, in dollars
// per million tokens (HasPrice false when it knows none).
type ListedModel struct {
	ID                          string
	HasPrice                    bool
	InputPerMTok, OutputPerMTok float64
}

// ListModels lists the models of the providers named, or of every one
// configured for the workspace in dir ("" for the user's settings)
// (ConfigService.ListModels).
func (s *Settings) ListModels(ctx context.Context, dir string, providers ...string) ([]ProviderModels, error) {
	res, err := s.c.ListModels(ctx, connect.NewRequest(&pb.ListModelsRequest{Workspace: dir, Providers: providers}))
	if err != nil {
		return nil, fromAPI(err)
	}
	out := make([]ProviderModels, len(res.Msg.Providers))
	for i, p := range res.Msg.Providers {
		out[i] = ProviderModels{Provider: p.Provider, Note: p.Note, Err: p.Error}
		for _, m := range p.Models {
			out[i].Models = append(out[i].Models, ListedModel{ID: m.Id, HasPrice: m.HasPrice, InputPerMTok: m.InputPerMtok, OutputPerMTok: m.OutputPerMtok})
		}
	}
	return out, nil
}

// RuleCheck is a permission rule checked without saving it: Error says
// why it's invalid; otherwise its canonical form, kind, pattern and how
// the pattern matches (prefix, glob, regex, path or name), and, for a
// sample, whether it applies (Tested, Matches) and a file the sample
// writes through a redirection (an allow rule still asks).
type RuleCheck struct {
	Error, Rule, Kind, Pattern, Form, Redirect string
	Tested, Matches                            bool
}

// CheckPermission checks rule as effect (allow, ask or deny), and tries it
// on sample unless that's "" (ConfigService.CheckPermission).
func (s *Settings) CheckPermission(ctx context.Context, effect, rule, sample string) (RuleCheck, error) {
	res, err := s.c.CheckPermission(ctx, connect.NewRequest(&pb.CheckPermissionRequest{Effect: effect, Rule: rule, Sample: sample}))
	if err != nil {
		return RuleCheck{}, fromAPI(err)
	}
	m := res.Msg
	return RuleCheck{Error: m.Error, Rule: m.Rule, Kind: m.Kind, Pattern: m.Pattern, Form: m.Form, Redirect: m.Redirect, Tested: m.Tested, Matches: m.Matches}, nil
}

// A sign-in's errors that the caller can't fix by trying again: the
// provider isn't one Blitz signs in to, the profile isn't a name, or the
// vendor's tool isn't installed (the message says where to get it).
var (
	ErrUnknownProvider = errors.New("unknown provider")
	ErrInvalidProfile  = errors.New("invalid profile")
	ErrNoTool          = errors.New("the vendor's tool isn't installed")
)

func init() {
	sentinels["UNKNOWN_PROVIDER"] = ErrUnknownProvider
	sentinels["INVALID_PROFILE"] = ErrInvalidProfile
	sentinels["NO_TOOL"] = ErrNoTool
}

// SignInStatus is a provider's sign-in: its vendor's tool (whether it's
// installed, and where to get it), and whether its credentials work now,
// with what they are.
type SignInStatus struct {
	Provider, Tool, Install string
	ToolFound, SignedIn     bool
	Detail                  string
}

func signInStatus(m *pb.SignInStatus) SignInStatus {
	if m == nil {
		return SignInStatus{}
	}
	return SignInStatus{Provider: m.Provider, Tool: m.Tool, Install: m.Install, ToolFound: m.ToolFound, SignedIn: m.SignedIn, Detail: m.Detail}
}

// SignInEvent is a line the vendor's tool printed during a sign-in, with
// the sign-in page's URL and a device code when it has them.
type SignInEvent struct {
	Line, URL, Code string
}

// SignIns are the providers' sign-ins (one provider's, or all when
// provider is ""), for the workspace in dir ("" for the user's settings)
// (ConfigService.GetSignIn).
func (s *Settings) SignIns(ctx context.Context, dir, provider, profile string) ([]SignInStatus, error) {
	res, err := s.c.GetSignIn(ctx, connect.NewRequest(&pb.GetSignInRequest{Workspace: dir, Provider: provider, Profile: profile}))
	if err != nil {
		return nil, fromAPI(err)
	}
	out := make([]SignInStatus, len(res.Msg.Statuses))
	for i, m := range res.Msg.Statuses {
		out[i] = signInStatus(m)
	}
	return out, nil
}

// SignIn runs provider's sign-in in the service, calling on with each line
// its tool prints, and returns the provider's sign-in after it
// (ConfigService.SignIn). Cancelling ctx stops it.
func (s *Settings) SignIn(ctx context.Context, dir, provider, profile string, on func(SignInEvent)) (SignInStatus, error) {
	stream, err := s.c.SignIn(ctx, connect.NewRequest(&pb.SignInRequest{Workspace: dir, Provider: provider, Profile: profile}))
	if err != nil {
		return SignInStatus{}, fromAPI(err)
	}
	defer stream.Close()
	for stream.Receive() {
		m := stream.Msg()
		if m.Done {
			return signInStatus(m.Status), nil
		}
		on(SignInEvent{Line: m.Line, URL: m.Url, Code: m.Code})
	}
	if err := stream.Err(); err != nil {
		return SignInStatus{}, fromAPI(err)
	}
	return SignInStatus{}, errors.New("the service ended the sign-in without a result")
}

// SignOut signs out of provider in the service and returns its sign-in
// after (ConfigService.SignOut).
func (s *Settings) SignOut(ctx context.Context, dir, provider, profile string) (SignInStatus, error) {
	res, err := s.c.SignOut(ctx, connect.NewRequest(&pb.SignOutRequest{Workspace: dir, Provider: provider, Profile: profile}))
	if err != nil {
		return SignInStatus{}, fromAPI(err)
	}
	return signInStatus(res.Msg.Status), nil
}
