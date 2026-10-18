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

package tools

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Workflows' calls need the web on; their requests are checked as
// web_fetch's are; an unknown MCP server is said by name.
func TestFlowCallGuards(t *testing.T) {
	cfg := testRegistryConfig(t)
	cfg.Web.Enabled = false
	r, err := NewRegistry(cfg, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { r.Close() })
	_, err = r.NetworkClient(nil)
	assert.ErrorIs(t, err, ErrWebOff)
	assert.ErrorIs(t, r.ApproveNetwork(context.Background(), "api_call", "GET x", &url.URL{Scheme: "https", Host: "example.com"}), ErrWebOff)

	cfg = testRegistryConfig(t)
	cfg.Web.Enabled, cfg.Sandbox.AllowNetwork = true, true
	cfg.Web.DenyDomains = []string{"*.evil.test"}
	cfg.Web.AllowDomains = []string{"api.example.com"}
	r, err = NewRegistry(cfg, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { r.Close() })
	ctx := context.Background()
	assert.ErrorContains(t, r.ApproveNetwork(ctx, "api_call", "GET", &url.URL{Scheme: "https", Host: "x.evil.test"}), "deny_domains")
	assert.ErrorContains(t, r.ApproveNetwork(ctx, "api_call", "GET", &url.URL{Scheme: "ftp", Host: "api.example.com"}), "only http and https")
	assert.ErrorContains(t, r.ApproveNetwork(ctx, "api_call", "GET", &url.URL{Scheme: "https", Host: "api.example.com", User: url.User("me")}), "credentials")
	assert.NoError(t, r.ApproveNetwork(ctx, "api_call", "GET", &url.URL{Scheme: "https", Host: "api.example.com"}), "web.allow_domains")
	wrapped := false
	c, err := r.NetworkClient(func(rt http.RoundTripper) http.RoundTripper { wrapped = true; return rt })
	require.NoError(t, err)
	assert.True(t, wrapped)
	assert.NotNil(t, c.CheckRedirect, "redirects are checked")

	_, err = r.CallMCP(ctx, "nope", "tool", nil)
	assert.ErrorContains(t, err, `no MCP server "nope"`)
}
