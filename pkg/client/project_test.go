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

	"github.com/retail-cortex/blitz/apps/service/servicetest"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Trusting a workspace's project settings through the service records the
// decision and reopens the workspace with them in force.
func TestRemoteProjectTrust(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	s := servicetest.New(func(ctx context.Context, dir string) (*engine.Workspace, error) {
		cfg, err := config.LoadWorkspace("", dir) // as the service does
		if err != nil {
			return nil, err
		}
		cfg.Tools.WorkspaceDir = dir
		cfg.Session.StorageDir = t.TempDir()
		return engine.Open(ctx, cfg, engine.Options{Model: runtime.NewMockLLM("gemini-3.8-flash")})
	})
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(func() { srv.Close(); s.Close() })

	ws := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(ws, ".blitz"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ws, ".blitz", "settings.toml"), []byte("[permissions]\nallow = [\"shell(make test)\"]\n"), 0o644))
	r, err := AttachHTTP(context.Background(), http.DefaultClient, srv.URL, ws, nil)
	require.NoError(t, err)

	p := r.ProjectSettings()
	assert.Equal(t, api.TrustNew, p.State)
	assert.False(t, p.Loaded)
	require.Len(t, p.Pending, 1)
	assert.ErrorIs(t, r.TrustProject("sha256:not-what-was-shown", true), api.ErrProjectChanged)

	require.NoError(t, r.TrustProject(p.Hash, true))
	p = r.ProjectSettings()
	assert.Equal(t, api.TrustTrusted, p.State)
	assert.True(t, p.Loaded, "the workspace wasn't reopened with them")

	require.NoError(t, r.ForgetProjectTrust())
	p = r.ProjectSettings()
	assert.Equal(t, api.TrustNew, p.State, "forgotten: waiting for trust again")
	assert.False(t, p.Loaded, "reopened without them")
}
