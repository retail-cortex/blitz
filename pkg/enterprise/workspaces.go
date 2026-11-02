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

package enterprise

import (
	"context"
	"net/http"
	"net/url"
)

// Workspace is one of the organization's workspaces the person has a role
// in (spec_053_enterprise EN-60): a Git repository the service keeps,
// cloned through it at CloneURL.
type Workspace struct {
	Handle   string
	Name     string
	Role     string // owner, editor, runner or viewer
	Sync     bool   // saved and shared as it changes (EN-62)
	CloneURL string
	Archived bool
}

// workspaceView is how the service answers for a workspace.
type workspaceView struct {
	Workspace struct {
		Handle   string  `json:"handle"`
		Name     string  `json:"name"`
		Role     string  `json:"role"`
		Sync     bool    `json:"sync"`
		Archived *string `json:"archived"`
	} `json:"workspace"`
	CloneURL string `json:"clone_url"`
}

func (v workspaceView) workspace() Workspace {
	w := v.Workspace
	return Workspace{Handle: w.Handle, Name: w.Name, Role: w.Role, Sync: w.Sync, CloneURL: v.CloneURL, Archived: w.Archived != nil}
}

// Workspaces are the organization's workspaces the person has a role in.
func (c *Client) Workspaces(ctx context.Context) ([]Workspace, error) {
	token, err := c.AccessToken(ctx)
	if err != nil {
		return nil, err
	}
	var out struct {
		Workspaces []workspaceView `json:"workspaces"`
	}
	if err := call(ctx, c.hc, http.MethodGet, c.url+"/api/v1/workspaces", token, nil, &out); err != nil {
		return nil, err
	}
	list := make([]Workspace, 0, len(out.Workspaces))
	for _, v := range out.Workspaces {
		list = append(list, v.workspace())
	}
	return list, nil
}

// CreateWorkspace makes a workspace of the organization's, the person its
// owner; handle "" lets the service make one from the name.
func (c *Client) CreateWorkspace(ctx context.Context, name, handle string) (Workspace, error) {
	token, err := c.AccessToken(ctx)
	if err != nil {
		return Workspace{}, err
	}
	var v workspaceView
	if err := call(ctx, c.hc, http.MethodPost, c.url+"/api/v1/workspaces", token, map[string]string{"name": name, "handle": handle}, &v); err != nil {
		return Workspace{}, err
	}
	return v.workspace(), nil
}

// Host is the service's host, as Git names it when it asks for a
// credential.
func (c *Client) Host() string {
	u, err := url.Parse(c.url)
	if err != nil {
		return ""
	}
	return u.Host
}
