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

package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/apps/service/servicetest"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/socket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The settings a front end reads without a workspace: a permission rule
// checked against a sample, and the providers' models (one without a key
// says why it can't list them).
func TestSettings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	s := servicetest.New(func(context.Context, string) (*engine.Workspace, error) { return nil, nil })
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	c := AttachSettingsHTTP(http.DefaultClient, srv.URL)
	ctx := context.Background()

	ok, err := c.CheckPermission(ctx, "allow", "shell(git log)", "git log --oneline")
	require.NoError(t, err)
	assert.Empty(t, ok.Error)
	assert.Equal(t, "shell", ok.Kind)
	assert.True(t, ok.Tested)
	assert.True(t, ok.Matches)

	bad, err := c.CheckPermission(ctx, "allow", "shell(", "")
	require.NoError(t, err)
	assert.NotEmpty(t, bad.Error)

	none, err := c.ListModels(ctx, "")
	require.NoError(t, err)
	assert.Empty(t, none, "nothing configured")
	gemini, err := c.ListModels(ctx, "", "gemini")
	require.NoError(t, err)
	require.Len(t, gemini, 1)
	assert.Equal(t, "gemini", gemini[0].Provider)
	assert.NotEmpty(t, gemini[0].Err, "no key")
}

// Release closes a workspace the service holds, so another process can
// open it; one that isn't open is nothing to do.
func TestRelease(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir, err := os.MkdirTemp("/tmp", "cr") // socket paths must be short
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { servicetest.Run(ctx, sock); close(done) }()
	t.Cleanup(func() { cancel(); servicetest.Stopped(t, done, 20*time.Second) })
	for deadline := time.Now().Add(10 * time.Second); !socket.Running(sock); time.Sleep(20 * time.Millisecond) {
		require.False(t, time.Now().After(deadline), "service didn't start")
	}
	ws := t.TempDir()
	assert.NoError(t, Release(ctx, sock, ws), "not open")
	_, err = Attach(ctx, sock, ws, nil)
	require.NoError(t, err)
	assert.NoError(t, Release(ctx, sock, ws), "open, idle")
}

// A service that isn't there is the call's error.
func TestSettingsUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	c := AttachSettingsHTTP(http.DefaultClient, srv.URL)
	_, err := c.ListModels(context.Background(), "")
	assert.Error(t, err)
	_, err = c.CheckPermission(context.Background(), "allow", "shell(ls)", "")
	assert.Error(t, err)
}

// Signing in through the service: the vendor's tool runs there, its lines
// stream back with the URL, and the status follows; signing out undoes
// it. A tool that isn't installed, or a provider Blitz doesn't sign in
// to, is the call's error.
func TestSignInThroughTheService(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "no-shell")) // only PATH: not the developer's own tools
	adc := filepath.Join(t.TempDir(), "adc.json")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", adc)
	bin := t.TempDir()
	gcloud := `#!/bin/sh
case "$*" in
  *login*) echo "Go to https://accounts.google.com/auth?x=1"; echo '{"type":"authorized_user"}' > "$GOOGLE_APPLICATION_CREDENTIALS" ;;
  *revoke*) rm -f "$GOOGLE_APPLICATION_CREDENTIALS" ;;
esac
`
	require.NoError(t, os.WriteFile(filepath.Join(bin, "gcloud"), []byte(gcloud), 0o755))
	t.Setenv("PATH", bin+":/usr/bin:/bin")

	s := servicetest.New(func(context.Context, string) (*engine.Workspace, error) { return nil, nil })
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	c := AttachSettingsHTTP(http.DefaultClient, srv.URL)
	ctx := context.Background()

	all, err := c.SignIns(ctx, "", "", "")
	require.NoError(t, err)
	require.Len(t, all, 4, "google, anthropic, aws, azure")
	google := all[0]
	assert.Equal(t, "google", google.Provider)
	assert.True(t, google.ToolFound)
	assert.False(t, google.SignedIn)

	var events []SignInEvent
	st, err := c.SignIn(ctx, "", "google", "", func(e SignInEvent) { events = append(events, e) })
	require.NoError(t, err)
	assert.True(t, st.SignedIn)
	require.NotEmpty(t, events)
	assert.Equal(t, "https://accounts.google.com/auth?x=1", events[0].URL)

	st, err = c.SignOut(ctx, "", "google", "")
	require.NoError(t, err)
	assert.False(t, st.SignedIn)

	_, err = c.SignIn(ctx, "", "azure", "", func(SignInEvent) {})
	assert.ErrorContains(t, err, "az isn't installed")
	_, err = c.SignIn(ctx, "", "openai", "", func(SignInEvent) {})
	assert.Error(t, err)
	_, err = c.SignIns(ctx, "", "openai", "")
	assert.Error(t, err)
	_, err = c.SignOut(ctx, "", "aws", "bad profile!")
	assert.Error(t, err)
}
