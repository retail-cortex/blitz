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
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
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

// WatchWorkspace follows a workspace's stream of pushes (the enterprise
// service's WS-06), calling on with each pushed ref's new commit, until ctx
// ends or the stream does (the error says why).
func (c *Client) WatchWorkspace(ctx context.Context, handle string, on func(ref, commit, by string)) error {
	token, err := c.AccessToken(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url+"/api/v1/workspaces/"+url.PathEscape(handle)+"/watch", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "text/event-stream")
	res, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("reaching the organization's service: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return &Refusal{Status: res.StatusCode}
	}
	sc := bufio.NewScanner(res.Body)
	for sc.Scan() {
		data, ok := strings.CutPrefix(sc.Text(), "data: ")
		if !ok {
			continue
		}
		var p struct{ Ref, Commit, By string }
		if json.Unmarshal([]byte(data), &p) == nil {
			on(p.Ref, p.Commit, p.By)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return errors.New("the organization's service ended the workspace's stream")
}
