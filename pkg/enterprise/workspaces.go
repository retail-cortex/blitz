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
	"regexp"
	"strings"
)

// Workspace is one of the organization's workspaces the person has a role
// in (spec_053_enterprise EN-60): a Git repository the service keeps,
// cloned through it at CloneURL.
type Workspace struct {
	// ID is the service's, which never changes (the Blitz-Workspace header,
	// EN-22).
	ID       string
	Handle   string
	Name     string
	Role     string // owner, editor, runner or viewer
	Sync     bool   // saved and shared as it changes (EN-62)
	CloneURL string
	Archived bool
}

// handleRE is a workspace's handle: a team's one word, or a person's
// ~<person>/<name> (blitz-enterprise spec_007_workspaces WS-01). It
// becomes a folder under the organization's, so nothing else is taken.
var handleRE = regexp.MustCompile(`^(~[a-z0-9][a-z0-9-]{0,62}/)?[a-z0-9][a-z0-9-]{0,62}$`)

// ValidHandle is whether h can be a workspace's handle.
func ValidHandle(h string) bool { return handleRE.MatchString(h) }

// errHandle is a service's answer naming a workspace no handle could.
var errHandle = errors.New("the organization's service named a workspace with a handle that isn't one")

// workspaceView is how the service answers for a workspace.
type workspaceView struct {
	Workspace struct {
		ID       string  `json:"id"`
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
	return Workspace{ID: w.ID, Handle: w.Handle, Name: w.Name, Role: w.Role, Sync: w.Sync, CloneURL: v.CloneURL, Archived: w.Archived != nil}
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
		if ValidHandle(v.Workspace.Handle) { // never a folder outside the organization's
			list = append(list, v.workspace())
		}
	}
	return list, nil
}

// CreateWorkspace makes a workspace of the organization's, the person its
// owner; handle "" lets the service make one from the name. A personal
// one's handle is ~<the person>/<handle> (blitz-enterprise WS-01).
func (c *Client) CreateWorkspace(ctx context.Context, name, handle string, personal bool) (Workspace, error) {
	token, err := c.AccessToken(ctx)
	if err != nil {
		return Workspace{}, err
	}
	var v workspaceView
	if err := call(ctx, c.hc, http.MethodPost, c.url+"/api/v1/workspaces", token, map[string]any{"name": name, "handle": handle, "personal": personal}, &v); err != nil {
		return Workspace{}, err
	}
	if !ValidHandle(v.Workspace.Handle) {
		return Workspace{}, errHandle
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
	err := c.events(ctx, "/api/v1/workspaces/"+url.PathEscape(handle)+"/watch", func(data string) {
		var p struct{ Ref, Commit, By string }
		if json.Unmarshal([]byte(data), &p) == nil {
			on(p.Ref, p.Commit, p.By)
		}
	})
	if err == nil {
		err = errors.New("the organization's service ended the workspace's stream")
	}
	return err
}

// events follows a stream of server-sent events at path, calling on with
// each event's data, until it ends (nil) or fails.
func (c *Client) events(ctx context.Context, path string, on func(data string)) error {
	token, err := c.AccessToken(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url+path, nil)
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
		return refusal(res)
	}
	sc := bufio.NewScanner(res.Body)
	for sc.Scan() {
		if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
			on(data)
		}
	}
	return sc.Err()
}
