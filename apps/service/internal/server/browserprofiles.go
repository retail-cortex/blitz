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
	"net/url"
	"os/exec"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// browserProfiles is the store of the person's browser profiles
// (spec_browser_profiles_049).
func (h configService) browserProfiles() *config.BrowserProfiles {
	return config.OpenBrowserProfiles(config.ConfigDir(h.s.configDir))
}

func profileMsg(p config.BrowserProfile) *pb.BrowserProfile {
	return &pb.BrowserProfile{Name: p.Name, Created: timestamppb.New(p.Created), AllowDomains: p.AllowDomains}
}

// profileError is a profile call's error as the API says it.
func profileError(err error) error {
	if errors.Is(err, config.ErrNoBrowserProfile) {
		return apiError(connect.CodeNotFound, "NO_BROWSER_PROFILE", err)
	}
	return invalid(err)
}

func (h configService) ListBrowserProfiles(_ context.Context, r req[pb.ListBrowserProfilesRequest]) (*connect.Response[pb.ListBrowserProfilesResponse], error) {
	s := h.browserProfiles()
	out := &pb.ListBrowserProfilesResponse{}
	for _, p := range s.List() {
		out.Profiles = append(out.Profiles, profileMsg(p))
	}
	if r.Msg.Workspace != "" {
		out.WorkspaceProfile = s.ForWorkspace(r.Msg.Workspace)
	}
	return ok(out)
}

func (h configService) CreateBrowserProfile(_ context.Context, r req[pb.CreateBrowserProfileRequest]) (*connect.Response[pb.CreateBrowserProfileResponse], error) {
	p, err := h.browserProfiles().Create(strings.TrimSpace(r.Msg.Name), time.Now())
	if err != nil {
		return nil, profileError(err)
	}
	return ok(&pb.CreateBrowserProfileResponse{Profile: profileMsg(p)})
}

func (h configService) DeleteBrowserProfile(_ context.Context, r req[pb.DeleteBrowserProfileRequest]) (*connect.Response[pb.DeleteBrowserProfileResponse], error) {
	if err := h.browserProfiles().Delete(r.Msg.Name); err != nil {
		return nil, profileError(err)
	}
	return ok(&pb.DeleteBrowserProfileResponse{})
}

func (h configService) ClearBrowserProfile(_ context.Context, r req[pb.ClearBrowserProfileRequest]) (*connect.Response[pb.ClearBrowserProfileResponse], error) {
	if err := h.browserProfiles().Clear(r.Msg.Name); err != nil {
		return nil, profileError(err)
	}
	return ok(&pb.ClearBrowserProfileResponse{})
}

func (h configService) SetBrowserProfileDomains(_ context.Context, r req[pb.SetBrowserProfileDomainsRequest]) (*connect.Response[pb.SetBrowserProfileDomainsResponse], error) {
	s := h.browserProfiles()
	var domains []string
	for _, d := range r.Msg.AllowDomains {
		if d = strings.ToLower(strings.TrimSpace(d)); d != "" {
			domains = append(domains, d)
		}
	}
	if err := s.SetAllowDomains(r.Msg.Name, domains); err != nil {
		return nil, profileError(err)
	}
	p, _ := s.Get(r.Msg.Name)
	return ok(&pb.SetBrowserProfileDomainsResponse{Profile: profileMsg(p)})
}

func (h configService) SetWorkspaceBrowserProfile(_ context.Context, r req[pb.SetWorkspaceBrowserProfileRequest]) (*connect.Response[pb.SetWorkspaceBrowserProfileResponse], error) {
	if err := h.browserProfiles().SetWorkspace(r.Msg.Workspace, r.Msg.Name); err != nil {
		return nil, profileError(err)
	}
	return ok(&pb.SetWorkspaceBrowserProfileResponse{})
}

// openBrowser starts a browser by hand, detached (replaced in tests).
var openBrowser = func(path string, args ...string) error {
	cmd := exec.Command(path, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func (h configService) OpenBrowserProfile(_ context.Context, r req[pb.OpenBrowserProfileRequest]) (*connect.Response[pb.OpenBrowserProfileResponse], error) {
	s := h.browserProfiles()
	if _, ok := s.Get(r.Msg.Name); !ok {
		return nil, profileError(config.ErrNoBrowserProfile)
	}
	page := "about:blank"
	if u, err := url.Parse(strings.TrimSpace(r.Msg.Url)); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		page = u.String()
	}
	cfg, err := config.Load(h.s.configDir)
	if err != nil {
		return nil, invalid(err)
	}
	path, err := engine.FindBrowser(cfg.Browser.Path)
	if err != nil {
		return nil, apiError(connect.CodeFailedPrecondition, "NO_BROWSER", err)
	}
	if err := openBrowser(path, "--user-data-dir="+s.Dir(r.Msg.Name), "--no-first-run", "--no-default-browser-check", page); err != nil {
		return nil, err
	}
	return ok(&pb.OpenBrowserProfileResponse{Browser: path})
}
