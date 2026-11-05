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
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
)

// sharedRemote is the Git remote a folder shared with the organization
// gets for it: its own origin, if it has one, stays as it was.
const sharedRemote = "organization"

// sharedFolder is a folder shared with the organization where it was,
// instead of a clone in the organization's folder (EN-66).
type sharedFolder struct {
	Dir    string `json:"dir"`
	Remote string `json:"remote"`
}

// sharedFolders are the folders shared with each organization's service,
// by its address and the workspace's handle, kept in
// ~/.blitz/org-folders.json.
type sharedFolders map[string]map[string]sharedFolder

var sharedMu sync.Mutex

func sharedPath() string { return filepath.Join(config.ConfigDir(""), "org-folders.json") }

// loadShared reads the shared folders (none when there's no file).
func loadShared() (sharedFolders, error) {
	data, err := os.ReadFile(sharedPath())
	if errors.Is(err, os.ErrNotExist) {
		return sharedFolders{}, nil
	}
	if err != nil {
		return nil, err
	}
	var f sharedFolders
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	return f, nil
}

// sharedFor is service's shared folders by handle (none on any failure).
func sharedFor(service string) map[string]sharedFolder {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	f, err := loadShared()
	if err != nil {
		return nil
	}
	return f[service]
}

// addShared records dir as service's workspace handle.
func addShared(service, handle string, sf sharedFolder) error {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	f, err := loadShared()
	if err != nil {
		return err
	}
	if f[service] == nil {
		f[service] = map[string]sharedFolder{}
	}
	f[service][handle] = sf
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(sharedPath()), 0o700); err != nil {
		return err
	}
	tmp := sharedPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, sharedPath())
}

// samePath reports whether a and b are the same folder, links followed
// (as canonical; a path that can't be resolved is compared as given).
func samePath(a, b string) bool {
	ka, err := canonical(a)
	if err != nil {
		ka = filepath.Clean(a)
	}
	kb, err := canonical(b)
	if err != nil {
		kb = filepath.Clean(b)
	}
	return ka == kb
}

// orgDir is where handle's workspace is on this computer, and its remote:
// a folder shared from here, else its clone in folder.
func orgDir(shared map[string]sharedFolder, folder, handle string) (string, string) {
	if sf, ok := shared[handle]; ok && sf.Dir != "" {
		return sf.Dir, sf.Remote
	}
	return filepath.Join(folder, filepath.FromSlash(handle)), "origin"
}

// OrgWorkspaceFor is the organization's workspace at dir, asked as the
// workspace opens: its ID ("" when dir isn't one; each model call carries
// it, spec_053 EN-22), and why it's read-only ("" when it isn't): it's a
// clone the person is only a Viewer of (EN-60). When the service can't be
// reached it's what the service last said for dir (EN-37), else neither
// (a Viewer's push is refused there anyway).
func (s *Server) OrgWorkspaceFor(ctx context.Context, dir string) (id, readOnly string) {
	c, cfg, err := s.orgClient()
	if err != nil || !c.Status().SignedIn {
		return "", ""
	}
	key, err := canonical(dir)
	if err != nil {
		return "", ""
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	list, err := c.Workspaces(ctx)
	if err != nil {
		// Offline (EN-37): what it was when the service last said.
		s.ent.orgForMu.Lock()
		defer s.ent.orgForMu.Unlock()
		last := s.ent.orgFor[key]
		return last[0], last[1]
	}
	defer func() {
		s.ent.orgForMu.Lock()
		defer s.ent.orgForMu.Unlock()
		if s.ent.orgFor == nil {
			s.ent.orgFor = map[string][2]string{}
		}
		s.ent.orgFor[key] = [2]string{id, readOnly}
	}()
	folder, shared := s.orgFolder(c, cfg), sharedFor(c.URL())
	for _, w := range list {
		at, _ := orgDir(shared, folder, w.Handle)
		if k, err := canonical(at); err != nil || k != key {
			continue
		}
		if w.Role == "viewer" {
			readOnly = fmt.Sprintf("you're a Viewer of %s's workspace %s: ask its owner to make you an Editor to change it", filepath.Base(folder), cmp.Or(w.Name, w.Handle))
		}
		return w.ID, readOnly
	}
	return "", ""
}
