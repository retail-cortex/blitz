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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The browser profiles over the API (spec_browser_profiles_049): made,
// chosen for a workspace, given sites, opened in Chrome to sign in,
// cleared and deleted.
func TestBrowserProfilesOverTheAPI(t *testing.T) {
	_, s := serve(t, nil)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	c := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()
	ws := t.TempDir()

	_, err := c.CreateBrowserProfile(ctx, connect.NewRequest(&pb.CreateBrowserProfileRequest{Name: "work"}))
	require.NoError(t, err)
	_, err = c.CreateBrowserProfile(ctx, connect.NewRequest(&pb.CreateBrowserProfileRequest{Name: "Not Valid"}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	_, err = c.SetWorkspaceBrowserProfile(ctx, connect.NewRequest(&pb.SetWorkspaceBrowserProfileRequest{Workspace: ws, Name: "work"}))
	require.NoError(t, err)
	d, err := c.SetBrowserProfileDomains(ctx, connect.NewRequest(&pb.SetBrowserProfileDomainsRequest{Name: "work", AllowDomains: []string{" Mail.Example.com ", ""}}))
	require.NoError(t, err)
	assert.Equal(t, []string{"mail.example.com"}, d.Msg.Profile.AllowDomains)
	l, err := c.ListBrowserProfiles(ctx, connect.NewRequest(&pb.ListBrowserProfilesRequest{Workspace: ws}))
	require.NoError(t, err)
	require.Len(t, l.Msg.Profiles, 1)
	assert.Equal(t, "work", l.Msg.WorkspaceProfile)

	// Signing in by hand: the system's browser on the profile's folder.
	chrome := filepath.Join(t.TempDir(), "chrome")
	require.NoError(t, os.WriteFile(chrome, []byte("#!/bin/sh\n"), 0o755))
	t.Setenv("PATH", filepath.Dir(chrome)+string(os.PathListSeparator)+os.Getenv("PATH"))
	var opened []string
	old := openBrowser
	openBrowser = func(path string, args ...string) error { opened = append([]string{path}, args...); return nil }
	t.Cleanup(func() { openBrowser = old })
	o, err := c.OpenBrowserProfile(ctx, connect.NewRequest(&pb.OpenBrowserProfileRequest{Name: "work", Url: "https://mail.example.com/login"}))
	require.NoError(t, err)
	assert.NotEmpty(t, o.Msg.Browser)
	require.NotEmpty(t, opened)
	assert.Contains(t, opened, "https://mail.example.com/login")
	assert.Contains(t, opened[1], filepath.Join("browser", "profiles", "work"))
	_, err = c.OpenBrowserProfile(ctx, connect.NewRequest(&pb.OpenBrowserProfileRequest{Name: "work", Url: "file:///etc/passwd"}))
	require.NoError(t, err)
	assert.Contains(t, opened, "about:blank", "only web pages are opened")

	_, err = c.ClearBrowserProfile(ctx, connect.NewRequest(&pb.ClearBrowserProfileRequest{Name: "work"}))
	require.NoError(t, err)
	_, err = c.DeleteBrowserProfile(ctx, connect.NewRequest(&pb.DeleteBrowserProfileRequest{Name: "work"}))
	require.NoError(t, err)
	l, err = c.ListBrowserProfiles(ctx, connect.NewRequest(&pb.ListBrowserProfilesRequest{Workspace: ws}))
	require.NoError(t, err)
	assert.Empty(t, l.Msg.Profiles)
	assert.Empty(t, l.Msg.WorkspaceProfile)
	for name, call := range map[string]func() error{
		"delete": func() error {
			_, err := c.DeleteBrowserProfile(ctx, connect.NewRequest(&pb.DeleteBrowserProfileRequest{Name: "work"}))
			return err
		},
		"open": func() error {
			_, err := c.OpenBrowserProfile(ctx, connect.NewRequest(&pb.OpenBrowserProfileRequest{Name: "work"}))
			return err
		},
		"choose": func() error {
			_, err := c.SetWorkspaceBrowserProfile(ctx, connect.NewRequest(&pb.SetWorkspaceBrowserProfileRequest{Workspace: ws, Name: "work"}))
			return err
		},
	} {
		t.Run("a profile that's gone: "+name, func(t *testing.T) {
			_, info := errorReason(t, call())
			assert.Equal(t, "NO_BROWSER_PROFILE", info.Reason)
		})
	}
}
