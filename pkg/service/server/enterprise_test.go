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
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/enterprise"
	"github.com/retail-cortex/blitz/pkg/enterprise/enterprisetest"
	"github.com/retail-cortex/blitz/pkg/secrets"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Connecting to the organization's service, signing in through the
// browser and with a code, checking it, signing out and disconnecting,
// as the desktop app and the CLI do it (spec_053_enterprise EN-10 to
// EN-14).
func TestEnterpriseJourney(t *testing.T) {
	// Registered first, so it runs last: the calls' kept connections end
	// with the servers they went to.
	t.Cleanup(http.DefaultClient.CloseIdleConnections)
	_, s := serve(t, nil)
	secrets.SetDefault(&secrets.Memory{})
	idp := enterprisetest.NewProvider(t)
	org := enterprisetest.NewService(t, idp.URL)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	c := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()

	got, err := c.GetEnterprise(ctx, connect.NewRequest(&pb.GetEnterpriseRequest{}))
	require.NoError(t, err)
	assert.Empty(t, got.Msg.State.Url, "not connected")

	// Signing in before connecting says to connect.
	signIn := func(device bool) (*pb.EnterpriseSignInResponse, []*pb.EnterpriseSignInResponse, error) {
		stream, err := c.EnterpriseSignIn(ctx, connect.NewRequest(&pb.EnterpriseSignInRequest{Device: device}))
		require.NoError(t, err)
		defer stream.Close()
		var seen []*pb.EnterpriseSignInResponse
		for stream.Receive() {
			m := stream.Msg()
			seen = append(seen, m)
			if m.Done {
				return m, seen, nil
			}
			if m.Url != "" && m.Code == "" {
				enterprisetest.Browse(t)(m.Url)
			}
		}
		return nil, seen, stream.Err()
	}
	_, _, err = signIn(false)
	code, info := errorReason(t, err)
	assert.Equal(t, connect.CodeFailedPrecondition, code)
	assert.Equal(t, "NOT_CONNECTED", info.Reason)

	t.Run("addresses that aren't a service", func(t *testing.T) {
		_, err := c.ConnectEnterprise(ctx, connect.NewRequest(&pb.ConnectEnterpriseRequest{Url: "http://blitz.acme.com"}))
		_, info := errorReason(t, err)
		assert.Equal(t, "INVALID_ADDRESS", info.Reason)
		_, err = c.ConnectEnterprise(ctx, connect.NewRequest(&pb.ConnectEnterpriseRequest{Url: "http://127.0.0.1:1"}))
		code, info := errorReason(t, err)
		assert.Equal(t, connect.CodeUnavailable, code)
		assert.Equal(t, "UNREACHABLE", info.Reason)
	})

	conn, err := c.ConnectEnterprise(ctx, connect.NewRequest(&pb.ConnectEnterpriseRequest{Url: org.URL + "/"}))
	require.NoError(t, err)
	st := conn.Msg.State
	assert.Equal(t, org.URL, st.Url)
	assert.Equal(t, "Acme Assist", st.Name)
	assert.Equal(t, []string{"gateway", "jobs", "branding"}, st.Capabilities)
	assert.True(t, st.Reachable)
	assert.False(t, st.SignedIn)
	cfg, err := config.Load("")
	require.NoError(t, err)
	assert.Equal(t, org.URL, cfg.Enterprise.URL, "kept in the global settings")
	models, err := c.ListModels(ctx, connect.NewRequest(&pb.ListModelsRequest{}))
	require.NoError(t, err)
	assert.Equal(t, "enterprise", models.Msg.DefaultProvider, "the organization's models are the models")
	require.Len(t, models.Msg.Providers, 1)
	assert.Equal(t, "enterprise", models.Msg.Providers[0].Provider)

	done, seen, err := signIn(false)
	require.NoError(t, err)
	assert.Contains(t, seen[0].Url, idp.URL+"/authorize?", "the address to open")
	assert.True(t, done.State.SignedIn)
	assert.Equal(t, "ana@acme.com", done.State.Email)
	assert.Equal(t, []string{"blitz dev"}, org.SignIns, "the service records the app and its version")

	checked, err := c.GetEnterprise(ctx, connect.NewRequest(&pb.GetEnterpriseRequest{Check: true}))
	require.NoError(t, err)
	assert.True(t, checked.Msg.State.Checked)
	assert.True(t, checked.Msg.State.Reachable)
	assert.True(t, checked.Msg.State.SignedIn)

	out, err := c.EnterpriseSignOut(ctx, connect.NewRequest(&pb.EnterpriseSignOutRequest{}))
	require.NoError(t, err)
	assert.False(t, out.Msg.State.SignedIn)
	assert.True(t, org.Ended["s1"])

	t.Run("with a code, for the CLI", func(t *testing.T) {
		done, seen, err := signIn(true)
		require.NoError(t, err)
		assert.Equal(t, "ABCD-EFGH", seen[0].Code)
		assert.Equal(t, "https://www.google.com/device", seen[0].Url)
		assert.True(t, done.State.SignedIn)
	})
	t.Run("one sign-in at a time", func(t *testing.T) {
		s.ent.signingIn.Lock()
		defer s.ent.signingIn.Unlock()
		_, _, err := signIn(false)
		_, info := errorReason(t, err)
		assert.Equal(t, "SIGN_IN_RUNNING", info.Reason)
	})
	t.Run("refused", func(t *testing.T) {
		idp.IDToken = "for-someone-else"
		defer func() { idp.IDToken = "" }()
		_, _, err := signIn(false)
		code, info := errorReason(t, err)
		assert.Equal(t, connect.CodeUnauthenticated, code)
		assert.Equal(t, "SIGN_IN_FAILED", info.Reason)
	})
	t.Run("the service unreachable when checked", func(t *testing.T) {
		down := enterprisetest.NewService(t, idp.URL)
		_, err := c.ConnectEnterprise(ctx, connect.NewRequest(&pb.ConnectEnterpriseRequest{Url: down.URL}))
		require.NoError(t, err)
		down.Close()
		got, err := c.GetEnterprise(ctx, connect.NewRequest(&pb.GetEnterpriseRequest{Check: true}))
		require.NoError(t, err)
		assert.False(t, got.Msg.State.Reachable)
		assert.Contains(t, got.Msg.State.Error, "reaching the organization's service")
	})

	gone, err := c.ConnectEnterprise(ctx, connect.NewRequest(&pb.ConnectEnterpriseRequest{}))
	require.NoError(t, err)
	assert.Empty(t, gone.Msg.State.Url)
	cfg, err = config.Load("")
	require.NoError(t, err)
	assert.Empty(t, cfg.Enterprise.URL)
	out, err = c.EnterpriseSignOut(ctx, connect.NewRequest(&pb.EnterpriseSignOutRequest{}))
	require.NoError(t, err)
	assert.Empty(t, out.Msg.State.Url, "nothing to sign out of")
}

// What goes wrong: settings that can't be read or name no service, the
// organization's service down when signing out or switching (signed out
// here all the same), and a sign-in given up on.
func TestEnterpriseErrors(t *testing.T) {
	t.Cleanup(http.DefaultClient.CloseIdleConnections)
	_, s := serve(t, nil)
	secrets.SetDefault(&secrets.Memory{})
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	c := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	settings := filepath.Join(home, ".blitz", ".env.toml")
	require.NoError(t, os.MkdirAll(filepath.Dir(settings), 0o700))

	for name, file := range map[string]string{
		"unreadable settings":     "[enterprise\n",
		"not a service's address": "[enterprise]\nurl = \"http://evil.example\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, os.WriteFile(settings, []byte(file), 0o600))
			_, err := c.GetEnterprise(ctx, connect.NewRequest(&pb.GetEnterpriseRequest{}))
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
			_, err = c.EnterpriseSignOut(ctx, connect.NewRequest(&pb.EnterpriseSignOutRequest{}))
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
			stream, err := c.EnterpriseSignIn(ctx, connect.NewRequest(&pb.EnterpriseSignInRequest{}))
			require.NoError(t, err)
			for stream.Receive() {
			}
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(stream.Err()))
			require.NoError(t, stream.Close())
		})
	}
	require.NoError(t, os.Remove(settings))

	idp := enterprisetest.NewProvider(t)
	org := enterprisetest.NewService(t, idp.URL)
	s.ent.http = &http.Client{} // the service's own client, as a proxy's would be
	_, err = c.ConnectEnterprise(ctx, connect.NewRequest(&pb.ConnectEnterpriseRequest{Url: org.URL}))
	require.NoError(t, err)

	t.Run("a sign-in given up on", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		stream, err := c.EnterpriseSignIn(cctx, connect.NewRequest(&pb.EnterpriseSignInRequest{}))
		require.NoError(t, err)
		require.True(t, stream.Receive(), "the address to open")
		cancel()
		for stream.Receive() {
		}
		assert.Equal(t, connect.CodeCanceled, connect.CodeOf(stream.Err()))
		_ = stream.Close()
	})

	signIn := func() {
		stream, err := c.EnterpriseSignIn(ctx, connect.NewRequest(&pb.EnterpriseSignInRequest{Device: true}))
		require.NoError(t, err)
		defer stream.Close()
		for stream.Receive() {
		}
		require.NoError(t, stream.Err())
	}
	signIn()
	org.Offline = true
	out, err := c.EnterpriseSignOut(ctx, connect.NewRequest(&pb.EnterpriseSignOutRequest{}))
	require.NoError(t, err, "signed out here, the service down")
	assert.False(t, out.Msg.State.SignedIn)

	org.Offline = false
	signIn()
	org.Offline = true
	gone, err := c.ConnectEnterprise(ctx, connect.NewRequest(&pb.ConnectEnterpriseRequest{}))
	require.NoError(t, err, "disconnected, the service down")
	assert.Empty(t, gone.Msg.State.Url)
}

// The organization's branding and terms through the service: none until
// connected, kept after a refresh and read back offline, the terms asked
// for and accepted, and all of it gone on disconnecting (EN-50 to EN-54).
func TestEnterpriseBrandingAndTerms(t *testing.T) {
	t.Cleanup(http.DefaultClient.CloseIdleConnections)
	_, s := serve(t, nil)
	secrets.SetDefault(&secrets.Memory{})
	idp := enterprisetest.NewProvider(t)
	org := enterprisetest.NewService(t, idp.URL)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	c := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()

	b, err := c.GetBranding(ctx, connect.NewRequest(&pb.GetBrandingRequest{Refresh: true}))
	require.NoError(t, err)
	assert.Nil(t, b.Msg.Branding, "not connected")
	_, err = c.GetTerms(ctx, connect.NewRequest(&pb.GetTermsRequest{}))
	_, info := errorReason(t, err)
	assert.Equal(t, "NOT_CONNECTED", info.Reason)
	_, err = c.AcceptTerms(ctx, connect.NewRequest(&pb.AcceptTermsRequest{Version: "v1"}))
	_, info = errorReason(t, err)
	assert.Equal(t, "NOT_CONNECTED", info.Reason)

	org.Branding = map[string]any{"name": "Acme Assist", "accent": "#1a56db", "images": map[string]string{"logo.svg": "data:image/svg+xml;base64,PHN2Zy8+"},
		"terms": []map[string]string{{"document": "privacy", "version": "p1", "url": "https://acme.com/privacy"}}}
	_, err = c.ConnectEnterprise(ctx, connect.NewRequest(&pb.ConnectEnterpriseRequest{Url: org.URL}))
	require.NoError(t, err)
	b, err = c.GetBranding(ctx, connect.NewRequest(&pb.GetBrandingRequest{}))
	require.NoError(t, err)
	assert.Nil(t, b.Msg.Branding, "none kept yet")
	b, err = c.GetBranding(ctx, connect.NewRequest(&pb.GetBrandingRequest{Refresh: true}))
	require.NoError(t, err)
	require.NotNil(t, b.Msg.Branding, b.Msg.Error)
	assert.Equal(t, "Acme Assist", b.Msg.Branding.Name)
	assert.Equal(t, "https://acme.com/privacy", b.Msg.Branding.Documents[0].Url)

	org.Offline = true
	org.Close() // offline: the kept copy
	b, err = c.GetBranding(ctx, connect.NewRequest(&pb.GetBrandingRequest{Refresh: true}))
	require.NoError(t, err)
	assert.Equal(t, "Acme Assist", b.Msg.Branding.GetName())
	assert.Contains(t, b.Msg.Error, "reaching the organization's service")

	// The terms, with a service that answers.
	org2 := enterprisetest.NewService(t, idp.URL)
	org2.Version = "v2"
	_, err = c.ConnectEnterprise(ctx, connect.NewRequest(&pb.ConnectEnterpriseRequest{Url: org2.URL}))
	require.NoError(t, err)
	_, err = c.GetTerms(ctx, connect.NewRequest(&pb.GetTermsRequest{}))
	_, info = errorReason(t, err)
	assert.Equal(t, "NOT_SIGNED_IN", info.Reason)
	stream, err := c.EnterpriseSignIn(ctx, connect.NewRequest(&pb.EnterpriseSignInRequest{Device: true}))
	require.NoError(t, err)
	for stream.Receive() {
	}
	require.NoError(t, stream.Err())
	_ = stream.Close()
	terms, err := c.GetTerms(ctx, connect.NewRequest(&pb.GetTermsRequest{}))
	require.NoError(t, err)
	assert.Equal(t, "v2", terms.Msg.Version)
	assert.False(t, terms.Msg.Accepted)
	assert.Equal(t, "Be kind.", terms.Msg.Text)
	_, err = c.AcceptTerms(ctx, connect.NewRequest(&pb.AcceptTermsRequest{Version: "v1"}))
	code, info := errorReason(t, err)
	assert.Equal(t, connect.CodeAborted, code)
	assert.Equal(t, "CONFLICT", info.Reason)
	accepted, err := c.AcceptTerms(ctx, connect.NewRequest(&pb.AcceptTermsRequest{Version: "v2"}))
	require.NoError(t, err)
	assert.Empty(t, accepted.Msg.State.TermsVersion)

	// The first organization's folder went when Blitz moved to the second;
	// disconnecting removes the second's.
	home, _ := os.UserHomeDir()
	entries, _ := os.ReadDir(filepath.Join(home, ".blitz", "enterprise"))
	assert.Empty(t, entries, "the second published no branding; the first's is gone")
	_, err = c.ConnectEnterprise(ctx, connect.NewRequest(&pb.ConnectEnterpriseRequest{}))
	require.NoError(t, err)
	b, err = c.GetBranding(ctx, connect.NewRequest(&pb.GetBrandingRequest{Refresh: true}))
	require.NoError(t, err)
	assert.Nil(t, b.Msg.Branding)

	t.Run("errors from the service", func(t *testing.T) {
		assert.Equal(t, "UNREACHABLE", errorInfo(enterpriseError(errors.New("connection refused"))).GetReason())
		assert.Equal(t, "TERMS_REQUIRED", errorInfo(enterpriseError(&enterprise.Refusal{Status: http.StatusForbidden, Code: "terms_required"})).GetReason())
	})
}

// The service keeps the organization's catalog current: fetched once
// signed in, again for each version its stream announces, loaded into
// the settings, and removed on disconnecting (EN-30 to EN-33).
func TestEnterpriseCatalogLoop(t *testing.T) {
	t.Cleanup(http.DefaultClient.CloseIdleConnections)
	_, s := serve(t, nil)
	secrets.SetDefault(&secrets.Memory{})
	s.ent.retry, s.ent.idle = 50*time.Millisecond, 50*time.Millisecond
	idp := enterprisetest.NewProvider(t)
	org := enterprisetest.NewService(t, idp.URL)
	org.Catalog = map[string]enterprisetest.CatalogFile{
		"agents/reviewer.md": {Kind: "agent", Name: "reviewer", Lock: "locked", Data: "---\nname: reviewer\n---\nReview."},
	}
	org.CatalogPolicy = map[string]any{"tools": map[string]any{"off": []string{"web_fetch"}}}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	c := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()

	_, err := c.ConnectEnterprise(ctx, connect.NewRequest(&pb.ConnectEnterpriseRequest{Url: org.URL}))
	require.NoError(t, err)
	dir := config.EnterpriseDir("", org.URL)
	time.Sleep(100 * time.Millisecond)
	assert.NoFileExists(t, filepath.Join(dir, "policy.json"), "not signed in: nothing fetched")

	stream, err := c.EnterpriseSignIn(ctx, connect.NewRequest(&pb.EnterpriseSignInRequest{Device: true}))
	require.NoError(t, err)
	for stream.Receive() {
	}
	require.NoError(t, stream.Err())
	_ = stream.Close()
	locked := filepath.Join(dir, "catalog", "locked")
	require.Eventually(t, func() bool { _, err := os.Stat(filepath.Join(locked, "agents", "reviewer.md")); return err == nil }, 5*time.Second, 20*time.Millisecond)
	cfg, err := config.Load("")
	require.NoError(t, err)
	assert.True(t, cfg.Org.Active())
	assert.Contains(t, cfg.Permissions.Deny, "web_fetch")

	org.Mu.Lock()
	org.Catalog["rules/style.md"] = enterprisetest.CatalogFile{Kind: "rule", Name: "style", Lock: "locked", Data: "Use Go."}
	org.Mu.Unlock()
	org.Announce <- 2
	require.Eventually(t, func() bool { _, err := os.Stat(filepath.Join(locked, "rules", "style.md")); return err == nil }, 5*time.Second, 20*time.Millisecond)

	_, err = c.ConnectEnterprise(ctx, connect.NewRequest(&pb.ConnectEnterpriseRequest{}))
	require.NoError(t, err)
	assert.NoDirExists(t, dir)
	cfg, err = config.Load("")
	require.NoError(t, err)
	assert.False(t, cfg.Org.Active())
}

// The organization's workspaces: listed with where each would be cloned,
// cloned through the service with Blitz's credential helper set in the
// clone, opened from the clone after, made anew, and Git's credential
// answered for the service's host only (EN-60, EN-61, EN-65).
func TestOrgWorkspaces(t *testing.T) {
	t.Cleanup(http.DefaultClient.CloseIdleConnections)
	_, s := serve(t, nil)
	secrets.SetDefault(&secrets.Memory{})
	idp := enterprisetest.NewProvider(t)
	org := enterprisetest.NewService(t, idp.URL)
	org.AddWorkspace(t, "plans", "Plans", "editor")
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	c := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()

	_, err := c.ListOrgWorkspaces(ctx, connect.NewRequest(&pb.ListOrgWorkspacesRequest{}))
	_, info := errorReason(t, err)
	assert.Equal(t, "NOT_CONNECTED", info.Reason)
	_, err = c.ConnectEnterprise(ctx, connect.NewRequest(&pb.ConnectEnterpriseRequest{Url: org.URL}))
	require.NoError(t, err)
	_, err = c.ListOrgWorkspaces(ctx, connect.NewRequest(&pb.ListOrgWorkspacesRequest{}))
	_, info = errorReason(t, err)
	assert.Equal(t, "NOT_SIGNED_IN", info.Reason)

	stream, err := c.EnterpriseSignIn(ctx, connect.NewRequest(&pb.EnterpriseSignInRequest{Device: true}))
	require.NoError(t, err)
	for stream.Receive() {
	}
	require.NoError(t, stream.Err())
	_ = stream.Close()

	list, err := c.ListOrgWorkspaces(ctx, connect.NewRequest(&pb.ListOrgWorkspacesRequest{}))
	require.NoError(t, err)
	home, _ := os.UserHomeDir()
	assert.Equal(t, "Acme Assist", list.Msg.Organization)
	require.Len(t, list.Msg.Workspaces, 1)
	ws := list.Msg.Workspaces[0]
	assert.Equal(t, "editor", ws.Role)
	assert.Equal(t, filepath.Join(home, "Blitz", "Acme Assist", "plans"), ws.Dir)
	assert.False(t, ws.Cloned)

	// Git asks the helper; here a script stands in for blitzd's.
	token, err := s.ent.c.AccessToken(ctx)
	require.NoError(t, err)
	t.Setenv("BLITZ_TEST_TOKEN", token)
	s.ent.gitHelper = `!f() { echo username=blitz; echo "password=$BLITZ_TEST_TOKEN"; }; f`
	opened, err := c.OpenOrgWorkspace(ctx, connect.NewRequest(&pb.OpenOrgWorkspaceRequest{Handle: "plans"}))
	require.NoError(t, err)
	assert.True(t, opened.Msg.Cloned)
	assert.FileExists(t, filepath.Join(ws.Dir, "README.md"))
	helper, err := exec.Command("git", "-C", ws.Dir, "config", "credential."+org.URL+".helper").Output()
	require.NoError(t, err)
	assert.Contains(t, string(helper), "BLITZ_TEST_TOKEN", "the clone's own settings name the helper")
	again, err := c.OpenOrgWorkspace(ctx, connect.NewRequest(&pb.OpenOrgWorkspaceRequest{Handle: "plans"}))
	require.NoError(t, err)
	assert.False(t, again.Msg.Cloned, "opened from the clone")

	_, err = c.OpenOrgWorkspace(ctx, connect.NewRequest(&pb.OpenOrgWorkspaceRequest{Handle: "nope"}))
	_, info = errorReason(t, err)
	assert.Equal(t, "NOT_FOUND", info.Reason)

	made, err := c.CreateOrgWorkspace(ctx, connect.NewRequest(&pb.CreateOrgWorkspaceRequest{Name: "Q4 Launch"}))
	require.NoError(t, err)
	assert.Equal(t, "q4-launch", made.Msg.Workspace.Handle)
	assert.Equal(t, "owner", made.Msg.Workspace.Role)

	t.Setenv("BLITZ_TEST_TOKEN", "not-a-token")
	_, err = c.OpenOrgWorkspace(ctx, connect.NewRequest(&pb.OpenOrgWorkspaceRequest{Handle: "q4-launch"}))
	_, info = errorReason(t, err)
	assert.Equal(t, "CLONE_FAILED", info.Reason)
	assert.NoDirExists(t, filepath.Join(home, "Blitz", "Acme Assist", "q4-launch"), "nothing half-cloned left")

	cred, err := c.GitCredential(ctx, connect.NewRequest(&pb.GitCredentialRequest{Host: strings.TrimPrefix(org.URL, "http://")}))
	require.NoError(t, err)
	assert.True(t, cred.Msg.Ok)
	assert.Equal(t, token, cred.Msg.Password)
	other, err := c.GitCredential(ctx, connect.NewRequest(&pb.GitCredentialRequest{Host: "github.com"}))
	require.NoError(t, err)
	assert.False(t, other.Msg.Ok)
	_, err = c.EnterpriseSignOut(ctx, connect.NewRequest(&pb.EnterpriseSignOutRequest{}))
	require.NoError(t, err)
	out, err := c.GitCredential(ctx, connect.NewRequest(&pb.GitCredentialRequest{Host: strings.TrimPrefix(org.URL, "http://")}))
	require.NoError(t, err)
	assert.False(t, out.Msg.Ok, "signed out: nothing, so Git says to sign in")
}
