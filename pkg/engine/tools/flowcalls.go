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
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Workflows' calls (spec_workflows_043 WF-14): an API or A2A step's
// requests go through web_fetch's guards (public addresses only unless
// web.allow_private, web.deny_domains, the permission rules, approval
// per host unless web.allow_domains), and an MCP step's call is approved
// as the agent's MCP calls are.

// ErrWebOff means workflows can't reach the network: [web] is off, or
// the sandbox forbids the network.
var ErrWebOff = errors.New("the web is off for this workspace ([web] enabled = false, or sandbox.allow_network = false)")

// NetworkClient is an HTTP client with web_fetch's guards: it dials only
// public addresses (unless web.allow_private), follows redirects only on
// the same host or to web.allow_domains, and stops at web.timeout_seconds.
// Its transport is wrapped by wrap, if given (headers, auth).
func (r *Registry) NetworkClient(wrap func(http.RoundTripper) http.RoundTripper) (*http.Client, error) {
	if !r.webOn || !r.web.AllowNetwork {
		return nil, ErrWebOff
	}
	f := newWebFetcher(r.web)
	c := *f.client
	if wrap != nil {
		c.Transport = wrap(c.Transport)
	}
	return &c, nil
}

// ApproveNetwork checks a workflow's request to u (http or https, no
// credentials in it, not a denied domain) and has it approved as a fetch
// of that host is: denied by a rule, asked by one, allowed by
// web.allow_domains, otherwise asked (tool names who asks; detail says
// what it does, "POST https://…").
func (r *Registry) ApproveNetwork(ctx context.Context, tool, detail string, u *url.URL) error {
	if !r.webOn || !r.web.AllowNetwork {
		return ErrWebOff
	}
	f := &webFetcher{cfg: r.web}
	if err := f.checkURL(u); err != nil {
		return err
	}
	return approveWeb(ctx, r.hooks, r.web, tool, detail, u)
}

// CallMCP calls tool on the MCP server named server with args, once it's
// approved as the agent's MCP calls are (the server's auto_approve
// skips the question; deny and ask rules hold).
func (r *Registry) CallMCP(ctx context.Context, server, tool string, args map[string]any) (*mcp.CallToolResult, error) {
	var s *mcpServer
	for _, x := range r.mcp.list() {
		if x.cfg.Name == server {
			s = x
		}
	}
	if s == nil {
		return nil, fmt.Errorf("there's no MCP server %q (servers: %s)", server, strings.Join(r.mcp.Servers(), ", "))
	}
	req := mcpApproval(server, tool, args)
	effect, _ := r.hooks.Rules().Decide(RuleMCP, req.Targets)
	if !s.cfg.AutoApprove || effect == EffectDeny || effect == EffectAsk {
		if err := r.hooks.Approve(ctx, req); err != nil {
			return nil, err
		}
	}
	cs, err := r.mcp.sessionOf(ctx, s)
	if err != nil {
		return nil, err
	}
	return cs.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
}
