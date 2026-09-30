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
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mcpHTTPServer is an MCP server over HTTP with a resource, a prompt and
// a tool; it counts requests carrying the X-Team header.
func mcpHTTPServer(t *testing.T) (url string, withHeader *atomic.Int32) {
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
	withHeader = &atomic.Int32{}
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Team") == "core" {
			withHeader.Add(1)
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	t.Cleanup(func() { // the server's side of each connection
		for ss := range srv.Sessions() {
			ss.Close()
		}
	})
	return ts.URL, withHeader
}

func TestMCPResourcesAndPrompts(t *testing.T) {
	url, withHeader := mcpHTTPServer(t)
	m, err := NewMCPManager([]config.MCPServerConfig{{Name: "srv", URL: url, Headers: map[string]string{"X-Team": "core"}}, {Name: "off", Command: "nope", Disabled: true}}, nil, nil)
	require.NoError(t, err)
	t.Cleanup(m.Close)
	ctx := context.Background()

	res, err := m.Resources(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, []MCPResource{{Server: "srv", URI: "file:///readme", Name: "readme", MIMEType: "text/plain"}}, res)
	text, err := m.ReadResource(ctx, "srv", "file:///readme")
	require.NoError(t, err)
	assert.Equal(t, "hello from the server", text)
	_, err = m.ReadResource(ctx, "nobody", "x")
	assert.Error(t, err)

	mention, ok, err := m.ResourceMention(ctx, "srv:file:///readme")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "hello from the server", mention)
	_, ok, _ = m.ResourceMention(ctx, "notes/today.md")
	assert.False(t, ok, "a path taken for a resource")

	prompts := m.Prompts(ctx, 5*time.Second)
	require.Len(t, prompts, 1)
	assert.Equal(t, "mcp__srv__greet", prompts[0].Command())
	assert.Equal(t, []string{"name", "tone"}, prompts[0].Arguments)
	assert.Equal(t, []string{"name"}, prompts[0].Required)
	got, err := m.GetPrompt(ctx, "srv", "greet", map[string]string{"name": "Ada", "tone": "warmly"})
	require.NoError(t, err)
	assert.Equal(t, "Say hello to Ada, warmly", got)

	assert.Positive(t, withHeader.Load(), "the configured header wasn't sent")
	assert.Equal(t, []string{"srv"}, m.Servers(), "a disabled server started")
}

// A server's request for input during a tool call goes to whoever the
// call's run asks; with nobody, or no call, it's declined.
func TestMCPElicitation(t *testing.T) {
	m := &MCPManager{}
	s := &mcpServer{cfg: config.MCPServerConfig{Name: "srv"}}
	req := &mcp.ElicitRequest{Params: &mcp.ElicitParams{Message: "Deploy?", RequestedSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"confirm": map[string]any{"type": "boolean", "title": "Go ahead"},
			"env":     map[string]any{"type": "string", "enum": []any{"staging", "prod"}},
			"count":   map[string]any{"type": "integer"},
		},
	}}}
	res, err := m.elicit(s, req)
	require.NoError(t, err)
	assert.Equal(t, "decline", res.Action, "no call in progress")

	var asked []string
	m.Ask = func(_ context.Context, q string, options []string) (string, error) {
		asked = append(asked, q)
		switch {
		case len(options) == 2 && options[0] == "true":
			return "true", nil
		case len(options) == 2:
			return "staging", nil
		}
		return "3", nil
	}
	s.call = context.Background()
	res, err = m.elicit(s, req)
	require.NoError(t, err)
	assert.Equal(t, "accept", res.Action)
	assert.Equal(t, map[string]any{"confirm": true, "env": "staging", "count": 3}, res.Content)
	require.Len(t, asked, 3)
	assert.Contains(t, asked[0], `The MCP server "srv" asks: Deploy?`)

	m.Ask = func(context.Context, string, []string) (string, error) { return "", context.Canceled }
	res, _ = m.elicit(s, req)
	assert.Equal(t, "decline", res.Action)
}
