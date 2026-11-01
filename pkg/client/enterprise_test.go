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
	"testing"

	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/enterprise/enterprisetest"
	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/retail-cortex/blitz/pkg/service/servicetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A front end connects, signs in with a code, checks and signs out.
func TestOrganization(t *testing.T) {
	t.Cleanup(http.DefaultClient.CloseIdleConnections)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	secrets.SetDefault(&secrets.Memory{})
	idp := enterprisetest.NewProvider(t)
	org := enterprisetest.NewService(t, idp.URL)
	s := servicetest.New(func(context.Context, string) (*engine.Workspace, error) { return nil, nil })
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	c := AttachSettingsHTTP(http.DefaultClient, srv.URL)
	ctx := context.Background()

	o, err := c.Organization(ctx, false)
	require.NoError(t, err)
	assert.Empty(t, o.URL)
	_, err = c.SignInOrganization(ctx, true, func(OrganizationPrompt) {})
	assert.ErrorContains(t, err, "connect to your organization's service first")
	_, err = c.Connect(ctx, "http://blitz.acme.com")
	assert.Error(t, err)

	o, err = c.Connect(ctx, org.URL)
	require.NoError(t, err)
	assert.Equal(t, "Acme Assist", o.Name)
	var prompts []OrganizationPrompt
	o, err = c.SignInOrganization(ctx, true, func(p OrganizationPrompt) { prompts = append(prompts, p) })
	require.NoError(t, err)
	assert.Equal(t, []OrganizationPrompt{{URL: "https://www.google.com/device", Code: "ABCD-EFGH"}}, prompts)
	assert.True(t, o.SignedIn)
	assert.Equal(t, "ana@acme.com", o.Email)
	assert.False(t, o.Renews.IsZero())

	o, err = c.Organization(ctx, true)
	require.NoError(t, err)
	assert.True(t, o.Checked && o.Reachable)

	o, err = c.SignOutOrganization(ctx)
	require.NoError(t, err)
	assert.False(t, o.SignedIn)
	o, err = c.Connect(ctx, "")
	require.NoError(t, err)
	assert.Empty(t, o.URL)
}
