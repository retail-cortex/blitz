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
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/signin"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
)

// A sign-in's errors keep their reasons.
func TestSignInError(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{signin.ErrUnknownProvider, "UNKNOWN_PROVIDER"},
		{fmt.Errorf("x: %w", signin.ErrBadProfile), "INVALID_PROFILE"},
		{signin.ErrNoTool, "NO_TOOL"},
		{signin.ErrRunning, "SIGN_IN_RUNNING"},
		{errors.New("exit status 1"), "SIGN_IN_FAILED"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, errorInfo(signInError(tt.err)).GetReason())
		})
	}
	var ce *connect.Error
	require.ErrorAs(t, signInError(context.Canceled), &ce)
	assert.Equal(t, connect.CodeCanceled, ce.Code())
}

// After a sign-in, a workspace whose model couldn't be built is built
// again; one whose model works is left alone. A workspace that isn't a
// folder is refused.
func TestCredentialsChanged(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var signedIn atomic.Bool
	s := New(func(ctx context.Context, dir string) (*engine.Workspace, error) {
		cfg := config.DefaultConfig()
		cfg.Tools.WorkspaceDir = dir
		cfg.Session.StorageDir = t.TempDir()
		return engine.Open(ctx, cfg, engine.Options{NewModel: func(_ context.Context, _ *config.Config, ref string) (model.LLM, error) {
			if !signedIn.Load() {
				return nil, errors.New("no credentials")
			}
			_, name := runtime.ParseModelRef(ref, "")
			return runtime.NewMockLLM(name), nil
		}})
	})
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	w, err := s.workspace(ctx, t.TempDir())
	require.NoError(t, err)
	require.Error(t, w.ModelErr())

	signedIn.Store(true)
	s.credentialsChanged(ctx)
	assert.NoError(t, w.ModelErr(), "built again with the new credentials")

	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	cc := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	_, err = cc.GetSignIn(ctx, connect.NewRequest(&pb.GetSignInRequest{Workspace: "relative"}))
	assert.Error(t, err)
	_, err = cc.SignOut(ctx, connect.NewRequest(&pb.SignOutRequest{Workspace: "relative", Provider: "google"}))
	assert.Error(t, err)
	stream, err := cc.SignIn(ctx, connect.NewRequest(&pb.SignInRequest{Workspace: "relative", Provider: "google"}))
	require.NoError(t, err)
	for stream.Receive() {
	}
	assert.Error(t, stream.Err())
}
