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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/enterprise"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// orgClient is the connection, and the settings, for the organization
// workspaces' calls: not connected is NOT_CONNECTED.
func (s *Server) orgClient() (*enterprise.Client, *config.Config, error) {
	cfg, err := config.Load(s.configDir)
	if err != nil {
		return nil, nil, invalid(err)
	}
	c, err := s.enterpriseFor(cfg.Enterprise.URL)
	if err != nil {
		return nil, nil, invalid(err)
	}
	if c == nil {
		return nil, nil, apiError(connect.CodeFailedPrecondition, "NOT_CONNECTED", errors.New("connect to your organization's service first"))
	}
	return c, cfg, nil
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9 ._-]+`)

// orgFolder is the organization's folder of clones: <workspaces_dir>/<its
// name, else its host> (EN-60).
func (s *Server) orgFolder(c *enterprise.Client, cfg *config.Config) string {
	name := c.Status().Info.Name
	if b := enterprise.LoadBranding(s.brandingDir(c)); b != nil && b.Name != "" {
		name = b.Name
	}
	name = strings.TrimSpace(unsafeName.ReplaceAllString(name, ""))
	if name == "" || strings.Trim(name, ".") == "" {
		name = strings.ReplaceAll(c.Host(), ":", "_")
	}
	root := cfg.Enterprise.WorkspacesDir
	if root == "" {
		root = "~/Blitz"
	}
	return filepath.Join(config.ExpandHome(root), name)
}

func (s *Server) orgWorkspaceMsg(w enterprise.Workspace, folder string) *pb.OrgWorkspace {
	dir := filepath.Join(folder, w.Handle)
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return &pb.OrgWorkspace{Handle: w.Handle, Name: w.Name, Role: w.Role, Sync: w.Sync, CloneUrl: w.CloneURL, Dir: dir, Cloned: err == nil}
}

func (h configService) ListOrgWorkspaces(ctx context.Context, _ req[pb.ListOrgWorkspacesRequest]) (*connect.Response[pb.ListOrgWorkspacesResponse], error) {
	c, cfg, err := h.s.orgClient()
	if err != nil {
		return nil, err
	}
	list, err := c.Workspaces(ctx)
	if err != nil {
		return nil, enterpriseError(err)
	}
	folder := h.s.orgFolder(c, cfg)
	out := &pb.ListOrgWorkspacesResponse{Organization: filepath.Base(folder)}
	for _, w := range list {
		if !w.Archived {
			out.Workspaces = append(out.Workspaces, h.s.orgWorkspaceMsg(w, folder))
		}
	}
	return ok(out)
}

func (h configService) CreateOrgWorkspace(ctx context.Context, r req[pb.CreateOrgWorkspaceRequest]) (*connect.Response[pb.CreateOrgWorkspaceResponse], error) {
	c, cfg, err := h.s.orgClient()
	if err != nil {
		return nil, err
	}
	w, err := c.CreateWorkspace(ctx, r.Msg.Name, r.Msg.Handle)
	if err != nil {
		return nil, enterpriseError(err)
	}
	return ok(&pb.CreateOrgWorkspaceResponse{Workspace: h.s.orgWorkspaceMsg(w, h.s.orgFolder(c, cfg))})
}

// gitHelper is the credential helper Git runs for the organization's
// service (EN-61): this program's git-credential, unless set (tests).
func (s *Server) gitHelper() string {
	if s.ent.gitHelper != "" {
		return s.ent.gitHelper
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return fmt.Sprintf("!%q git-credential", exe)
}

// git runs git in dir with the organization's credential helper alone,
// never asking at a terminal.
func (s *Server) git(ctx context.Context, dir string, args ...string) error {
	all := append([]string{"-c", "credential.helper=", "-c", "credential.helper=" + s.gitHelper()}, args...)
	cmd := exec.CommandContext(ctx, "git", all...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (h configService) OpenOrgWorkspace(ctx context.Context, r req[pb.OpenOrgWorkspaceRequest]) (*connect.Response[pb.OpenOrgWorkspaceResponse], error) {
	c, cfg, err := h.s.orgClient()
	if err != nil {
		return nil, err
	}
	list, err := c.Workspaces(ctx)
	if err != nil {
		return nil, enterpriseError(err)
	}
	i := -1
	for j, w := range list {
		if w.Handle == r.Msg.Handle {
			i = j
		}
	}
	if i < 0 {
		return nil, apiError(connect.CodeNotFound, "NOT_FOUND", fmt.Errorf("you have no role in a workspace %q of your organization's", r.Msg.Handle))
	}
	w := h.s.orgWorkspaceMsg(list[i], h.s.orgFolder(c, cfg))
	if w.Cloned {
		return ok(&pb.OpenOrgWorkspaceResponse{Dir: w.Dir})
	}
	if err := os.MkdirAll(filepath.Dir(w.Dir), 0o700); err != nil {
		return nil, invalid(err)
	}
	if err := h.s.git(ctx, filepath.Dir(w.Dir), "clone", w.CloneUrl, w.Dir); err != nil {
		_ = os.RemoveAll(w.Dir)
		return nil, apiError(connect.CodeUnavailable, "CLONE_FAILED", fmt.Errorf("cloning %s: %w", w.Name, err))
	}
	// The helper in the clone's own settings, for the service only: Git
	// run there by the person, or by an agent, signs in as Blitz does.
	if err := h.s.git(ctx, w.Dir, "config", "credential."+c.URL()+".helper", h.s.gitHelper()); err != nil {
		return nil, invalid(err)
	}
	return ok(&pb.OpenOrgWorkspaceResponse{Dir: w.Dir, Cloned: true})
}

func (h configService) GitCredential(ctx context.Context, r req[pb.GitCredentialRequest]) (*connect.Response[pb.GitCredentialResponse], error) {
	c, err := h.s.enterpriseClient()
	if err != nil || c == nil || c.Host() != r.Msg.Host {
		return ok(&pb.GitCredentialResponse{})
	}
	token, err := c.AccessToken(ctx)
	if err != nil {
		if errors.Is(err, enterprise.ErrNotSignedIn) {
			return ok(&pb.GitCredentialResponse{})
		}
		return nil, enterpriseError(err)
	}
	return ok(&pb.GitCredentialResponse{Ok: true, Username: "blitz", Password: token})
}
