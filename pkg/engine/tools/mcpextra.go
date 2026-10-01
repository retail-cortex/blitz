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
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// Beyond tools, an MCP server has resources and prompts, and may ask the
// user for input during a tool call (elicitation) (spec_parity_027
// PAR-MCP-02, -03, -05). Blitz gives each server its own MCP client: it
// answers elicitation, and it notes the session the ADK's toolset opens,
// so resources and prompts go over the same connection.

// mcpClient is server s's MCP client.
func (m *MCPManager) mcpClient(s *mcpServer) *mcp.Client {
	c := mcp.NewClient(&mcp.Implementation{Name: "blitz", Version: "1"}, &mcp.ClientOptions{
		ElicitationHandler: func(ctx context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			return m.elicit(s, req)
		},
	})
	// Every request carries the session: the latest is the live one.
	c.AddSendingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if cs, ok := req.GetSession().(*mcp.ClientSession); ok {
				s.sessionMu.Lock()
				s.session = cs
				s.sessionMu.Unlock()
			}
			return next(ctx, method, req)
		}
	})
	return c
}

// sessionOf is server s's live session, connecting first if need be.
func (m *MCPManager) sessionOf(ctx context.Context, s *mcpServer) (*mcp.ClientSession, error) {
	s.sessionMu.Lock()
	cs := s.session
	s.sessionMu.Unlock()
	if cs != nil {
		return cs, nil
	}
	if ok, retryIn := s.health.Allow(); !ok {
		return nil, fmt.Errorf("MCP server %q is unavailable after repeated failures; retrying in %s", s.cfg.Name, retryIn.Round(time.Second))
	}
	lctx, cancel := context.WithTimeout(ctx, mcpListTimeout)
	defer cancel()
	if _, err := s.toolset.Tools(readonlyWith{ctx: lctx}); err != nil { // connects
		// The failed connect's session was noted as it sent initialize:
		// forget it, or every later call would use it and fail.
		s.sessionMu.Lock()
		s.session = nil
		s.sessionMu.Unlock()
		m.recordFailure(ctx, s, err)
		return nil, err
	}
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	if s.session == nil {
		return nil, fmt.Errorf("MCP server %q: no session", s.cfg.Name)
	}
	return s.session, nil
}

// server is the running server named name.
func (m *MCPManager) server(name string) (*mcpServer, error) {
	for _, s := range m.servers {
		if s.cfg.Name == name {
			return s, nil
		}
	}
	return nil, fmt.Errorf("no MCP server %q", name)
}

// MCPResource is a resource an MCP server offers.
type MCPResource struct {
	Server      string `json:"server"`
	URI         string `json:"uri"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	MIMEType    string `json:"mime_type,omitempty"`
}

// Resources lists the resources of server (every server when "").
func (m *MCPManager) Resources(ctx context.Context, server string) ([]MCPResource, error) {
	var out []MCPResource
	var errs []error
	for _, s := range m.servers {
		if server != "" && s.cfg.Name != server || s.toolset == nil {
			continue
		}
		cs, err := m.sessionOf(ctx, s)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if cs.InitializeResult() != nil && cs.InitializeResult().Capabilities != nil && cs.InitializeResult().Capabilities.Resources == nil {
			continue // offers none
		}
		for r, err := range cs.Resources(ctx, nil) {
			if err != nil {
				errs = append(errs, fmt.Errorf("MCP server %q: %w", s.cfg.Name, err))
				break
			}
			out = append(out, MCPResource{Server: s.cfg.Name, URI: r.URI, Name: r.Name, Description: r.Description, MIMEType: r.MIMEType})
		}
	}
	if server != "" && len(out) == 0 && len(errs) == 0 {
		if _, err := m.server(server); err != nil {
			return nil, err
		}
	}
	return out, errors.Join(errs...)
}

// maxResourceText bounds a resource's text as a tool or mention returns it.
const maxResourceText = 256 << 10

// ReadResource returns the text of server's resource uri (binary content
// described, not included).
func (m *MCPManager) ReadResource(ctx context.Context, server, uri string) (string, error) {
	s, err := m.server(server)
	if err != nil {
		return "", err
	}
	cs, err := m.sessionOf(ctx, s)
	if err != nil {
		return "", err
	}
	res, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		return "", fmt.Errorf("MCP server %q: %w", server, err)
	}
	var b strings.Builder
	for _, c := range res.Contents {
		if c.Text != "" {
			b.WriteString(c.Text)
		} else if len(c.Blob) > 0 {
			fmt.Fprintf(&b, "(binary content, %d bytes, %s)", len(c.Blob), c.MIMEType)
		}
		if b.Len() > maxResourceText {
			return b.String()[:maxResourceText] + "\n… (cut)", nil
		}
	}
	return b.String(), nil
}

// MCPPrompt is a prompt an MCP server offers, as the slash command
// /mcp__<server>__<name>.
type MCPPrompt struct {
	Server      string
	Name        string
	Description string
	// Arguments are its arguments' names, in order; Required those that
	// must be given.
	Arguments []string
	Required  []string
}

// Command is the prompt's slash command name.
func (p MCPPrompt) Command() string { return "mcp__" + p.Server + "__" + p.Name }

// Prompts lists every server's prompts, each server waiting at most wait.
func (m *MCPManager) Prompts(ctx context.Context, wait time.Duration) []MCPPrompt {
	var mu sync.Mutex
	var out []MCPPrompt
	var wg sync.WaitGroup
	for _, s := range m.servers {
		if s.toolset == nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sctx, cancel := context.WithTimeout(ctx, wait)
			defer cancel()
			cs, err := m.sessionOf(sctx, s)
			if err != nil || cs.InitializeResult() != nil && cs.InitializeResult().Capabilities != nil && cs.InitializeResult().Capabilities.Prompts == nil {
				return
			}
			for p, err := range cs.Prompts(sctx, nil) {
				if err != nil {
					return
				}
				mp := MCPPrompt{Server: s.cfg.Name, Name: p.Name, Description: p.Description}
				for _, a := range p.Arguments {
					mp.Arguments = append(mp.Arguments, a.Name)
					if a.Required {
						mp.Required = append(mp.Required, a.Name)
					}
				}
				mu.Lock()
				out = append(out, mp)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool { return out[i].Command() < out[j].Command() })
	return out
}

// GetPrompt runs prompt name of server with args and returns its messages
// as the text of a turn.
func (m *MCPManager) GetPrompt(ctx context.Context, server, name string, args map[string]string) (string, error) {
	s, err := m.server(server)
	if err != nil {
		return "", err
	}
	cs, err := m.sessionOf(ctx, s)
	if err != nil {
		return "", err
	}
	res, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{Name: name, Arguments: args})
	if err != nil {
		return "", fmt.Errorf("MCP server %q, prompt %q: %w", server, name, err)
	}
	var parts []string
	for _, msg := range res.Messages {
		switch c := msg.Content.(type) {
		case *mcp.TextContent:
			parts = append(parts, c.Text)
		case *mcp.EmbeddedResource:
			if c.Resource != nil && c.Resource.Text != "" {
				parts = append(parts, c.Resource.Text)
			}
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

// elicit answers server s's request for input during a tool call: the
// question goes to whoever the call's run asks (the user, or a task's
// session), each requested field in turn; unattended runs decline.
func (m *MCPManager) elicit(s *mcpServer, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
	ctx := s.currentCall()
	decline := &mcp.ElicitResult{Action: "decline"}
	if ctx == nil || m.Ask == nil {
		return decline, nil
	}
	p := req.Params
	intro := fmt.Sprintf("The MCP server %q asks: %s", s.cfg.Name, p.Message)
	if p.URL != "" { // the server wants the user to visit a page
		answer, err := m.Ask(ctx, intro+"\n\n"+p.URL+"\n\nOpen it and continue?", []string{"Continue", "Decline"})
		if err != nil || answer != "Continue" {
			return decline, nil
		}
		return &mcp.ElicitResult{Action: "accept"}, nil
	}
	content := map[string]any{}
	if schema := requestedSchema(p.RequestedSchema); schema != nil {
		names := make([]string, 0, len(schema.Properties))
		for n := range schema.Properties {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			prop := schema.Properties[n]
			q := intro + "\n\n" + n
			if prop.Title != "" {
				q = intro + "\n\n" + prop.Title
			}
			if prop.Description != "" {
				q += " (" + prop.Description + ")"
			}
			var options []string
			for _, e := range prop.Enum {
				options = append(options, fmt.Sprint(e))
			}
			if prop.Type == "boolean" {
				options = []string{"true", "false"}
			}
			answer, err := m.Ask(ctx, q, options)
			if err != nil {
				return decline, nil
			}
			switch prop.Type {
			case "boolean":
				content[n] = strings.EqualFold(strings.TrimSpace(answer), "true")
			case "integer":
				v, err := strconv.Atoi(strings.TrimSpace(answer))
				if err != nil {
					return decline, nil
				}
				content[n] = v
			case "number":
				v, err := strconv.ParseFloat(strings.TrimSpace(answer), 64)
				if err != nil {
					return decline, nil
				}
				content[n] = v
			default:
				content[n] = answer
			}
		}
	} else {
		answer, err := m.Ask(ctx, intro, nil)
		if err != nil {
			return decline, nil
		}
		content["answer"] = answer
	}
	return &mcp.ElicitResult{Action: "accept", Content: content}, nil
}

// currentCall is the context of server s's tool call in progress, for
// its elicitation to reach the right person; nil when none runs.
func (s *mcpServer) currentCall() context.Context {
	s.callMu.Lock()
	defer s.callMu.Unlock()
	return s.call
}

// ListMCPResourcesInput optionally names a server.
type ListMCPResourcesInput struct {
	Server string `json:"server,omitempty" jsonschema:"An MCP server's name; empty lists every server's resources"`
}

// ListMCPResourcesOutput lists resources.
type ListMCPResourcesOutput struct {
	Resources []MCPResource `json:"resources"`
	Error     string        `json:"error,omitempty"`
}

// ReadMCPResourceInput names a resource.
type ReadMCPResourceInput struct {
	Server string `json:"server" jsonschema:"The MCP server's name"`
	URI    string `json:"uri" jsonschema:"The resource's URI, from list_mcp_resources"`
}

// ReadMCPResourceOutput is a resource's text.
type ReadMCPResourceOutput struct {
	Text  string `json:"text,omitempty"`
	Error string `json:"error,omitempty"`
}

// NewMCPResourceTools creates list_mcp_resources and read_mcp_resource.
// Both only read; each server's use is approved as its tools are.
func NewMCPResourceTools(r *Registry) ([]tool.Tool, error) {
	list, err := functiontool.New(
		functiontool.Config{Name: "list_mcp_resources", Description: "List the resources (documents, records, files) that MCP servers offer, to read with read_mcp_resource"},
		func(ctx agent.Context, in ListMCPResourcesInput) (ListMCPResourcesOutput, error) {
			if in.Server != "" {
				if err := r.approveMCPServer(ctx, in.Server, "list_mcp_resources", nil); err != nil {
					return ListMCPResourcesOutput{Error: err.Error()}, nil
				}
			}
			res, err := r.mcp.Resources(ctx, in.Server)
			out := ListMCPResourcesOutput{Resources: res}
			if out.Resources == nil {
				out.Resources = []MCPResource{}
			}
			if err != nil {
				out.Error = err.Error()
			}
			return out, nil
		})
	if err != nil {
		return nil, err
	}
	read, err := functiontool.New(
		functiontool.Config{Name: "read_mcp_resource", Description: "Read a resource an MCP server offers, by its server and URI"},
		func(ctx agent.Context, in ReadMCPResourceInput) (ReadMCPResourceOutput, error) {
			if err := r.approveMCPServer(ctx, in.Server, "read_mcp_resource", map[string]any{"uri": in.URI}); err != nil {
				return ReadMCPResourceOutput{Error: err.Error()}, nil
			}
			text, err := r.mcp.ReadResource(ctx, in.Server, in.URI)
			if err != nil {
				return ReadMCPResourceOutput{Error: err.Error()}, nil
			}
			return ReadMCPResourceOutput{Text: text}, nil
		})
	if err != nil {
		return nil, err
	}
	return []tool.Tool{list, read}, nil
}

// approveMCPServer approves using server as its tools are: at once for an
// auto-approved server, else through the usual approval.
func (r *Registry) approveMCPServer(ctx context.Context, server, action string, args map[string]any) error {
	s, err := r.mcp.server(server)
	if err != nil {
		return err
	}
	if s.cfg.AutoApprove {
		return nil
	}
	return r.hooks.Approve(ctx, mcpApproval(server, action, args))
}

// askUser asks the user a question as ask_user_question does: a
// background task's session, else the front end's prompter; nobody in an
// unattended run.
func (h *Hooks) askUser(ctx context.Context, question string, options []string) (string, error) {
	if a, ok := taskAskerFrom(ctx); ok && a.Ask != nil {
		return a.Ask(ctx, question, options)
	}
	if isUnattended(ctx) {
		return "", errors.New("nobody can answer in an unattended run")
	}
	p := h.userPrompter()
	if p == nil {
		return "", errors.New("no one to ask")
	}
	return p(ctx, question, options)
}

// ResourceMention reads "@server:uri" for a prompt: the resource's text
// and whether server is an MCP server (else it isn't a resource mention).
func (m *MCPManager) ResourceMention(ctx context.Context, mention string) (text string, ok bool, err error) {
	server, uri, found := strings.Cut(mention, ":")
	if !found || uri == "" {
		return "", false, nil
	}
	if _, err := m.server(server); err != nil {
		return "", false, nil
	}
	text, err = m.ReadResource(ctx, server, uri)
	return text, true, err
}

// elicitSchema is the part of an elicitation's requested schema Blitz
// asks about: flat fields of simple types.
type elicitSchema struct {
	Properties map[string]struct {
		Type        string `json:"type"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Enum        []any  `json:"enum"`
	} `json:"properties"`
}

// requestedSchema reads the schema an elicitation sent (nil: none).
func requestedSchema(v any) *elicitSchema {
	if v == nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var s elicitSchema
	if json.Unmarshal(data, &s) != nil || len(s.Properties) == 0 {
		return nil
	}
	return &s
}
