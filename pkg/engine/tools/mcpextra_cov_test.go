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
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serveMCP serves srv over streamable HTTP for the test and returns its URL.
func serveMCP(t *testing.T, srv *mcp.Server) string {
	t.Helper()
	ts := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil))
	t.Cleanup(ts.Close)
	t.Cleanup(func() {
		for ss := range srv.Sessions() {
			ss.Close()
		}
	})
	return ts.URL
}

// httpManager is a manager over the given HTTP servers, closed with the test.
func httpManager(t *testing.T, cfgs ...config.MCPServerConfig) *MCPManager {
	t.Helper()
	m, err := NewMCPManager(cfgs, nil, nil)
	require.NoError(t, err)
	t.Cleanup(m.Close)
	return m
}

// failMethods makes srv answer the named methods with an error.
func failMethods(srv *mcp.Server, methods ...string) {
	srv.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			for _, m := range methods {
				if method == m {
					return nil, errors.New("broken " + method)
				}
			}
			return next(ctx, method, req)
		}
	})
}

// list_mcp_resources and read_mcp_resource ask for a server as its tools
// are asked for: an auto-approved server is used at once, others only
// once approved, and a server that doesn't exist is an error.
func TestMCPResourceToolsApproval(t *testing.T) {
	url, _ := mcpHTTPServer(t)
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Tools.ApprovalsFile = ""
	cfg.MCP.Servers = []config.MCPServerConfig{{Name: "srv", URL: url}, {Name: "auto", URL: url, AutoApprove: true}}
	reg, err := NewRegistry(cfg, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { reg.Close() })
	list := reg.tools["list_mcp_resources"].(runnerTool)
	read := reg.tools["read_mcp_resource"].(runnerTool)

	tests := []struct {
		name    string
		tool    runnerTool
		args    map[string]any
		approve bool
		asked   int
		want    string // in the output
		wantErr string
	}{
		{"list every server", list, map[string]any{}, false, 0, "file:///readme", ""},
		{"list a denied server", list, map[string]any{"server": "srv"}, false, 1, "", "not approved"},
		{"list an approved server", list, map[string]any{"server": "srv"}, true, 1, "file:///readme", ""},
		{"list an unknown server", list, map[string]any{"server": "nope"}, true, 0, "", `no MCP server "nope"`},
		{"read a denied server", read, map[string]any{"server": "srv", "uri": "file:///readme"}, false, 1, "", "not approved"},
		{"read an auto-approved server", read, map[string]any{"server": "auto", "uri": "file:///readme"}, false, 0, "hello from the server", ""},
		{"read a missing resource", read, map[string]any{"server": "auto", "uri": "file:///missing"}, false, 0, "", `MCP server "auto"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, reqs := approverHooks(tt.approve)
			reg.hooks = h
			out := runTool(t, tt.tool, tt.args)
			assert.Len(t, *reqs, tt.asked, "approval requests")
			if tt.wantErr != "" {
				assert.Contains(t, strings.ToLower(errOf(out)), strings.ToLower(tt.wantErr))
				return
			}
			assert.Empty(t, errOf(out))
			assert.Contains(t, fmt.Sprint(out), tt.want)
		})
	}
}

// A server that can't be reached fails resources and prompts with its
// error, and after repeated failures it's paused rather than tried again.
func TestMCPExtrasUnreachableServer(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	m := httpManager(t, config.MCPServerConfig{Name: "gone", URL: dead.URL})
	var warnings []string
	m.Warn = func(s string) { warnings = append(warnings, s) }
	ctx := context.Background()

	_, err := m.Resources(ctx, "")
	require.Error(t, err)
	_, err = m.ReadResource(ctx, "gone", "file:///x")
	require.Error(t, err)
	_, err = m.GetPrompt(ctx, "gone", "greet", nil)
	assert.ErrorContains(t, err, "unavailable after repeated failures")
	assert.Empty(t, m.Prompts(ctx, time.Second), "a paused server's prompts")
	assert.NotEmpty(t, warnings, "the failures weren't reported")
}

// Servers without a session the manager can use (a toolset the caller
// built), without a toolset, or not the one asked for give nothing.
func TestMCPExtrasWithoutSession(t *testing.T) {
	m := NewMCPManagerFromToolsets([]MCPToolset{
		{Config: config.MCPServerConfig{Name: "mem"}, Toolset: inMemoryMCP(t, "echo")},
		{Config: config.MCPServerConfig{Name: "empty"}},
	}, nil)
	ctx := context.Background()

	_, err := m.Resources(ctx, "mem")
	assert.ErrorContains(t, err, `MCP server "mem": no session`)
	res, err := m.Resources(ctx, "empty")
	require.NoError(t, err, "a server without a toolset offers no resources")
	assert.Empty(t, res)
	_, err = m.Resources(ctx, "nobody")
	assert.ErrorContains(t, err, `no MCP server "nobody"`)
	assert.Empty(t, m.Prompts(ctx, time.Second))
	_, err = m.GetPrompt(ctx, "nobody", "greet", nil)
	assert.ErrorContains(t, err, `no MCP server "nobody"`)
	_, err = m.GetPrompt(ctx, "mem", "greet", nil)
	assert.ErrorContains(t, err, "no session")

	text, ok, err := m.ResourceMention(ctx, "nobody:file:///x")
	assert.NoError(t, err)
	assert.False(t, ok, "a mention of a server that doesn't exist")
	assert.Empty(t, text)
}

// What a server offers and how it fails shape resources and prompts: a
// server without resources or prompts lists none, a listing that fails
// is an error (resources) or nothing (prompts), binary content is
// described, long text is cut, and embedded resources are a prompt's text.
func TestMCPExtrasServerContent(t *testing.T) {
	plain := mcp.NewServer(&mcp.Implementation{Name: "plain", Version: "1"}, nil)
	mcp.AddTool(plain, &mcp.Tool{Name: "noop"}, func(context.Context, *mcp.CallToolRequest, echoArgs) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{}, nil, nil
	})

	rich := mcp.NewServer(&mcp.Implementation{Name: "rich", Version: "1"}, nil)
	rich.AddResource(&mcp.Resource{URI: "file:///logo", Name: "logo"}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: "file:///logo", MIMEType: "image/png", Blob: []byte{1, 2, 3}}}}, nil
	})
	rich.AddResource(&mcp.Resource{URI: "file:///big", Name: "big"}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: "file:///big", Text: strings.Repeat("x", maxResourceText+10)}}}, nil
	})
	rich.AddPrompt(&mcp.Prompt{Name: "embed"}, func(context.Context, *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: "intro"}},
			{Role: "user", Content: &mcp.EmbeddedResource{Resource: &mcp.ResourceContents{URI: "file:///notes", Text: "embedded notes"}}},
			{Role: "user", Content: &mcp.EmbeddedResource{Resource: &mcp.ResourceContents{URI: "file:///blob", Blob: []byte{1}}}},
		}}, nil
	})

	broken := mcp.NewServer(&mcp.Implementation{Name: "broken", Version: "1"}, nil)
	broken.AddResource(&mcp.Resource{URI: "file:///x", Name: "x"}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{}, nil
	})
	broken.AddPrompt(&mcp.Prompt{Name: "p"}, func(context.Context, *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{}, nil
	})
	failMethods(broken, "resources/list", "prompts/list")

	m := httpManager(t,
		config.MCPServerConfig{Name: "plain", URL: serveMCP(t, plain)},
		config.MCPServerConfig{Name: "rich", URL: serveMCP(t, rich)},
		config.MCPServerConfig{Name: "broken", URL: serveMCP(t, broken)},
	)
	ctx := context.Background()

	res, err := m.Resources(ctx, "plain")
	require.NoError(t, err)
	assert.Empty(t, res, "a server without resources")
	_, err = m.Resources(ctx, "broken")
	assert.ErrorContains(t, err, "broken resources/list")
	res, err = m.Resources(ctx, "rich")
	require.NoError(t, err)
	assert.Len(t, res, 2)

	prompts := m.Prompts(ctx, 5*time.Second)
	require.Len(t, prompts, 1, "only rich lists its prompts: %v", prompts)
	assert.Equal(t, "mcp__rich__embed", prompts[0].Command())

	text, err := m.ReadResource(ctx, "rich", "file:///logo")
	require.NoError(t, err)
	assert.Equal(t, "(binary content, 3 bytes, image/png)", text)
	text, err = m.ReadResource(ctx, "rich", "file:///big")
	require.NoError(t, err)
	assert.Len(t, text, maxResourceText+len("\n… (cut)"))
	assert.True(t, strings.HasSuffix(text, "… (cut)"), "long text isn't marked as cut")

	got, err := m.GetPrompt(ctx, "rich", "embed", nil)
	require.NoError(t, err)
	assert.Equal(t, "intro\n\nembedded notes", got)
	_, err = m.GetPrompt(ctx, "rich", "missing", nil)
	assert.ErrorContains(t, err, `MCP server "rich", prompt "missing"`)
}

// A server's elicitation during a tool call reaches the manager's Ask
// over the real connection, and the answer goes back to the server.
func TestMCPElicitationOverTheWire(t *testing.T) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "deployer", Version: "1"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "deploy"}, func(ctx context.Context, req *mcp.CallToolRequest, _ echoArgs) (*mcp.CallToolResult, any, error) {
		res, err := req.Session.Elicit(ctx, &mcp.ElicitParams{Message: "Where to?", RequestedSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"env": map[string]any{"type": "string"}},
		}})
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("%s %v", res.Action, res.Content["env"])}}}, nil, nil
	})
	m := httpManager(t, config.MCPServerConfig{Name: "deployer", URL: serveMCP(t, srv)})
	var asked []string
	m.Ask = func(_ context.Context, q string, _ []string) (string, error) {
		asked = append(asked, q)
		return "prod", nil
	}
	tl, err := m.Toolsets()[0].Tools(createTestToolContext())
	require.NoError(t, err)
	require.Len(t, tl, 1)
	out, err := tl[0].(runnerTool).Run(createTestToolContext(), map[string]any{"text": ""})
	require.NoError(t, err)
	assert.Contains(t, fmt.Sprint(out), "accept prod")
	require.Len(t, asked, 1)
	assert.Contains(t, asked[0], `The MCP server "deployer" asks: Where to?`)
}

// Each kind of elicitation becomes the right questions and answer: a
// page to visit, typed fields (an answer that isn't a number declines),
// and a bare message with no schema.
func TestMCPElicitationKinds(t *testing.T) {
	field := func(props map[string]any) map[string]any {
		return map[string]any{"type": "object", "properties": props}
	}
	tests := []struct {
		name       string
		params     *mcp.ElicitParams
		answer     string
		askErr     error
		wantAction string
		wantQ      string
		want       map[string]any
	}{
		{"visit accepted", &mcp.ElicitParams{Message: "Sign in", URL: "https://example.com/auth"}, "Continue", nil, "accept", "https://example.com/auth", nil},
		{"visit declined", &mcp.ElicitParams{Message: "Sign in", URL: "https://example.com/auth"}, "Decline", nil, "decline", "Open it and continue?", nil},
		{"number", &mcp.ElicitParams{Message: "Size", RequestedSchema: field(map[string]any{"size": map[string]any{"type": "number", "description": "in GB"}})}, "1.5", nil, "accept", "size (in GB)", map[string]any{"size": 1.5}},
		{"not a number", &mcp.ElicitParams{Message: "Size", RequestedSchema: field(map[string]any{"size": map[string]any{"type": "number"}})}, "big", nil, "decline", "size", nil},
		{"not an integer", &mcp.ElicitParams{Message: "Count", RequestedSchema: field(map[string]any{"n": map[string]any{"type": "integer"}})}, "many", nil, "decline", "n", nil},
		{"no schema", &mcp.ElicitParams{Message: "Why?"}, "because", nil, "accept", "Why?", map[string]any{"answer": "because"}},
		{"no schema, no answer", &mcp.ElicitParams{Message: "Why?"}, "", errors.New("gone"), "decline", "Why?", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var asked []string
			m := &MCPManager{Ask: func(_ context.Context, q string, _ []string) (string, error) {
				asked = append(asked, q)
				return tt.answer, tt.askErr
			}}
			s := &mcpServer{cfg: config.MCPServerConfig{Name: "srv"}, call: context.Background()}
			res, err := m.elicit(s, &mcp.ElicitRequest{Params: tt.params})
			require.NoError(t, err)
			assert.Equal(t, tt.wantAction, res.Action)
			require.Len(t, asked, 1)
			assert.Contains(t, asked[0], tt.wantQ)
			if tt.want != nil {
				assert.Equal(t, tt.want, res.Content)
			}
		})
	}
}

// Only a schema with fields is asked about field by field.
func TestRequestedSchema(t *testing.T) {
	tests := []struct {
		name   string
		schema any
		fields int
	}{
		{"none", nil, 0},
		{"not JSON", map[string]any{"x": func() {}}, 0},
		{"not an object", "text", 0},
		{"no fields", map[string]any{"type": "object"}, 0},
		{"fields", map[string]any{"properties": map[string]any{"a": map[string]any{"type": "string"}}}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := requestedSchema(tt.schema)
			if tt.fields == 0 {
				assert.Nil(t, s)
				return
			}
			require.NotNil(t, s)
			assert.Len(t, s.Properties, tt.fields)
		})
	}
}

// A server's question goes to a background task's session when there is
// one, is refused in an unattended run, and otherwise goes to the front
// end's prompter, if any.
func TestHooksAskUser(t *testing.T) {
	prompter := func(context.Context, string, []string) (string, error) { return "from the user", nil }
	task := TaskAsker{Ask: func(context.Context, string, []string) (string, error) { return "from the task", nil }}
	tests := []struct {
		name     string
		ctx      context.Context
		prompter api.UserPromptFunc
		want     string
		wantErr  string
	}{
		{"background task", WithTaskAsker(Background(context.Background()), task), prompter, "from the task", ""},
		{"unattended", Unattended(context.Background(), nil), prompter, "", "unattended"},
		{"no prompter", context.Background(), nil, "", "no one to ask"},
		{"the user", context.Background(), prompter, "from the user", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHooks(Policy{})
			if tt.prompter != nil {
				h.SetUserPrompter(tt.prompter)
			}
			got, err := h.askUser(tt.ctx, "Proceed?", nil)
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
