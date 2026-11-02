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

	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
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
	t.Cleanup(func() { s.Close() }) // its catalog's loop with it
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

	org.Version = "v1"
	terms, err := c.Terms(ctx)
	require.NoError(t, err)
	assert.Equal(t, OrganizationTerms{Version: "v1", Text: "Be kind."}, terms)
	_, err = c.AcceptTerms(ctx, "v0")
	assert.Error(t, err, "not the version in force")
	o, err = c.AcceptTerms(ctx, "v1")
	require.NoError(t, err)
	assert.Empty(t, o.Terms)

	o, err = c.SignOutOrganization(ctx)
	require.NoError(t, err)
	assert.False(t, o.SignedIn)
	o, err = c.Connect(ctx, "")
	require.NoError(t, err)
	assert.Empty(t, o.URL)
}

// A service that's gone: each call says so.
func TestOrganizationUnreachable(t *testing.T) {
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()
	c := AttachSettingsHTTP(http.DefaultClient, gone.URL)
	ctx := context.Background()
	_, err := c.Organization(ctx, false)
	assert.Error(t, err)
	_, err = c.Connect(ctx, "https://b.acme.com")
	assert.Error(t, err)
	_, err = c.SignInOrganization(ctx, false, func(OrganizationPrompt) {})
	assert.Error(t, err)
	_, err = c.SignOutOrganization(ctx)
	assert.Error(t, err)
	assert.Equal(t, Organization{}, organization(nil))
	_, err = c.Terms(ctx)
	assert.Error(t, err)
	_, err = c.AcceptTerms(ctx, "v1")
	assert.Error(t, err)
	_, _, err = c.OrgWorkspaces(ctx)
	assert.Error(t, err)
	_, err = c.CreateOrgWorkspace(ctx, "x", "", false)
	assert.Error(t, err)
	_, _, err = c.ShareWithOrganization(ctx, "/x", "x", "", false)
	assert.Error(t, err)
	_, _, err = c.OpenOrgWorkspace(ctx, "x")
	assert.Error(t, err)
	_, _, _, err = c.GitCredential(ctx, "x")
	assert.Error(t, err)

	// The organization's jobs, usage and catalog (EN-24, EN-34, EN-36, EN-42, EN-43).
	for name, call := range map[string]func() error{
		"ServiceJobs":           func() error { _, err := c.ServiceJobs(ctx); return err },
		"ServiceJobRuns":        func() error { _, err := c.ServiceJobRuns(ctx, "/x", "digest"); return err },
		"RunServiceJob":         func() error { _, err := c.RunServiceJob(ctx, "j"); return err },
		"SetServiceJobEnabled":  func() error { return c.SetServiceJobEnabled(ctx, "j", true) },
		"ServiceRunFiles":       func() error { _, err := c.ServiceRunFiles(ctx, "urn:blitz:artifacts:a:b:c"); return err },
		"SaveServiceRunFile":    func() error { _, err := c.SaveServiceRunFile(ctx, "urn:blitz:artifacts:a:b:c/f"); return err },
		"ServiceApprovals":      func() error { _, err := c.ServiceApprovals(ctx); return err },
		"AnswerServiceRun":      func() error { _, err := c.AnswerServiceRun(ctx, "r", true, ""); return err },
		"OrgUsage":              func() error { _, err := c.OrgUsage(ctx); return err },
		"Publishable":           func() error { _, err := c.Publishable(ctx, "/x"); return err },
		"PublishToOrganization": func() error { _, err := c.PublishToOrganization(ctx, "/x", "", nil); return err },
		"CatalogReviews":        func() error { _, err := c.CatalogReviews(ctx, ""); return err },
		"OrgItems":              func() error { _, err := c.OrgItems(ctx, "/x", ""); return err },
		"CopyOrgItem":           func() error { _, err := c.CopyOrgItem(ctx, "/x", "rule", "style"); return err },
		"ListModels":            func() error { _, err := c.ListModels(ctx, "/x"); return err },
		"OrgNotices":            func() error { _, err := c.OrgNotices(ctx); return err },
		"SetOrgNotices":         func() error { _, err := c.SetOrgNotices(ctx, &pb.OrgNotices{}); return err },
	} {
		assert.Error(t, call(), name)
	}
}
