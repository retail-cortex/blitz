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

package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mcpServer is an MCP server over HTTP with a resource and a prompt
// taking a required name and an optional tone.
func mcpServer(t *testing.T) string {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	srv.AddResource(&mcp.Resource{URI: "file:///readme", Name: "readme", MIMEType: "text/plain"}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: "file:///readme", Text: "hello from the server"}}}, nil
	})
	srv.AddPrompt(&mcp.Prompt{Name: "greet", Description: "Greet someone", Arguments: []*mcp.PromptArgument{{Name: "name", Required: true}, {Name: "tone"}}},
		func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			text := "Say hello to " + req.Params.Arguments["name"]
			if tone := req.Params.Arguments["tone"]; tone != "" {
				text += ", " + tone
			}
			return &mcp.GetPromptResult{Messages: []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: text}}}}, nil
		})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	t.Cleanup(func() {
		for ss := range srv.Sessions() {
			ss.Close()
		}
	})
	return ts.URL
}

// openMCP opens a workspace with the test MCP server as "srv" and a
// stdio server that is listed but never started.
func openMCP(t *testing.T) *Workspace {
	t.Helper()
	url := mcpServer(t)
	w, _ := openTestWith(t, func(c *config.Config) {
		c.MCP.Servers = []config.MCPServerConfig{
			{Name: "srv", URL: url, AutoApprove: true, Agents: []string{"*"}, Prefix: "srv_"},
			{Name: "other", Command: "some-server", Args: []string{"--stdio"}, Agents: []string{"reviewer"}},
		}
	})
	return w
}

// MCP servers are listed with their targets, offered to the agents they
// name, and their prompts are commands.
func TestMCPServersAndPrompts(t *testing.T) {
	w := openMCP(t)
	servers := w.ListMCPServers()
	require.Len(t, servers, 2)
	assert.Equal(t, api.MCPServer{Name: "other", Target: "some-server --stdio"}, servers[1])
	assert.True(t, servers[0].AutoApprove)

	offers := w.ActiveAgentTools().MCP
	assert.Equal(t, []api.MCPOffer{{Server: "srv", Prefix: "srv_"}}, offers, "only srv is offered to the primary agent")

	cmds := w.ListCommands()
	i := slices.IndexFunc(cmds, func(c api.CommandInfo) bool { return c.Name == "mcp__srv__greet" })
	require.GreaterOrEqual(t, i, 0, "the prompt as a command")
	assert.Equal(t, "name= tone=", cmds[i].ArgumentHint)
	assert.Equal(t, "mcp:srv", cmds[i].Source)

	ctx := context.Background()
	for _, tc := range []struct{ name, args, want, err string }{
		{name: "in order", args: "Ada warmly", want: "Say hello to Ada, warmly"},
		{name: "by name", args: "tone=briefly name=Bob", want: "Say hello to Bob, briefly"},
		{name: "mixed", args: "name=Cy kindly", want: "Say hello to Cy, kindly"},
		{name: "too many", args: "a b c", err: "takes 2 arguments"},
		{name: "missing required", args: "tone=x", err: "needs name="},
	} {
		t.Run(tc.name, func(t *testing.T) {
			turn := &api.Turn{Text: "/mcp__srv__greet " + tc.args}
			opts, err := w.expandCommand(ctx, turn)
			if tc.err != "" {
				assert.ErrorContains(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			assert.Nil(t, opts)
			assert.Contains(t, turn.Prompt, tc.want)
		})
	}
	_, err := w.expandCommand(ctx, &api.Turn{Text: "/mcp__srv__nope"})
	assert.ErrorIs(t, err, api.ErrUnknownCommand)
}

// @server:uri adds an MCP resource to the prompt; one that can't be read
// says so.
func TestMCPResourceMentions(t *testing.T) {
	w := openMCP(t)
	got := w.withMentions(context.Background(), "read @srv:file:///readme", "read @srv:file:///readme")
	assert.Contains(t, got, `<resource name="srv:file:///readme">`+"\nhello from the server\n</resource>")
	got = w.withMentions(context.Background(), "read @srv:file:///missing", "read @srv:file:///missing")
	assert.Contains(t, got, "(couldn't be read:")
}
