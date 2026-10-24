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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	assert.Equal(t, "ask", l.Msg.Show, "the default")
	assert.Equal(t, "ask", l.Msg.Open, "the default")
	_, err = c.SetConfigValue(ctx, connect.NewRequest(&pb.SetConfigValueRequest{Key: "browser.show", Value: "silent"}))
	require.NoError(t, err)
	_, err = c.SetConfigValue(ctx, connect.NewRequest(&pb.SetConfigValueRequest{Key: "browser.open", Value: "system"}))
	require.NoError(t, err)
	l, err = c.ListBrowserProfiles(ctx, connect.NewRequest(&pb.ListBrowserProfilesRequest{}))
	require.NoError(t, err)
	assert.Equal(t, "silent", l.Msg.Show)
	assert.Equal(t, "system", l.Msg.Open)
	_, err = c.SetConfigValue(ctx, connect.NewRequest(&pb.SetConfigValueRequest{Key: "browser.show", Value: "sometimes"}))
	assert.Error(t, err)
	_, err = c.SetConfigValue(ctx, connect.NewRequest(&pb.SetConfigValueRequest{Key: "browser.open", Value: "elsewhere"}))
	assert.ErrorContains(t, err, "ask, embedded, system")

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

// Signing in where there's no browser, or one that won't start, says so;
// a profile that's gone can't be cleared or given sites.
func TestBrowserProfilesFailOverTheAPI(t *testing.T) {
	_, s := serve(t, nil)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	c := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()
	_, err := c.CreateBrowserProfile(ctx, connect.NewRequest(&pb.CreateBrowserProfileRequest{Name: "work"}))
	require.NoError(t, err)
	open := func() error {
		_, err := c.OpenBrowserProfile(ctx, connect.NewRequest(&pb.OpenBrowserProfileRequest{Name: "work"}))
		return err
	}

	settings := func(toml string) {
		dir := filepath.Join(os.Getenv("HOME"), ".blitz")
		require.NoError(t, os.MkdirAll(dir, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".env.toml"), []byte(toml), 0o600))
	}

	settings("[[[ not toml")
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(open()), "settings it can't read")
	settings("[browser]\npath = \"" + filepath.Join(t.TempDir(), "no-chrome") + "\"\n")
	_, info := errorReason(t, open())
	assert.Equal(t, "NO_BROWSER", info.Reason)

	chrome := filepath.Join(t.TempDir(), "chrome")
	require.NoError(t, os.WriteFile(chrome, []byte("#!/bin/sh\n"), 0o755))
	settings("[browser]\npath = \"" + chrome + "\"\n")
	old := openBrowser
	openBrowser = func(string, ...string) error { return errors.New("won't start") }
	t.Cleanup(func() { openBrowser = old })
	assert.ErrorContains(t, open(), "won't start")

	_, err = c.DeleteBrowserProfile(ctx, connect.NewRequest(&pb.DeleteBrowserProfileRequest{Name: "work"}))
	require.NoError(t, err)
	for name, call := range map[string]func() error{
		"clear": func() error {
			_, err := c.ClearBrowserProfile(ctx, connect.NewRequest(&pb.ClearBrowserProfileRequest{Name: "work"}))
			return err
		},
		"sites": func() error {
			_, err := c.SetBrowserProfileDomains(ctx, connect.NewRequest(&pb.SetBrowserProfileDomainsRequest{Name: "work", AllowDomains: []string{"example.com"}}))
			return err
		},
	} {
		t.Run("a profile that's gone: "+name, func(t *testing.T) {
			_, info := errorReason(t, call())
			assert.Equal(t, "NO_BROWSER_PROFILE", info.Reason)
		})
	}
}

// The window's side of the agent's browser (spec_agent_browser_050): it
// watches until it goes, and a reply for a browser that isn't open, or a
// workspace that isn't one, is an error.
func TestAgentBrowserOverTheAPI(t *testing.T) {
	c, _ := serve(t, nil)
	ws := t.TempDir()
	ctx := context.Background()

	// watch is how the watch ends, whether at the call or the stream.
	watch := func(dir string) error {
		wctx, stop := context.WithTimeout(ctx, 100*time.Millisecond)
		defer stop()
		stream, err := c.workspaces.WatchAgentBrowser(wctx, connect.NewRequest(&pb.WatchAgentBrowserRequest{Workspace: dir}))
		if err != nil {
			return err
		}
		defer stream.Close()
		assert.False(t, stream.Receive(), "nothing asked of it")
		return stream.Err()
	}
	// Opened first, so the watch's short deadline is only the watch's.
	_, err := c.workspaces.ListProcesses(ctx, connect.NewRequest(&pb.ListProcessesRequest{Workspace: ws}))
	require.NoError(t, err)
	// The deadline ends it: the client's error, or the server's clean end
	// when that comes first.
	if werr := watch(ws); werr != nil {
		assert.Equal(t, connect.CodeDeadlineExceeded, connect.CodeOf(werr), "watched until it went: %v", werr)
	}

	_, err = c.workspaces.AgentBrowserReply(ctx, connect.NewRequest(&pb.AgentBrowserReplyRequest{Workspace: ws, BrowserId: "b-1", Closed: true}))
	_, info := errorReason(t, err)
	assert.Equal(t, "NO_AGENT_BROWSER", info.Reason)

	gone := filepath.Join(t.TempDir(), "gone")
	_, err = c.workspaces.AgentBrowserReply(ctx, connect.NewRequest(&pb.AgentBrowserReplyRequest{Workspace: gone, BrowserId: "b-1"}))
	assert.Error(t, err)
	err = watch(gone)
	assert.Error(t, err)
	assert.NotEqual(t, connect.CodeDeadlineExceeded, connect.CodeOf(err), "refused, not watched")
}
