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
	"github.com/retail-cortex/blitz/pkg/config"
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
	assert.Equal(t, []string{"gateway", "jobs"}, st.Capabilities)
	assert.True(t, st.Reachable)
	assert.False(t, st.SignedIn)
	cfg, err := config.Load("")
	require.NoError(t, err)
	assert.Equal(t, org.URL, cfg.Enterprise.URL, "kept in the global settings")

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
