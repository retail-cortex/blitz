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
	"time"

	"connectrpc.com/connect"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// Organization is the organization's service this computer connects to,
// and who is signed in to it (spec_053_enterprise EN-10 to EN-14).
type Organization struct {
	// URL is the service's address; "" when not connected.
	URL          string
	Name         string
	Version      string
	Capabilities []string
	SignedIn     bool
	Email        string
	PersonName   string
	Roles        []string
	// Renews is when the access token is next renewed; zero until one is
	// asked for.
	Renews time.Time
	// Terms is a version of the organization's terms to accept, if any.
	Terms string
	// Checked says the service was asked; Reachable whether it answered,
	// and Error why not.
	Checked   bool
	Reachable bool
	Error     string
	// Managed says the machine-wide settings set it (EN-10): it can't be
	// changed or disconnected here.
	Managed bool
}

func organization(m *pb.EnterpriseState) Organization {
	if m == nil {
		return Organization{}
	}
	o := Organization{URL: m.Url, Name: m.Name, Version: m.Version, Capabilities: m.Capabilities, SignedIn: m.SignedIn,
		Email: m.Email, PersonName: m.PersonName, Roles: m.Roles, Terms: m.TermsVersion,
		Checked: m.Checked, Reachable: m.Reachable, Error: m.Error, Managed: m.Managed}
	if m.RenewsUnix != 0 {
		o.Renews = time.Unix(m.RenewsUnix, 0)
	}
	return o
}

// Organization is the organization's service and the sign-in to it;
// with check, after asking the service (ConfigService.GetEnterprise).
func (s *Settings) Organization(ctx context.Context, check bool) (Organization, error) {
	res, err := s.c.GetEnterprise(ctx, connect.NewRequest(&pb.GetEnterpriseRequest{Check: check}))
	if err != nil {
		return Organization{}, fromAPI(err)
	}
	return organization(res.Msg.State), nil
}

// Connect connects to the organization's service at url, after checking
// it's one, or disconnects with "" (ConfigService.ConnectEnterprise).
func (s *Settings) Connect(ctx context.Context, url string) (Organization, error) {
	res, err := s.c.ConnectEnterprise(ctx, connect.NewRequest(&pb.ConnectEnterpriseRequest{Url: url}))
	if err != nil {
		return Organization{}, fromAPI(err)
	}
	return organization(res.Msg.State), nil
}

// OrganizationPrompt is what to do to sign in: open URL, and enter Code
// there when there is one.
type OrganizationPrompt struct {
	URL, Code string
}

// SignInOrganization signs in to the organization's service: in a
// browser on the service's computer, or with a code (device), calling on
// with what to open (ConfigService.EnterpriseSignIn). Cancelling ctx
// stops it.
func (s *Settings) SignInOrganization(ctx context.Context, device bool, on func(OrganizationPrompt)) (Organization, error) {
	stream, err := s.c.EnterpriseSignIn(ctx, connect.NewRequest(&pb.EnterpriseSignInRequest{Device: device}))
	if err != nil {
		return Organization{}, fromAPI(err)
	}
	defer stream.Close()
	for stream.Receive() {
		m := stream.Msg()
		if m.Done {
			return organization(m.State), nil
		}
		on(OrganizationPrompt{URL: m.Url, Code: m.Code})
	}
	if err := stream.Err(); err != nil {
		return Organization{}, fromAPI(err)
	}
	return Organization{}, errors.New("the service ended the sign-in without a result")
}

// SignOutOrganization signs out of the organization's service
// (ConfigService.EnterpriseSignOut).
func (s *Settings) SignOutOrganization(ctx context.Context) (Organization, error) {
	res, err := s.c.EnterpriseSignOut(ctx, connect.NewRequest(&pb.EnterpriseSignOutRequest{}))
	if err != nil {
		return Organization{}, fromAPI(err)
	}
	return organization(res.Msg.State), nil
}

// OrganizationTerms are the organization's terms in force: their version
// ("" for none), whether they're accepted, and the text or its link.
type OrganizationTerms struct {
	Version   string
	Accepted  bool
	Text, URL string
}

// Terms are the organization's terms in force (ConfigService.GetTerms).
func (s *Settings) Terms(ctx context.Context) (OrganizationTerms, error) {
	res, err := s.c.GetTerms(ctx, connect.NewRequest(&pb.GetTermsRequest{}))
	if err != nil {
		return OrganizationTerms{}, fromAPI(err)
	}
	return OrganizationTerms{Version: res.Msg.Version, Accepted: res.Msg.Accepted, Text: res.Msg.Text, URL: res.Msg.Url}, nil
}

// AcceptTerms records that the person accepted version of the terms
// (ConfigService.AcceptTerms).
func (s *Settings) AcceptTerms(ctx context.Context, version string) (Organization, error) {
	res, err := s.c.AcceptTerms(ctx, connect.NewRequest(&pb.AcceptTermsRequest{Version: version}))
	if err != nil {
		return Organization{}, fromAPI(err)
	}
	return organization(res.Msg.State), nil
}

// OrgWorkspace is one of the organization's workspaces (EN-60).
type OrgWorkspace struct {
	Handle, Name, Role string
	Sync               bool
	// Dir is where it is, or would be, cloned; Cloned whether it is.
	Dir    string
	Cloned bool
}

func orgWorkspace(m *pb.OrgWorkspace) OrgWorkspace {
	return OrgWorkspace{Handle: m.GetHandle(), Name: m.GetName(), Role: m.GetRole(), Sync: m.GetSync(), Dir: m.GetDir(), Cloned: m.GetCloned()}
}

// OrgWorkspaces are the organization's workspaces the person has a role
// in, and the organization's name (ConfigService.ListOrgWorkspaces).
func (s *Settings) OrgWorkspaces(ctx context.Context) ([]OrgWorkspace, string, error) {
	res, err := s.c.ListOrgWorkspaces(ctx, connect.NewRequest(&pb.ListOrgWorkspacesRequest{}))
	if err != nil {
		return nil, "", fromAPI(err)
	}
	out := make([]OrgWorkspace, len(res.Msg.Workspaces))
	for i, w := range res.Msg.Workspaces {
		out[i] = orgWorkspace(w)
	}
	return out, res.Msg.Organization, nil
}

// CreateOrgWorkspace makes a workspace of the organization's
// (ConfigService.CreateOrgWorkspace).
func (s *Settings) CreateOrgWorkspace(ctx context.Context, name, handle string, personal bool) (OrgWorkspace, error) {
	res, err := s.c.CreateOrgWorkspace(ctx, connect.NewRequest(&pb.CreateOrgWorkspaceRequest{Name: name, Handle: handle, Personal: personal}))
	if err != nil {
		return OrgWorkspace{}, fromAPI(err)
	}
	return orgWorkspace(res.Msg.Workspace), nil
}

// ShareWithOrganization shares the folder dir with the organization where
// it is (ConfigService.ShareWithOrganization, EN-66): the workspace, and
// what the person should know.
func (s *Settings) ShareWithOrganization(ctx context.Context, dir, name, handle string, personal bool) (OrgWorkspace, []string, error) {
	res, err := s.c.ShareWithOrganization(ctx, connect.NewRequest(&pb.ShareWithOrganizationRequest{Workspace: dir, Name: name, Handle: handle, Personal: personal}))
	if err != nil {
		return OrgWorkspace{}, nil, fromAPI(err)
	}
	return orgWorkspace(res.Msg.Workspace), res.Msg.Notes, nil
}

// OpenOrgWorkspace clones an organization workspace the first time, and
// gives its folder (ConfigService.OpenOrgWorkspace).
func (s *Settings) OpenOrgWorkspace(ctx context.Context, handle string) (dir string, cloned bool, err error) {
	res, err := s.c.OpenOrgWorkspace(ctx, connect.NewRequest(&pb.OpenOrgWorkspaceRequest{Handle: handle}))
	if err != nil {
		return "", false, fromAPI(err)
	}
	return res.Msg.Dir, res.Msg.Cloned, nil
}

// GitCredential is Git's credential for host (ConfigService.GitCredential):
// ok false when the service has none for it.
func (s *Settings) GitCredential(ctx context.Context, host string) (user, password string, ok bool, err error) {
	res, err := s.c.GitCredential(ctx, connect.NewRequest(&pb.GitCredentialRequest{Host: host}))
	if err != nil {
		return "", "", false, fromAPI(err)
	}
	return res.Msg.Username, res.Msg.Password, res.Msg.Ok, nil
}
