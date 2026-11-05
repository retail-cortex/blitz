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
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
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
		wait := signInWait
		signInWait = 10 * time.Millisecond
		defer func() { signInWait = wait }()
		end, ok := s.ent.beginSignIn(context.Background())
		require.True(t, ok)
		defer end()
		_, _, err := signIn(false)
		code, info := errorReason(t, err)
		assert.Equal(t, "SIGN_IN_RUNNING", info.Reason)
		assert.Equal(t, connect.CodeAlreadyExists, code, "as a provider's")
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
		assert.Equal(t, "REFUSED", errorInfo(enterpriseError(&enterprise.Refusal{Status: http.StatusForbidden})).GetReason(), "a refusal with no code")
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
	org.CatalogPolicy = map[string]any{"tools": map[string]any{"off": []string{"web_fetch"}}, "refresh": "1h"}
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
	// The policy is written beside the catalog, not with it.
	var cfg *config.Config
	require.Eventually(t, func() bool {
		cfg, err = config.Load("")
		return err == nil && slices.Contains(cfg.Permissions.Deny, "web_fetch")
	}, 5*time.Second, 20*time.Millisecond)
	assert.True(t, cfg.Org.Active())

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

	// Someone's own files where the clone would go are left as they are.
	require.NoError(t, os.MkdirAll(ws.Dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ws.Dir, "mine.md"), []byte("Mine."), 0o644))
	_, err = c.OpenOrgWorkspace(ctx, connect.NewRequest(&pb.OpenOrgWorkspaceRequest{Handle: "plans"}))
	_, info = errorReason(t, err)
	assert.Equal(t, "FOLDER_IN_THE_WAY", info.Reason)
	mine, err := os.ReadFile(filepath.Join(ws.Dir, "mine.md"))
	require.NoError(t, err)
	assert.Equal(t, "Mine.", string(mine))
	assert.NoDirExists(t, filepath.Join(ws.Dir, ".git"))
	beside, err := filepath.Glob(filepath.Join(filepath.Dir(ws.Dir), ".plans-*"))
	require.NoError(t, err)
	assert.Empty(t, beside, "the clone beside it is gone")
	require.NoError(t, os.RemoveAll(ws.Dir))

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
	for _, tc := range []struct {
		protocol string
		ok       bool
	}{{"http", true}, {"https", false}, {"", true}} { // the test service is http://
		got, err := c.GitCredential(ctx, connect.NewRequest(&pb.GitCredentialRequest{Host: strings.TrimPrefix(org.URL, "http://"), Protocol: tc.protocol}))
		require.NoError(t, err)
		assert.Equal(t, tc.ok, got.Msg.Ok, "asked over %q", tc.protocol)
	}
	_, err = c.EnterpriseSignOut(ctx, connect.NewRequest(&pb.EnterpriseSignOutRequest{}))
	require.NoError(t, err)
	out, err := c.GitCredential(ctx, connect.NewRequest(&pb.GitCredentialRequest{Host: strings.TrimPrefix(org.URL, "http://")}))
	require.NoError(t, err)
	assert.False(t, out.Msg.Ok, "signed out: nothing, so Git says to sign in")
}

// A cloned workspace with sync on is kept in step by the service: an edit
// is saved and shared, and a push announced on its stream comes in
// (EN-62, EN-64).
func TestOrgSync(t *testing.T) {
	t.Cleanup(http.DefaultClient.CloseIdleConnections)
	_, s := serve(t, nil)
	secrets.SetDefault(&secrets.Memory{})
	s.ent.retry, s.ent.idle = 50*time.Millisecond, 50*time.Millisecond
	s.ent.syncEvery, s.ent.syncQuiet, s.ent.syncPoll = 50*time.Millisecond, 50*time.Millisecond, 20*time.Millisecond
	idp := enterprisetest.NewProvider(t)
	org := enterprisetest.NewService(t, idp.URL)
	org.AddWorkspace(t, "plans", "Plans", "editor")
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	c := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()

	_, err := c.ConnectEnterprise(ctx, connect.NewRequest(&pb.ConnectEnterpriseRequest{Url: org.URL}))
	require.NoError(t, err)
	stream, err := c.EnterpriseSignIn(ctx, connect.NewRequest(&pb.EnterpriseSignInRequest{Device: true}))
	require.NoError(t, err)
	for stream.Receive() {
	}
	require.NoError(t, stream.Err())
	_ = stream.Close()
	token, err := s.ent.c.AccessToken(ctx)
	require.NoError(t, err)
	t.Setenv("BLITZ_TEST_TOKEN", token)
	s.ent.gitHelper = `!f() { echo username=blitz; echo "password=$BLITZ_TEST_TOKEN"; }; f`

	opened, err := c.OpenOrgWorkspace(ctx, connect.NewRequest(&pb.OpenOrgWorkspaceRequest{Handle: "plans"}))
	require.NoError(t, err)
	dir := opened.Msg.Dir
	state := func() string {
		st, err := c.GetOrgSync(ctx, connect.NewRequest(&pb.GetOrgSyncRequest{Workspace: dir}))
		require.NoError(t, err)
		return st.Msg.State
	}
	require.Eventually(t, func() bool { return state() == "saved" }, 10*time.Second, 20*time.Millisecond, "kept in step once cloned")
	// The service unreachable for several looks at its list: the sync
	// keeps running (it used to stop with the first failed request).
	org.Mu.Lock()
	org.ListDown = true
	org.Mu.Unlock()
	time.Sleep(10 * s.ent.syncEvery)
	assert.NotEmpty(t, state(), "still syncing while the list can't be read")
	org.Mu.Lock()
	org.ListDown = false
	org.Mu.Unlock()
	require.Eventually(t, func() bool { return state() == "saved" }, 10*time.Second, 20*time.Millisecond, "saved again once it answers")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "plan.md"), []byte("Ship it."), 0o644))
	bare := org.Repo("plans")
	lastSave := func() string {
		out, _ := exec.Command("git", "-C", bare, "log", "-1", "--format=%s", "main").Output()
		return strings.TrimSpace(string(out))
	}
	require.Eventually(t, func() bool { return lastSave() == "Ana: edited plan.md" }, 10*time.Second, 20*time.Millisecond, "saved and shared")

	// Bo pushes; the stream says so; it comes in.
	other := t.TempDir()
	run := func(dir string, args ...string) {
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=Bo", "-c", "user.email=bo@acme.com"}, args...)...).CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
	run(other, "clone", "-q", bare, "bo")
	require.NoError(t, os.WriteFile(filepath.Join(other, "bo", "bo.md"), []byte("Bo's"), 0o644))
	run(filepath.Join(other, "bo"), "add", "-A")
	run(filepath.Join(other, "bo"), "commit", "-q", "-m", "Bo: edited bo.md")
	run(filepath.Join(other, "bo"), "push", "-q", "origin", "main")
	org.Pushes <- "plans"
	require.Eventually(t, func() bool { _, err := os.Stat(filepath.Join(dir, "bo.md")); return err == nil }, 10*time.Second, 20*time.Millisecond, "Bo's change came in")

	none, err := c.GetOrgSync(ctx, connect.NewRequest(&pb.GetOrgSyncRequest{Workspace: t.TempDir()}))
	require.NoError(t, err)
	assert.Empty(t, none.Msg.State, "not one kept in step")

	// Bo and Ana change the same lines at once: both versions are kept, and
	// the agent's next turn is told so, once (EN-63).
	run(filepath.Join(other, "bo"), "pull", "-q", "origin", "main")
	require.NoError(t, os.WriteFile(filepath.Join(other, "bo", "plan.md"), []byte("Bo's plan."), 0o644))
	run(filepath.Join(other, "bo"), "commit", "-q", "-am", "Bo: edited plan.md")
	run(filepath.Join(other, "bo"), "push", "-q", "origin", "main")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plan.md"), []byte("Ana's plan."), 0o644))
	require.Eventually(t, func() bool { return state() == "conflicts" }, 10*time.Second, 20*time.Millisecond, "both versions kept")
	note := s.conflictNote(dir)
	assert.Contains(t, note, "- plan.md (the shared version) and plan (conflict, Ana).md (this person's)")
	assert.Empty(t, s.conflictNote(dir), "told once")
	assert.Empty(t, s.conflictNote(t.TempDir()), "not one kept in step")
}

// The organization workspaces' calls when it can't work: not connected,
// settings the service can't use, the organization's service down.
func TestOrgWorkspacesFailures(t *testing.T) {
	t.Cleanup(http.DefaultClient.CloseIdleConnections)
	_, s := serve(t, nil)
	secrets.SetDefault(&secrets.Memory{})
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	c := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()

	for name, call := range map[string]func() error{
		"create": func() error {
			_, err := c.CreateOrgWorkspace(ctx, connect.NewRequest(&pb.CreateOrgWorkspaceRequest{Name: "x"}))
			return err
		},
		"open": func() error {
			_, err := c.OpenOrgWorkspace(ctx, connect.NewRequest(&pb.OpenOrgWorkspaceRequest{Handle: "x"}))
			return err
		},
	} {
		_, info := errorReason(t, call())
		assert.Equal(t, "NOT_CONNECTED", info.Reason, name)
	}
	cred, err := c.GitCredential(ctx, connect.NewRequest(&pb.GitCredentialRequest{Host: "blitz.acme.com"}))
	require.NoError(t, err)
	assert.False(t, cred.Msg.Ok, "not connected: nothing")
	st, err := c.GetOrgSync(ctx, connect.NewRequest(&pb.GetOrgSyncRequest{Workspace: "/no/such/folder"}))
	require.NoError(t, err)
	assert.Empty(t, st.Msg.State)

	home, _ := os.UserHomeDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".blitz"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".blitz", ".env.toml"), []byte("[enterprise\n"), 0o600))
	_, err = c.ListOrgWorkspaces(ctx, connect.NewRequest(&pb.ListOrgWorkspacesRequest{}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "settings it can't read")
	require.NoError(t, os.WriteFile(filepath.Join(home, ".blitz", ".env.toml"), []byte("[enterprise]\nurl = \"http://evil.example\"\n"), 0o600))
	_, err = c.ListOrgWorkspaces(ctx, connect.NewRequest(&pb.ListOrgWorkspacesRequest{}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "an address that isn't a service")
	require.NoError(t, os.Remove(filepath.Join(home, ".blitz", ".env.toml")))

	idp := enterprisetest.NewProvider(t)
	org := enterprisetest.NewService(t, idp.URL)
	_, err = c.ConnectEnterprise(ctx, connect.NewRequest(&pb.ConnectEnterpriseRequest{Url: org.URL}))
	require.NoError(t, err)
	stream, err := c.EnterpriseSignIn(ctx, connect.NewRequest(&pb.EnterpriseSignInRequest{Device: true}))
	require.NoError(t, err)
	for stream.Receive() {
	}
	_ = stream.Close()
	org.Close()
	for name, call := range map[string]func() error{
		"list": func() error {
			_, err := c.ListOrgWorkspaces(ctx, connect.NewRequest(&pb.ListOrgWorkspacesRequest{}))
			return err
		},
		"create": func() error {
			_, err := c.CreateOrgWorkspace(ctx, connect.NewRequest(&pb.CreateOrgWorkspaceRequest{Name: "x"}))
			return err
		},
		"open": func() error {
			_, err := c.OpenOrgWorkspace(ctx, connect.NewRequest(&pb.OpenOrgWorkspaceRequest{Handle: "x"}))
			return err
		},
	} {
		assert.Error(t, call(), name)
	}
	assert.Equal(t, "!\"x\" git-credential", fmt.Sprintf("!%q git-credential", "x"))
	s.ent.gitHelper = ""
	assert.Contains(t, s.gitHelper(), "git-credential", "this program's, by default")
}

// A job in an organization workspace with runs_on: service shows the
// service's runs of it (EN-42): found by the clone's folder and the job's
// name, run now, switched off, and a run's files listed and saved in
// Downloads, a second copy beside the first.
func TestServiceJobs(t *testing.T) {
	t.Cleanup(http.DefaultClient.CloseIdleConnections)
	_, s := serve(t, nil)
	secrets.SetDefault(&secrets.Memory{})
	idp := enterprisetest.NewProvider(t)
	org := enterprisetest.NewService(t, idp.URL)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	c := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()
	home, _ := os.UserHomeDir()
	plans := filepath.Join(home, "Blitz", "Acme Assist", "plans")

	_, err := c.GetServiceJob(ctx, connect.NewRequest(&pb.GetServiceJobRequest{Workspace: plans, Name: "digest"}))
	_, info := errorReason(t, err)
	assert.Equal(t, "NOT_CONNECTED", info.Reason)
	for _, call := range []func() error{
		func() error {
			_, err := c.RunServiceJob(ctx, connect.NewRequest(&pb.RunServiceJobRequest{JobId: "j1"}))
			return err
		},
		func() error {
			_, err := c.SetServiceJobEnabled(ctx, connect.NewRequest(&pb.SetServiceJobEnabledRequest{JobId: "j1"}))
			return err
		},
		func() error {
			_, err := c.ListServiceJobs(ctx, connect.NewRequest(&pb.ListServiceJobsRequest{}))
			return err
		},
		func() error {
			_, err := c.GetOrgUsage(ctx, connect.NewRequest(&pb.GetOrgUsageRequest{}))
			return err
		},
		func() error {
			_, err := c.ListServiceApprovals(ctx, connect.NewRequest(&pb.ListServiceApprovalsRequest{}))
			return err
		},
		func() error {
			_, err := c.AnswerServiceRun(ctx, connect.NewRequest(&pb.AnswerServiceRunRequest{RunId: "r"}))
			return err
		},
		func() error {
			_, err := c.ListServiceRunFiles(ctx, connect.NewRequest(&pb.ListServiceRunFilesRequest{Artifacts: "urn:blitz:artifacts:" + strings.Repeat("9b2f0c4e-1d2a-4b7e-9f00-1c2d3e4f5a6b:", 2) + "9b2f0c4e-1d2a-4b7e-9f00-1c2d3e4f5a6b"}))
			return err
		},
		func() error {
			_, err := c.SaveServiceRunFile(ctx, connect.NewRequest(&pb.SaveServiceRunFileRequest{Urn: "urn:blitz:artifacts:" + strings.Repeat("9b2f0c4e-1d2a-4b7e-9f00-1c2d3e4f5a6b:", 2) + "9b2f0c4e-1d2a-4b7e-9f00-1c2d3e4f5a6b/a.txt"}))
			return err
		},
	} {
		_, info := errorReason(t, call())
		assert.Equal(t, "NOT_CONNECTED", info.Reason)
	}
	notYet, err := c.WatchServiceRuns(ctx, connect.NewRequest(&pb.WatchServiceRunsRequest{}))
	require.NoError(t, err)
	assert.False(t, notYet.Receive())
	_, info = errorReason(t, notYet.Err())
	assert.Equal(t, "NOT_CONNECTED", info.Reason)
	_, err = c.ConnectEnterprise(ctx, connect.NewRequest(&pb.ConnectEnterpriseRequest{Url: org.URL}))
	require.NoError(t, err)
	_, err = c.GetServiceJob(ctx, connect.NewRequest(&pb.GetServiceJobRequest{Workspace: plans, Name: "digest"}))
	_, info = errorReason(t, err)
	assert.Equal(t, "NOT_SIGNED_IN", info.Reason)
	stream, err := c.EnterpriseSignIn(ctx, connect.NewRequest(&pb.EnterpriseSignInRequest{Device: true}))
	require.NoError(t, err)
	for stream.Receive() {
	}
	require.NoError(t, stream.Err())
	_ = stream.Close()

	const (
		ws  = "9b2f0c4e-1d2a-4b7e-9f00-1c2d3e4f5a6b"
		job = "51aa7e10-0b6c-4c2a-8d1e-2f3a4b5c6d7e"
		run = "e8030f9a-7b1c-4d2e-a3f4-5b6c7d8e9f01"
	)
	set := "urn:blitz:artifacts:" + ws + ":" + job + ":" + run
	org.Mu.Lock()
	org.ServiceJobs = []enterprisetest.ServiceJob{{ID: job, URN: "urn:blitz:jobs:" + job, Workspace: "plans", Name: "digest", Schedule: "Daily at 6 AM", Enabled: true},
		{ID: "other", Workspace: "q4", Name: "digest"}}
	org.ServiceRuns = map[string][]enterprisetest.ServiceRun{job: {{ID: run, Status: "succeeded", Trigger: "schedule", Branch: "blitz/jobs/digest/" + run, Artifacts: set, Output: "Done."}}}
	org.Files = map[string][]enterprisetest.File{set: {
		{URN: set + "/events.jsonl", Title: "events.jsonl", Size: 10},
		{URN: set + "/out/sales%20by%20week.csv", Title: "out/sales by week.csv", MediaType: "text/csv", Size: 4},
		{URN: "not a name", Title: "odd"},
	}}
	org.Contents = map[string]string{set + "/out/sales%20by%20week.csv": "w,s\n"}
	org.Mu.Unlock()

	got, err := c.GetServiceJob(ctx, connect.NewRequest(&pb.GetServiceJobRequest{Workspace: plans, Name: "digest"}))
	require.NoError(t, err)
	require.True(t, got.Msg.Found)
	assert.Equal(t, job, got.Msg.Job.Id)
	require.Len(t, got.Msg.Runs, 1)
	assert.Equal(t, "blitz/jobs/digest/"+run, got.Msg.Runs[0].Branch)
	assert.Equal(t, set, got.Msg.Runs[0].Artifacts)

	got, err = c.GetServiceJob(ctx, connect.NewRequest(&pb.GetServiceJobRequest{Workspace: plans, Name: "tidy"}))
	require.NoError(t, err)
	assert.False(t, got.Msg.Found, "not read from main yet")
	_, err = c.GetServiceJob(ctx, connect.NewRequest(&pb.GetServiceJobRequest{Workspace: t.TempDir(), Name: "digest"}))
	_, info = errorReason(t, err)
	assert.Equal(t, "NOT_ORG_WORKSPACE", info.Reason)

	started, err := c.RunServiceJob(ctx, connect.NewRequest(&pb.RunServiceJobRequest{JobId: job}))
	require.NoError(t, err)
	assert.Equal(t, "running", started.Msg.Run.Status)
	_, err = c.RunServiceJob(ctx, connect.NewRequest(&pb.RunServiceJobRequest{JobId: "nope"}))
	assert.Error(t, err)
	_, err = c.SetServiceJobEnabled(ctx, connect.NewRequest(&pb.SetServiceJobEnabledRequest{JobId: job, Enabled: false}))
	require.NoError(t, err)
	_, err = c.SetServiceJobEnabled(ctx, connect.NewRequest(&pb.SetServiceJobEnabledRequest{JobId: "nope"}))
	assert.Error(t, err)

	files, err := c.ListServiceRunFiles(ctx, connect.NewRequest(&pb.ListServiceRunFilesRequest{Artifacts: set}))
	require.NoError(t, err)
	require.Len(t, files.Msg.Files, 2, "never a name that isn't one")
	assert.Equal(t, "out/sales by week.csv", files.Msg.Files[1].Path)
	for range 2 {
		_, err = c.SaveServiceRunFile(ctx, connect.NewRequest(&pb.SaveServiceRunFileRequest{Urn: files.Msg.Files[1].Urn}))
		require.NoError(t, err)
	}
	data, err := os.ReadFile(filepath.Join(home, "Downloads", "sales by week (2).csv"))
	require.NoError(t, err)
	assert.Equal(t, "w,s\n", string(data))
	// Larger than a run's file may be: never saved cut off (EN-42a).
	maxSave = 3
	_, err = c.SaveServiceRunFile(ctx, connect.NewRequest(&pb.SaveServiceRunFileRequest{Urn: files.Msg.Files[1].Urn}))
	maxSave = 100 << 20
	_, info = errorReason(t, err)
	assert.Equal(t, "UPLOAD_TOO_LARGE", info.Reason)
	assert.NoFileExists(t, filepath.Join(home, "Downloads", "sales by week (3).csv"), "nothing left half-saved")
	// Nowhere to save it: Downloads is a file.
	t.Setenv("HOME", t.TempDir())
	newHome, _ := os.UserHomeDir()
	require.NoError(t, os.WriteFile(filepath.Join(newHome, "Downloads"), nil, 0o600))
	_, err = c.SaveServiceRunFile(ctx, connect.NewRequest(&pb.SaveServiceRunFileRequest{Urn: files.Msg.Files[1].Urn}))
	assert.Equal(t, connect.CodeInternal, connect.CodeOf(err))
	t.Setenv("HOME", home)

	for name, call := range map[string]func() error{
		"a file's name for its run's files": func() error {
			_, err := c.ListServiceRunFiles(ctx, connect.NewRequest(&pb.ListServiceRunFilesRequest{Artifacts: files.Msg.Files[1].Urn}))
			return err
		},
		"a run's files' name for a file": func() error {
			_, err := c.SaveServiceRunFile(ctx, connect.NewRequest(&pb.SaveServiceRunFileRequest{Urn: set}))
			return err
		},
		"not a name": func() error {
			_, err := c.SaveServiceRunFile(ctx, connect.NewRequest(&pb.SaveServiceRunFileRequest{Urn: "x"}))
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(call()))
		})
	}
	_, err = c.SaveServiceRunFile(ctx, connect.NewRequest(&pb.SaveServiceRunFileRequest{Urn: set + "/gone.txt"}))
	_, info = errorReason(t, err)
	assert.Equal(t, "NOT_FOUND", info.Reason)
	_, err = c.ListServiceRunFiles(ctx, connect.NewRequest(&pb.ListServiceRunFilesRequest{Artifacts: "urn:blitz:artifacts:" + ws + ":" + job + ":" + ws}))
	assert.Error(t, err)

	// The runs going now, then again as each changes.
	org.Mu.Lock()
	org.Going = []enterprisetest.GoingRun{{ServiceRun: enterprisetest.ServiceRun{ID: run, Status: "running"}, Job: job, JobName: "digest", Workspace: "plans"},
		{ServiceRun: enterprisetest.ServiceRun{ID: "odd"}, Workspace: "../x"}}
	org.Mu.Unlock()
	wctx, stopWatch := context.WithCancel(ctx)
	watch, err := c.WatchServiceRuns(wctx, connect.NewRequest(&pb.WatchServiceRunsRequest{}))
	require.NoError(t, err)
	require.True(t, watch.Receive(), watch.Err())
	require.Len(t, watch.Msg().Runs, 1, "never a handle that isn't one")
	assert.Equal(t, plans, watch.Msg().Runs[0].Dir)
	assert.Equal(t, "digest", watch.Msg().Runs[0].JobName)
	assert.Nil(t, watch.Msg().Change)
	assert.True(t, watch.Msg().Listed)
	org.RunChanges <- enterprisetest.RunChange{Run: "x", Workspace: "../x", Status: "running"}
	org.RunChanges <- enterprisetest.RunChange{Run: run, Job: job, Workspace: "plans", Status: "succeeded"}
	require.True(t, watch.Receive(), watch.Err())
	require.NotNil(t, watch.Msg().Change, "the odd one isn't sent")
	assert.Equal(t, job, watch.Msg().Change.JobId, "the job whose runs to ask for again")
	// The service's stream drops: followed again, with the list as it is.
	defer func(d time.Duration) { watchRunsRetry = d }(watchRunsRetry)
	watchRunsRetry = 20 * time.Millisecond
	org.EndRunStreams <- struct{}{}
	require.True(t, watch.Receive(), watch.Err())
	assert.Nil(t, watch.Msg().Change, "the list again")
	assert.Len(t, watch.Msg().Runs, 1)
	// Then the license stops carrying jobs: the stream says so.
	org.Mu.Lock()
	org.JobsRefused = true
	org.Mu.Unlock()
	org.EndRunStreams <- struct{}{}
	for watch.Receive() {
	}
	_, info = errorReason(t, watch.Err())
	assert.Equal(t, "FEATURE_OFF", info.Reason)
	org.Mu.Lock()
	org.JobsRefused = false
	org.Mu.Unlock()
	stopWatch()
	_ = watch.Close()

	// A paused run waits for the person's answer: listed, answered once.
	org.Mu.Lock()
	org.Waiting = []enterprisetest.GoingRun{{ServiceRun: enterprisetest.ServiceRun{ID: run, Status: "paused",
		Pending: map[string]string{"tool": "run_shell_command", "kind": "shell", "detail": "make deploy"}}, Job: job, JobName: "digest", Workspace: "plans"}}
	org.Mu.Unlock()
	// The person's usage and limits (EN-24).
	org.Mu.Lock()
	org.Usage = map[string]any{
		"today": map[string]any{"claude": map[string]any{"requests": 3, "input_tokens": 1200, "output_tokens": 300, "cost_usd": 0.05},
			"gemini": map[string]any{"requests": 9, "cost_usd": 0.20}},
		"month":       map[string]any{"claude": map[string]any{"requests": 30, "cost_usd": 1.5}},
		"limits":      []any{map[string]any{"scope_kind": "person", "period": "day", "unit": "usd", "amount_usd": 5.0, "used": map[string]any{"cost_usd": 0.25}, "resets": "2026-11-03T00:00:00Z"}},
		"rate_limits": []any{map[string]any{"model": "claude", "person_rpm": 10}},
		"prompt_logs": true,
	}
	org.Mu.Unlock()
	usage, err := c.GetOrgUsage(ctx, connect.NewRequest(&pb.GetOrgUsageRequest{}))
	require.NoError(t, err)
	require.Len(t, usage.Msg.Today, 2)
	assert.Equal(t, "gemini", usage.Msg.Today[0].Model, "the costliest first")
	require.Len(t, usage.Msg.Limits, 1)
	assert.InDelta(t, 0.25, usage.Msg.Limits[0].Used.CostUsd, 1e-9)
	assert.Equal(t, int32(10), usage.Msg.RateLimits[0].PersonRpm)
	assert.True(t, usage.Msg.PromptLogs)

	jobs, err := c.ListServiceJobs(ctx, connect.NewRequest(&pb.ListServiceJobsRequest{}))
	require.NoError(t, err)
	require.Len(t, jobs.Msg.Jobs, 2)
	assert.Equal(t, plans, jobs.Msg.Jobs[0].Dir)
	waiting, err := c.ListServiceApprovals(ctx, connect.NewRequest(&pb.ListServiceApprovalsRequest{}))
	require.NoError(t, err)
	require.Len(t, waiting.Msg.Runs, 1)
	assert.Equal(t, "make deploy", waiting.Msg.Runs[0].Run.Pending.Detail)
	assert.Equal(t, plans, waiting.Msg.Runs[0].Dir)
	answered, err := c.AnswerServiceRun(ctx, connect.NewRequest(&pb.AnswerServiceRunRequest{RunId: run, Approve: true, Note: "Go ahead."}))
	require.NoError(t, err)
	assert.Equal(t, "running", answered.Msg.Run.Status)
	org.Mu.Lock()
	assert.Equal(t, map[string]any{"approve": true, "note": "Go ahead."}, org.Answers[run])
	org.Mu.Unlock()
	_, err = c.AnswerServiceRun(ctx, connect.NewRequest(&pb.AnswerServiceRunRequest{RunId: run, Approve: false}))
	_, info = errorReason(t, err)
	assert.Equal(t, "ANSWERED", info.Reason)

	// A Downloads folder that can't be one.
	require.NoError(t, os.RemoveAll(filepath.Join(home, "Downloads")))
	require.NoError(t, os.WriteFile(filepath.Join(home, "Downloads"), nil, 0o600))
	_, err = c.SaveServiceRunFile(ctx, connect.NewRequest(&pb.SaveServiceRunFileRequest{Urn: files.Msg.Files[1].Urn}))
	_, info = errorReason(t, err)
	assert.Equal(t, "INTERNAL", info.Reason)

	org.Mu.Lock()
	org.JobsRefused = true
	org.Mu.Unlock()
	_, err = c.GetServiceJob(ctx, connect.NewRequest(&pb.GetServiceJobRequest{Workspace: plans, Name: "digest"}))
	_, info = errorReason(t, err)
	assert.Equal(t, "FEATURE_OFF", info.Reason)
	_, err = c.ListServiceApprovals(ctx, connect.NewRequest(&pb.ListServiceApprovalsRequest{}))
	_, info = errorReason(t, err)
	assert.Equal(t, "FEATURE_OFF", info.Reason)
	_, err = c.ListServiceJobs(ctx, connect.NewRequest(&pb.ListServiceJobsRequest{}))
	_, info = errorReason(t, err)
	assert.Equal(t, "FEATURE_OFF", info.Reason)
	_, err = c.AnswerServiceRun(ctx, connect.NewRequest(&pb.AnswerServiceRunRequest{RunId: run}))
	_, info = errorReason(t, err)
	assert.Equal(t, "FEATURE_OFF", info.Reason)
	refused, err := c.WatchServiceRuns(ctx, connect.NewRequest(&pb.WatchServiceRunsRequest{}))
	require.NoError(t, err)
	assert.False(t, refused.Receive())
	_, info = errorReason(t, refused.Err())
	assert.Equal(t, "FEATURE_OFF", info.Reason)
}

// A folder already on this computer shared with the organization where it
// is (EN-66): its work committed, its history checked for secrets, pushed
// to a new workspace (a personal one here), kept in step there through its
// own remote, its origin left alone; never twice, never a secret, never a
// folder inside another repository.
func TestShareWithOrganization(t *testing.T) {
	t.Cleanup(http.DefaultClient.CloseIdleConnections)
	_, s := serve(t, nil)
	secrets.SetDefault(&secrets.Memory{})
	s.ent.retry, s.ent.idle = 50*time.Millisecond, 50*time.Millisecond
	s.ent.syncEvery, s.ent.syncQuiet, s.ent.syncPoll = 50*time.Millisecond, 50*time.Millisecond, 20*time.Millisecond
	idp := enterprisetest.NewProvider(t)
	org := enterprisetest.NewService(t, idp.URL)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	c := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()
	git := func(dir string, args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=Ana", "-c", "user.email=ana@acme.com", "-c", "init.defaultBranch=main"}, args...)...).CombinedOutput()
		require.NoError(t, err, "%s", out)
		return strings.TrimSpace(string(out))
	}
	share := func(dir, name string, personal bool) (*connect.Response[pb.ShareWithOrganizationResponse], error) {
		return c.ShareWithOrganization(ctx, connect.NewRequest(&pb.ShareWithOrganizationRequest{Workspace: dir, Name: name, Personal: personal}))
	}
	notes := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(notes, "plan.md"), []byte("Ship it.\n"), 0o644))

	_, err := share(notes, "Home", true)
	_, info := errorReason(t, err)
	assert.Equal(t, "NOT_CONNECTED", info.Reason)
	_, err = c.ConnectEnterprise(ctx, connect.NewRequest(&pb.ConnectEnterpriseRequest{Url: org.URL}))
	require.NoError(t, err)
	_, err = share(notes, "Home", true)
	_, info = errorReason(t, err)
	assert.Equal(t, "NOT_SIGNED_IN", info.Reason)
	stream, err := c.EnterpriseSignIn(ctx, connect.NewRequest(&pb.EnterpriseSignInRequest{Device: true}))
	require.NoError(t, err)
	for stream.Receive() {
	}
	require.NoError(t, stream.Err())
	_ = stream.Close()
	token, err := s.ent.c.AccessToken(ctx)
	require.NoError(t, err)
	t.Setenv("BLITZ_TEST_TOKEN", token)
	s.ent.gitHelper = `!f() { echo username=blitz; echo "password=$BLITZ_TEST_TOKEN"; }; f`

	t.Run("a key in its history", func(t *testing.T) {
		leaky := t.TempDir()
		git(leaky, "init", "-q")
		require.NoError(t, os.WriteFile(filepath.Join(leaky, "config.py"), []byte("KEY = 'AKIAIOSFODNN7EXAMPLE'\n"), 0o644))
		git(leaky, "add", "-A")
		git(leaky, "commit", "-q", "-m", "first")
		git(leaky, "rm", "-q", "config.py")
		git(leaky, "commit", "-q", "-m", "gone")
		_, err := share(leaky, "Leaky", false)
		code, info := errorReason(t, err)
		assert.Equal(t, connect.CodeFailedPrecondition, code)
		assert.Equal(t, "SECRETS_FOUND", info.Reason)
		assert.Contains(t, err.Error(), "config.py:1")
		org.Mu.Lock()
		_, made := org.Workspaces["leaky"]
		org.Mu.Unlock()
		assert.False(t, made, "nothing made")
	})

	// A key in changes not yet committed: refused before they're
	// committed, so nothing is left in the history (a new folder isn't
	// left a repository either).
	t.Run("a key not yet committed", func(t *testing.T) {
		repo := t.TempDir()
		git(repo, "init", "-q")
		require.NoError(t, os.WriteFile(filepath.Join(repo, "README.md"), []byte("Hello.\n"), 0o644))
		git(repo, "add", "-A")
		git(repo, "commit", "-q", "-m", "first")
		head := git(repo, "rev-parse", "HEAD")
		plain := t.TempDir()
		cases := []struct {
			name, dir, file string
			check           func(t *testing.T)
		}{
			{"an untracked file", repo, ".env", func(t *testing.T) {
				assert.Equal(t, head, git(repo, "rev-parse", "HEAD"), "no commit made")
				assert.Equal(t, "?? .env", git(repo, "status", "--porcelain"))
			}},
			{"a changed file", repo, "README.md", func(t *testing.T) {
				assert.Equal(t, head, git(repo, "rev-parse", "HEAD"), "no commit made")
			}},
			{"a folder that isn't a repository", plain, "config.py", func(t *testing.T) {
				assert.NoDirExists(t, filepath.Join(plain, ".git"), "left as it was")
			}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				require.NoError(t, os.WriteFile(filepath.Join(tc.dir, tc.file), []byte("KEY = 'AKIAIOSFODNN7EXAMPLE'\n"), 0o644))
				defer git(repo, "checkout", "-q", "--", "README.md")
				_, err := share(tc.dir, "Unsaved", false)
				_, info := errorReason(t, err)
				assert.Equal(t, "SECRETS_FOUND", info.Reason)
				assert.Contains(t, err.Error(), tc.file+":1")
				tc.check(t)
				require.NoError(t, os.Remove(filepath.Join(tc.dir, tc.file)))
			})
		}
		org.Mu.Lock()
		_, made := org.Workspaces["unsaved"]
		org.Mu.Unlock()
		assert.False(t, made, "nothing made")
	})

	t.Run("inside another repository", func(t *testing.T) {
		outer := t.TempDir()
		git(outer, "init", "-q")
		inner := filepath.Join(outer, "sub")
		require.NoError(t, os.MkdirAll(inner, 0o755))
		_, err := share(inner, "Sub", false)
		_, info := errorReason(t, err)
		assert.Equal(t, "INSIDE_REPOSITORY", info.Reason)
		_, err = share(filepath.Join(outer, "nope"), "Nope", false)
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})

	shared, err := share(notes, "Home", true)
	require.NoError(t, err)
	assert.Equal(t, "~ana/home", shared.Msg.Workspace.Handle)
	assert.Equal(t, notes, shared.Msg.Workspace.Dir, "where it is")
	assert.True(t, shared.Msg.Workspace.Cloned)
	assert.Contains(t, shared.Msg.Notes[0], "committed first")
	bare := org.Repo("~ana/home")
	assert.Equal(t, "Ship it.", git(bare, "show", "main:plan.md"))
	assert.Equal(t, org.URL+"/git/~ana/home.git", git(notes, "remote", "get-url", "organization"))
	assert.Equal(t, "organization/main", git(notes, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"),
		"main follows the organization's, so Changes can pull and count")
	assert.Equal(t, "\x00"+s.gitHelper()+"\x00", git(notes, "config", "-z", "--get-all", "credential."+org.URL+".helper"),
		"Blitz's helper alone: an empty one first clears the person's own")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull) // this computer's helpers (a keychain's) aren't asked
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	// No name or email guessed from the computer (as a Linux runner's):
	// the saves are the person's.
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "user.useConfigOnly")
	t.Setenv("GIT_CONFIG_VALUE_0", "true")
	w := pb.NewWorkspaceServiceClient(http.DefaultClient, srv.URL)
	pullCtx, cancelPull := context.WithTimeout(ctx, 20*time.Second)
	_, err = w.Pull(pullCtx, connect.NewRequest(&pb.PullRequest{Workspace: notes}))
	cancelPull()
	require.NoError(t, err, "Changes pulls with Blitz's own helper in the folder's config")

	_, err = share(notes, "Home", true)
	_, info = errorReason(t, err)
	assert.Equal(t, "ALREADY_SHARED", info.Reason)
	list, err := c.ListOrgWorkspaces(ctx, connect.NewRequest(&pb.ListOrgWorkspacesRequest{}))
	require.NoError(t, err)
	require.Len(t, list.Msg.Workspaces, 1)
	assert.Equal(t, notes, list.Msg.Workspaces[0].Dir)

	// Kept in step where it is, through its own remote.
	// Each wait ends as soon as it holds; a busy CI runner's commits and
	// pushes take longer than 10 s now and then.
	synced := assert.EventuallyWithT(t, func(ct *assert.CollectT) {
		st, err := c.GetOrgSync(ctx, connect.NewRequest(&pb.GetOrgSyncRequest{Workspace: notes}))
		require.NoError(ct, err)
		if st.Msg.State == "" {
			// None runs for it: say which do, and what the list says, so a
			// failure on CI can be read.
			s.ent.syncs.mu.Lock()
			running := slices.Collect(maps.Keys(s.ent.syncs.running))
			s.ent.syncs.mu.Unlock()
			l, lerr := c.ListOrgWorkspaces(ctx, connect.NewRequest(&pb.ListOrgWorkspacesRequest{}))
			var listed []string
			if lerr == nil {
				for _, w := range l.Msg.Workspaces {
					listed = append(listed, fmt.Sprintf("%s=%s sync=%v", w.Handle, w.Dir, w.Sync))
				}
			}
			_, gerr := os.Stat(filepath.Join(notes, ".git"))
			ct.Errorf("no sync for %s (.git: %v); running %v; listed %v (%v)", notes, gerr, running, listed, lerr)
			return
		}
		assert.Equal(ct, "saved", st.Msg.State, "its sync's state")
	}, 30*time.Second, 20*time.Millisecond)
	if !synced {
		// Where the sync is stuck: its goroutines' stacks.
		buf := make([]byte, 1<<20)
		buf = buf[:runtime.Stack(buf, true)]
		for g := range strings.SplitSeq(string(buf), "\n\n") {
			if strings.Contains(g, "orgsync") || strings.Contains(g, "os/exec") {
				t.Log(g)
			}
		}
		t.FailNow()
	}
	require.NoError(t, os.WriteFile(filepath.Join(notes, "more.md"), []byte("More."), 0o644))
	require.Eventually(t, func() bool {
		out, _ := exec.Command("git", "-C", bare, "log", "-1", "--format=%s", "main").Output()
		return strings.Contains(string(out), "more.md")
	}, 30*time.Second, 20*time.Millisecond, "its change saved and shared")

	// Save now: at once, and the state after.
	require.NoError(t, os.WriteFile(filepath.Join(notes, "now.md"), []byte("Now."), 0o644))
	saved, err := c.SaveOrgSync(ctx, connect.NewRequest(&pb.SaveOrgSyncRequest{Workspace: notes}))
	require.NoError(t, err)
	assert.Equal(t, "saved", saved.Msg.State)
	assert.False(t, saved.Msg.Failed)
	assert.Equal(t, git(notes, "rev-parse", "HEAD"), git(bare, "rev-parse", "main"), "on the service's main")
	_, err = c.SaveOrgSync(ctx, connect.NewRequest(&pb.SaveOrgSyncRequest{Workspace: t.TempDir()}))
	_, info = errorReason(t, err)
	assert.Equal(t, "NOT_SYNCED", info.Reason)

	// A repository with an origin of its own keeps it.
	code := t.TempDir()
	git(code, "init", "-q")
	git(code, "remote", "add", "origin", "https://github.com/acme/code.git")
	require.NoError(t, os.WriteFile(filepath.Join(code, "main.go"), []byte("package main\n"), 0o644))
	git(code, "add", "-A")
	git(code, "commit", "-q", "-m", "first")
	made, err := share(code, "Code", false)
	require.NoError(t, err)
	assert.Equal(t, "code", made.Msg.Workspace.Handle)
	assert.Empty(t, made.Msg.Notes, "nothing to commit first")
	assert.Equal(t, "https://github.com/acme/code.git", git(code, "remote", "get-url", "origin"))

	t.Run("what can go wrong", func(t *testing.T) {
		folder := func(files map[string]string) string {
			dir := t.TempDir()
			for name, data := range files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644))
			}
			return dir
		}
		// Too many files to check, in its changes or its history: nothing
		// shared, nothing committed (EN-66, N26 safety-enterprise#9).
		defer func(n int, size int64) { maxShareScan, largeShareFile = n, size }(maxShareScan, largeShareFile)
		maxShareScan = 1
		big := folder(map[string]string{"data.csv": "a,b,c\n1,2,3\n", "notes.md": "x"})
		git(big, "init", "-q")
		git(big, "remote", "add", "organization", "https://old.example/x.git")
		_, err := share(big, "Big", false)
		code, info := errorReason(t, err)
		assert.Equal(t, connect.CodeFailedPrecondition, code)
		assert.Equal(t, "SECRETS_UNCHECKED", info.Reason)
		assert.Equal(t, "1", info.Metadata["max_files"])
		assert.ErrorContains(t, err, "more than 1 files")
		_, noHead := exec.Command("git", "-C", big, "rev-parse", "--verify", "-q", "HEAD").Output()
		assert.Error(t, noHead, "nothing committed")
		long := folder(map[string]string{"a.md": "a"})
		git(long, "init", "-q")
		git(long, "add", "-A")
		git(long, "commit", "-q", "-m", "first")
		require.NoError(t, os.WriteFile(filepath.Join(long, "b.md"), []byte("b"), 0o644))
		git(long, "add", "-A")
		git(long, "commit", "-q", "-m", "second")
		_, err = share(long, "Long", false)
		_, info = errorReason(t, err)
		assert.Equal(t, "SECRETS_UNCHECKED", info.Reason, "a history past the limit")

		// A large file, and a remote called organization already there.
		maxShareScan, largeShareFile = 50_000, 4
		made, err := share(big, "Big", false)
		require.NoError(t, err)
		assert.Contains(t, strings.Join(made.Msg.Notes, " "), "data.csv is 0 MB")
		assert.Equal(t, org.URL+"/git/big.git", git(big, "remote", "get-url", "organization"))
		maxShareScan, largeShareFile = 50_000, 50<<20

		// A name that's taken: nothing pushed.
		_, err = share(folder(map[string]string{"a.md": "a"}), "Big", false)
		assert.Error(t, err)

		// A push the service refuses: the folder is shared all the same,
		// and sent as it syncs; sharing it again is refused.
		t.Setenv("BLITZ_TEST_TOKEN", "not a token")
		refused := folder(map[string]string{"b.md": "b"})
		_, err = share(refused, "Refused", false)
		_, info = errorReason(t, err)
		assert.Equal(t, "PUSH_FAILED", info.Reason)
		assert.Equal(t, "refused", info.Metadata["handle"])
		assert.Equal(t, refused, sharedFor(org.URL)["refused"].Dir, "recorded before it's sent")
		t.Setenv("BLITZ_TEST_TOKEN", token)
		_, err = share(refused, "Refused", false)
		_, info = errorReason(t, err)
		assert.Equal(t, "ALREADY_SHARED", info.Reason)
		require.Eventually(t, func() bool {
			_, err := c.SaveOrgSync(ctx, connect.NewRequest(&pb.SaveOrgSyncRequest{Workspace: refused}))
			if err != nil {
				return false
			}
			out, _ := exec.Command("git", "-C", org.Repo("refused"), "show", "main:b.md").Output()
			return string(out) == "b"
		}, 10*time.Second, 50*time.Millisecond, "sent by its sync")

		// The record of shared folders can't be read.
		require.NoError(t, os.WriteFile(sharedPath(), []byte("not JSON"), 0o600))
		assert.Nil(t, sharedFor(org.URL))
		assert.Error(t, addShared(org.URL, "x", sharedFolder{Dir: "/x"}))
		_, err = share(folder(map[string]string{"c.md": "c"}), "Unrecorded", false)
		_, info = errorReason(t, err)
		assert.Equal(t, "INTERNAL", info.Reason)
		require.NoError(t, os.Remove(sharedPath()))
		require.NoError(t, addShared(org.URL, "~ana/home", sharedFolder{Dir: notes, Remote: "organization"}))
	})

	// A shared folder's jobs are found by where it is.
	found, err := c.GetServiceJob(ctx, connect.NewRequest(&pb.GetServiceJobRequest{Workspace: notes, Name: "digest"}))
	require.NoError(t, err)
	assert.False(t, found.Msg.Found)

	// Each is known by the service's ID (EN-22); a Viewer's clone is
	// read-only, saying why; an Owner's isn't.
	org.AddWorkspace(t, "board", "Board", "viewer")
	home, _ := os.UserHomeDir()
	board := filepath.Join(home, "Blitz", "Acme Assist", "board")
	require.NoError(t, os.MkdirAll(board, 0o700))
	id, readOnly := s.OrgWorkspaceFor(ctx, board)
	assert.Equal(t, enterprisetest.WorkspaceID("board"), id)
	assert.Contains(t, readOnly, "a Viewer of Acme Assist's workspace Board")
	id, readOnly = s.OrgWorkspaceFor(ctx, notes)
	assert.Equal(t, enterprisetest.WorkspaceID("~ana/home"), id)
	assert.Empty(t, readOnly, "Ana's own")
	for _, dir := range []string{t.TempDir(), filepath.Join(t.TempDir(), "gone")} {
		id, readOnly = s.OrgWorkspaceFor(ctx, dir)
		assert.Empty(t, id+readOnly, "not the organization's")
	}

	// The service gone (EN-37): each is what it last said, a Viewer's
	// clone still read-only.
	org.Close()
	id, readOnly = s.OrgWorkspaceFor(ctx, board)
	assert.Equal(t, enterprisetest.WorkspaceID("board"), id)
	assert.Contains(t, readOnly, "a Viewer of Acme Assist's workspace Board")
	id, readOnly = s.OrgWorkspaceFor(ctx, t.TempDir())
	assert.Empty(t, id+readOnly, "never said: neither")
}

// An Admin publishes a workspace's items to the organization's catalog
// (EN-36): listed with how each applies and what the catalog would
// refuse, sent with their lock and groups set (a skill with its files),
// and the reviews listed; one the catalog would refuse isn't sent.
func TestPublishToOrganization(t *testing.T) {
	t.Cleanup(http.DefaultClient.CloseIdleConnections)
	_, s := serve(t, nil)
	secrets.SetDefault(&secrets.Memory{})
	idp := enterprisetest.NewProvider(t)
	org := enterprisetest.NewService(t, idp.URL)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	c := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()
	ws := t.TempDir()
	for p, data := range map[string]string{
		".agents/agents/writer.md":   "---\nname: writer\ndescription: Writes briefs\n---\nWrite a brief.\n",
		".agents/agents/broken.md":   "no front matter",
		".agents/rules/short.md":     "---\nwhen: after_agent\naction: warn\n---\nOne line.\n",
		".agents/skills/go/SKILL.md": "---\nname: go\ndescription: Go\n---\nUse gofmt.\n",
		".agents/skills/go/lint.sh":  "#!/bin/sh\ngolangci-lint run\n",
		".agents/notes/readme.md":    "not an item",
	} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(ws, p)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(ws, p), []byte(data), 0o644))
	}

	list, err := c.ListPublishable(ctx, connect.NewRequest(&pb.ListPublishableRequest{Workspace: ws}))
	require.NoError(t, err)
	got := map[string]*pb.PublishableItem{}
	for _, it := range list.Msg.Items {
		got[it.Kind+"/"+it.Name] = it
	}
	require.Len(t, got, 4, "agents, a rule and a skill, not its script nor notes")
	assert.Equal(t, "default", got["agent/writer"].Lock)
	assert.NotEmpty(t, got["agent/broken"].Problems)
	assert.Empty(t, got["skill/go"].Problems)

	publish := func(items ...*pb.PublishItem) (*connect.Response[pb.PublishToOrganizationResponse], error) {
		return c.PublishToOrganization(ctx, connect.NewRequest(&pb.PublishToOrganizationRequest{Workspace: ws, Items: items}))
	}
	_, err = publish(&pb.PublishItem{Kind: "agent", Name: "writer", Lock: "locked"})
	_, info := errorReason(t, err)
	assert.Equal(t, "NOT_CONNECTED", info.Reason)
	_, err = c.ConnectEnterprise(ctx, connect.NewRequest(&pb.ConnectEnterpriseRequest{Url: org.URL}))
	require.NoError(t, err)
	stream, err := c.EnterpriseSignIn(ctx, connect.NewRequest(&pb.EnterpriseSignInRequest{Device: true}))
	require.NoError(t, err)
	for stream.Receive() {
	}
	require.NoError(t, stream.Err())
	_ = stream.Close()

	_, err = publish(&pb.PublishItem{Kind: "agent", Name: "broken", Lock: "default"})
	_, info = errorReason(t, err)
	assert.Equal(t, "PROBLEMS", info.Reason)
	_, err = publish(&pb.PublishItem{Kind: "agent", Name: "nope", Lock: "default"})
	_, info = errorReason(t, err)
	assert.Equal(t, "NOT_FOUND", info.Reason)
	_, err = publish(&pb.PublishItem{Kind: "agent", Name: "writer", Lock: "maybe"})
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	_, err = publish()
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	// A key in what would be published, in a skill's script (the catalog's
	// own check reads only the items): nothing goes (EN-36a).
	leaky := filepath.Join(ws, ".agents", "skills", "go", "deploy.sh")
	require.NoError(t, os.WriteFile(leaky, []byte("#!/bin/sh\nKEY='AKIAIOSFODNN7EXAMPLE' make deploy\n"), 0o644))
	_, err = publish(&pb.PublishItem{Kind: "skill", Name: "go"})
	_, info = errorReason(t, err)
	assert.Equal(t, "SECRETS_FOUND", info.Reason)
	assert.Contains(t, err.Error(), ".agents/skills/go/deploy.sh:2")
	require.NoError(t, os.Remove(leaky))

	// The workspace named as every workspace call's is: never "" (blitzd's
	// own folder) or a relative one.
	for _, bad := range []string{"", "relative"} {
		calls := map[string]func() error{
			"ListPublishable": func() error {
				_, err := c.ListPublishable(ctx, connect.NewRequest(&pb.ListPublishableRequest{Workspace: bad}))
				return err
			},
			"PublishToOrganization": func() error {
				_, err := c.PublishToOrganization(ctx, connect.NewRequest(&pb.PublishToOrganizationRequest{Workspace: bad, Items: []*pb.PublishItem{{Kind: "agent", Name: "writer"}}}))
				return err
			},
		}
		for name, call := range calls {
			_, info := errorReason(t, call())
			assert.Equal(t, "INVALID_WORKSPACE", info.Reason, "%s %q", name, bad)
		}
	}

	made, err := publish(&pb.PublishItem{Kind: "agent", Name: "writer", Lock: "locked", Groups: []string{"sales"}}, &pb.PublishItem{Kind: "skill", Name: "go"})
	require.NoError(t, err)
	assert.Equal(t, "open", made.Msg.Review.State)
	assert.Equal(t, []string{"agents/writer.md", "skills/go/SKILL.md", "skills/go/lint.sh"}, made.Msg.Review.Paths)
	assert.Contains(t, made.Msg.Review.Title, "agent writer, skill go")
	org.Mu.Lock()
	assert.Contains(t, org.Published["agents/writer.md"], "lock: locked\ngroups: [sales]")
	assert.Equal(t, "#!/bin/sh\ngolangci-lint run\n", org.Published["skills/go/lint.sh"])
	assert.Contains(t, org.Published["skills/go/SKILL.md"], "lock: default", "none named: a default")
	org.Mu.Unlock()
	data, err := os.ReadFile(filepath.Join(ws, ".agents", "agents", "writer.md"))
	require.NoError(t, err)
	assert.NotContains(t, string(data), "lock:", "the workspace's own file as it was")

	reviews, err := c.ListCatalogReviews(ctx, connect.NewRequest(&pb.ListCatalogReviewsRequest{}))
	require.NoError(t, err)
	require.Len(t, reviews.Msg.Reviews, 1)
	org.Mu.Lock()
	org.NotAdmin = true
	org.Mu.Unlock()
	_, err = c.ListCatalogReviews(ctx, connect.NewRequest(&pb.ListCatalogReviewsRequest{}))
	_, info = errorReason(t, err)
	assert.Equal(t, "FORBIDDEN", info.Reason)
	_, err = publish(&pb.PublishItem{Kind: "rule", Name: "short", Lock: "default"})
	_, info = errorReason(t, err)
	assert.Equal(t, "FORBIDDEN", info.Reason)
}

// The organization's items in a workspace (EN-37): listed with their lock
// and groups and whether the workspace has its own, a skill as one item;
// a default one copied into .agents (a skill's whole folder), a locked one
// and one the workspace has already refused, and nothing before a catalog.
func TestOrgItems(t *testing.T) {
	t.Cleanup(http.DefaultClient.CloseIdleConnections)
	_, s := serve(t, nil)
	secrets.SetDefault(&secrets.Memory{})
	s.ent.retry, s.ent.idle = 50*time.Millisecond, 50*time.Millisecond
	idp := enterprisetest.NewProvider(t)
	org := enterprisetest.NewService(t, idp.URL)
	org.Catalog = map[string]enterprisetest.CatalogFile{
		"agents/reviewer.md":     {Kind: "agent", Name: "reviewer", Lock: "locked", Data: "---\nname: reviewer\n---\nReview."},
		"rules/style.md":         {Kind: "rule", Name: "style", Lock: "default", Data: "Use Go."},
		"skills/deploy/SKILL.md": {Kind: "skill", Name: "deploy", Lock: "default", Data: "---\nname: deploy\n---\nDeploy."},
		"skills/deploy/run.sh":   {Kind: "skill_file", Name: "deploy", Lock: "default", Data: "echo hi"},
	}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	c := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()
	ws := t.TempDir()
	list := func(kind string) *pb.ListOrgItemsResponse {
		res, err := c.ListOrgItems(ctx, connect.NewRequest(&pb.ListOrgItemsRequest{Workspace: ws, Kind: kind}))
		require.NoError(t, err)
		return res.Msg
	}
	copyItem := func(kind, name string) (*connect.Response[pb.CopyOrgItemResponse], error) {
		return c.CopyOrgItem(ctx, connect.NewRequest(&pb.CopyOrgItemRequest{Workspace: ws, Kind: kind, Name: name}))
	}

	_, err := c.ListOrgItems(ctx, connect.NewRequest(&pb.ListOrgItemsRequest{Workspace: ws}))
	_, info := errorReason(t, err)
	assert.Equal(t, "NOT_CONNECTED", info.Reason)
	_, err = copyItem("rule", "style")
	_, info = errorReason(t, err)
	assert.Equal(t, "NOT_CONNECTED", info.Reason)
	_, err = c.ConnectEnterprise(ctx, connect.NewRequest(&pb.ConnectEnterpriseRequest{Url: org.URL}))
	require.NoError(t, err)
	assert.Empty(t, list("").Items, "no catalog yet")
	_, err = copyItem("rule", "style")
	_, info = errorReason(t, err)
	assert.Equal(t, "NOT_FOUND", info.Reason)

	stream, err := c.EnterpriseSignIn(ctx, connect.NewRequest(&pb.EnterpriseSignInRequest{Device: true}))
	require.NoError(t, err)
	for stream.Receive() {
	}
	require.NoError(t, stream.Err())
	_ = stream.Close()
	require.Eventually(t, func() bool { return len(list("").Items) == 3 }, 5*time.Second, 20*time.Millisecond)

	all := list("")
	assert.Equal(t, int32(4), all.Version)
	assert.Equal(t, []string{"agents/reviewer.md", "rules/style.md", "skills/deploy/SKILL.md"}, []string{all.Items[0].Path, all.Items[1].Path, all.Items[2].Path})
	assert.Equal(t, "locked", all.Items[0].Lock)
	rules := list("rule")
	require.Len(t, rules.Items, 1)
	assert.False(t, rules.Items[0].Overridden)

	_, err = copyItem("agent", "reviewer")
	_, info = errorReason(t, err)
	assert.Equal(t, "LOCKED", info.Reason)
	_, err = copyItem("agent", "nope")
	_, info = errorReason(t, err)
	assert.Equal(t, "NOT_FOUND", info.Reason)

	got, err := copyItem("skill", "deploy")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{".agents/skills/deploy/SKILL.md", ".agents/skills/deploy/run.sh"}, got.Msg.Paths)
	data, err := os.ReadFile(filepath.Join(ws, ".agents", "skills", "deploy", "run.sh"))
	require.NoError(t, err)
	assert.Equal(t, "echo hi", string(data))
	_, err = copyItem("rule", "style")
	require.NoError(t, err)
	assert.True(t, list("rule").Items[0].Overridden, "the workspace has its own now")
	_, err = copyItem("rule", "style")
	_, info = errorReason(t, err)
	assert.Equal(t, "ALREADY_EXISTS", info.Reason)
	for _, bad := range []string{"", "relative"} {
		_, err = c.CopyOrgItem(ctx, connect.NewRequest(&pb.CopyOrgItemRequest{Workspace: bad, Kind: "rule", Name: "style"}))
		_, info = errorReason(t, err)
		assert.Equal(t, "INVALID_WORKSPACE", info.Reason, "copied into %q", bad)
		_, err = c.ListOrgItems(ctx, connect.NewRequest(&pb.ListOrgItemsRequest{Workspace: bad}))
		_, info = errorReason(t, err)
		assert.Equal(t, "INVALID_WORKSPACE", info.Reason, "listed for %q", bad)
	}

	// How the person hears of their jobs' runs (JB-31).
	notices, err := c.GetOrgNotices(ctx, connect.NewRequest(&pb.GetOrgNoticesRequest{}))
	require.NoError(t, err)
	assert.True(t, notices.Msg.Notices.Email)
	set, err := c.SetOrgNotices(ctx, connect.NewRequest(&pb.SetOrgNoticesRequest{Notices: &pb.OrgNotices{Webhook: "https://hooks.example/x", EveryRun: true}}))
	require.NoError(t, err)
	assert.Equal(t, "https://hooks.example/x", set.Msg.Notices.Webhook)
	assert.False(t, set.Msg.Notices.Email)
	_, err = c.SetOrgNotices(ctx, connect.NewRequest(&pb.SetOrgNoticesRequest{Notices: &pb.OrgNotices{Webhook: "http://x"}}))
	assert.Error(t, err)
}

// Device management's organization (EN-10): connected from the start,
// said to be managed, and neither changed nor disconnected here.
func TestManagedOrganization(t *testing.T) {
	t.Cleanup(http.DefaultClient.CloseIdleConnections)
	_, s := serve(t, nil)
	secrets.SetDefault(&secrets.Memory{})
	idp := enterprisetest.NewProvider(t)
	org := enterprisetest.NewService(t, idp.URL)
	managed := filepath.Join(t.TempDir(), "managed.toml")
	require.NoError(t, os.WriteFile(managed, []byte("[enterprise]\nurl = \""+org.URL+"\"\n"), 0o644))
	t.Setenv(config.ManagedEnv, managed)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	c := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()

	got, err := c.GetEnterprise(ctx, connect.NewRequest(&pb.GetEnterpriseRequest{}))
	require.NoError(t, err)
	assert.Equal(t, org.URL, got.Msg.State.Url)
	assert.True(t, got.Msg.State.Managed)
	for _, u := range []string{"", "https://other.example"} {
		_, err = c.ConnectEnterprise(ctx, connect.NewRequest(&pb.ConnectEnterpriseRequest{Url: u}))
		_, info := errorReason(t, err)
		assert.Equal(t, "MANAGED", info.Reason, u)
	}
}
