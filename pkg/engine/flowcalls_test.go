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
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/config/configtest"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	adka2a "google.golang.org/adk/v2/server/adka2a/v2"
	adksession "google.golang.org/adk/v2/session"
)

// openCalls opens a workspace whose workflows may reach this machine (a
// test server) without asking, with a secret "shop" in memory (8
// characters or more: shorter values aren't masked).
func openCalls(t *testing.T, more func(*config.Config)) *Workspace {
	t.Helper()
	w, _ := openTestWith(t, func(c *config.Config) {
		configtest.RunTools(c)
		c.Web.Enabled, c.Web.AllowPrivate, c.Sandbox.AllowNetwork = true, true, true
		c.Web.AllowDomains = []string{"127.0.0.1"}
		if more != nil {
			more(c)
		}
	})
	secrets.SetDefault(&secrets.Memory{})
	require.NoError(t, config.Vault("").Set(secrets.GlobalScope, "shop", "tok-12345678"))
	return w
}

// stepOutputs collects each step's status and output preview.
func stepOutputs(mu *sync.Mutex, into map[string]string) func(api.Event) {
	return func(e api.Event) {
		if e.Step != nil {
			mu.Lock()
			into[e.Step.ID] = e.Step.Status + ": " + e.Step.Output + e.Step.Error
			mu.Unlock()
		}
	}
}

// An API call: a bearer token from a secret, a query and a templated
// header; its JSON response is data a condition and the next call read;
// that call gets an OAuth2 token (client credentials) and sends JSON; a
// 404 fails its step, caught by an error link. The secret never shows.
func TestFlowAPICall(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	tokens := 0
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/token":
			tokens++
			require.NoError(t, r.ParseForm())
			id, secret, _ := r.BasicAuth()
			seen = append(seen, "token "+id+":"+secret+" "+r.Form.Get("grant_type")+" "+r.Form.Get("audience"))
			rw.Header().Set("Content-Type", "application/json")
			_, _ = rw.Write([]byte(`{"access_token": "oauth-abc", "token_type": "bearer", "expires_in": 3600}`))
		case "/orders/42":
			seen = append(seen, "get "+r.Header.Get("Authorization")+" "+r.URL.Query().Get("expand")+" "+r.Header.Get("X-Order"))
			_, _ = rw.Write([]byte(`{"status": "open", "total": 150, "echo": "tok-12345678"}`))
		case "/notes":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			seen = append(seen, "post "+r.Header.Get("Authorization")+" "+r.Header.Get("Content-Type")+" "+body["order"].(string)+" "+string(must(json.Marshal(body["total"]))))
			rw.WriteHeader(http.StatusCreated)
			_, _ = rw.Write([]byte(`{"id": "n1"}`))
		default:
			http.NotFound(rw, r)
		}
	}))
	defer srv.Close()
	w := openCalls(t, nil)
	writeFlow(t, w, "orders", `nodes:
  - id: get
    type: api_call
    url: "`+srv.URL+`/orders/{{ input }}"
    query: [{name: expand, value: items}]
    headers: [{name: X-Order, value: "order {{ input }}"}]
    auth: {type: bearer, token: "{secrets:shop}"}
  - id: note
    type: api_call
    method: POST
    url: "`+srv.URL+`/notes"
    json: {order: "{{ input }}", total: "{{ steps.get.data.body.total }}"}
    auth: {type: oauth2, token_url: "`+srv.URL+`/token", client_id: blitz, client_secret: "{secrets:shop}", audience: shop}
  - {id: gone, type: api_call, url: "`+srv.URL+`/missing"}
  - {id: caught, type: accumulate}
edges:
  - {from: get, to: note, when: "output.data.body.total > 100 && output.data.status == 200"}
  - {from: get, to: gone}
  - {from: gone, to: caught, error: true}
`)
	s := newSession(t, w)
	var emu sync.Mutex
	outs := map[string]string{}
	res, err := w.Run(context.Background(), s.ID, api.Turn{Text: "/orders 42", Command: true}, stepOutputs(&emu, outs))
	require.NoError(t, err)
	assert.Equal(t, []string{
		"get Bearer tok-12345678 items order 42",
		"token blitz:tok-12345678 client_credentials shop",
		"post Bearer oauth-abc application/json 42 150",
	}, seen[:3])
	assert.Contains(t, outs["note"], `"id": "n1"`)
	assert.Contains(t, outs["gone"], "404 Not Found")
	assert.Contains(t, res.Output, "404 Not Found", "the error went on as the next step's input")
	for id, o := range outs {
		assert.NotContains(t, o, "tok-12345678", "step %s shows the secret", id)
	}

	// A second run reuses the OAuth2 token.
	_, err = w.Run(context.Background(), s.ID, api.Turn{Text: "/orders 42", Command: true}, func(api.Event) {})
	require.NoError(t, err)
	assert.Equal(t, 1, tokens)
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// An API call needs the web on, and a host off web.allow_domains is asked
// about (here, refused: nobody can be asked).
func TestFlowAPICallRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { _, _ = rw.Write([]byte("ok")) }))
	defer srv.Close()
	w, _ := openTestWith(t, func(c *config.Config) { c.Web.Enabled = false })
	writeFlow(t, w, "call", "nodes: [{id: a, type: api_call, url: '"+srv.URL+"'}]\n")
	s := newSession(t, w)
	_, err := w.Run(context.Background(), s.ID, api.Turn{Text: "/call", Command: true}, func(api.Event) {})
	assert.ErrorContains(t, err, "the web is off")

	w2, _ := openTestWith(t, func(c *config.Config) {
		c.Web.Enabled, c.Web.AllowPrivate, c.Sandbox.AllowNetwork = true, true, true
	})
	writeFlow(t, w2, "call", "nodes: [{id: a, type: api_call, url: '"+srv.URL+"'}]\n")
	s2 := newSession(t, w2)
	_, err = w2.Run(context.Background(), s2.ID, api.Turn{Text: "/call", Command: true}, func(api.Event) {})
	require.Error(t, err, "not approved")
}

// An MCP call: its arguments filled in (a value of its own type when the
// template is alone), the tool's text and structured content its result;
// a tool that reports an error fails the step.
func TestFlowMCPCall(t *testing.T) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "shop", Version: "1"}, nil)
	type lookupIn struct {
		Order string `json:"order"`
		Count int    `json:"count"`
	}
	type lookupOut struct {
		Status string `json:"status"`
	}
	mcp.AddTool(srv, &mcp.Tool{Name: "lookup", Description: "Look an order up"}, func(_ context.Context, _ *mcp.CallToolRequest, in lookupIn) (*mcp.CallToolResult, lookupOut, error) {
		return nil, lookupOut{Status: "order " + in.Order + " x" + strings.Repeat("I", in.Count)}, nil
	})
	mcp.AddTool(srv, &mcp.Tool{Name: "broken", Description: "Fails"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, struct{}, error) {
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "the shop is closed"}}}, struct{}{}, nil
	})
	ts := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil))
	t.Cleanup(ts.Close)
	t.Cleanup(func() {
		for ss := range srv.Sessions() {
			ss.Close()
		}
	})
	w := openCalls(t, func(c *config.Config) {
		c.MCP.Servers = []config.MCPServerConfig{{Name: "shop", URL: ts.URL, AutoApprove: true, Agents: []string{"*"}}}
	})
	writeFlow(t, w, "mcp", `nodes:
  - {id: count, type: extract, fields: [{name: n, type: number}]}
  - {id: look, type: mcp_call, server: shop, tool: lookup, arguments: {order: "{{ input }}", count: "{{ int(steps.count.data.n) }}"}}
  - {id: fail, type: mcp_call, server: shop, tool: broken}
  - {id: ok, type: accumulate}
edges:
  - {from: count, to: look}
  - {from: look, to: fail}
  - {from: fail, to: ok, error: true}
`)
	judge := runtime.NewMockLLM("judge", text(`{"n": 3}`))
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return judge, nil }
	s := newSession(t, w)
	var mu sync.Mutex
	outs := map[string]string{}
	_, err := w.Run(context.Background(), s.ID, api.Turn{Text: "/mcp 42", Command: true}, stepOutputs(&mu, outs))
	require.NoError(t, err)
	assert.Contains(t, outs["look"], "order 42 xIII")
	assert.Contains(t, outs["fail"], "the shop is closed")
	assert.Contains(t, outs["ok"], "done")

	writeFlow(t, w, "nosuch", "nodes: [{id: a, type: mcp_call, server: nope, tool: x}]\n")
	_, err = w.Run(context.Background(), s.ID, api.Turn{Text: "/nosuch", Command: true}, func(api.Event) {})
	assert.ErrorContains(t, err, `no MCP server "nope"`)
}

// An A2A call: the remote agent's card is read, its answer is the step's
// output; a host the card names that isn't approved isn't reached.
func TestFlowA2ACall(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	base := &url.URL{Scheme: "http", Host: ln.Addr().String()}
	remote, err := llmagent.New(llmagent.Config{Name: "shipping", Description: "Says when orders ship", Model: runtime.NewMockLLM("remote", text("Order 42 ships Friday."))})
	require.NoError(t, err)
	card := &a2a.AgentCard{Name: "shipping", Description: "Says when orders ship", Version: "1.0.0",
		SupportedInterfaces: []*a2a.AgentInterface{{URL: base.JoinPath("/invoke").String(), ProtocolBinding: a2a.TransportProtocolJSONRPC, ProtocolVersion: a2a.Version}},
		DefaultInputModes:   []string{"text/plain"}, DefaultOutputModes: []string{"text/plain"}}
	exec := adka2a.NewExecutor(adka2a.ExecutorConfig{RunnerConfig: runner.Config{AppName: "shipping", Agent: remote, SessionService: adksession.InMemoryService()}})
	var mu sync.Mutex
	var auth []string
	mux := http.NewServeMux()
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(card))
	rpc := a2asrv.NewJSONRPCHandler(a2asrv.NewHandler(exec))
	mux.Handle("/invoke", http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auth = append(auth, r.Header.Get("Authorization"))
		mu.Unlock()
		rpc.ServeHTTP(rw, r)
	}))
	hs := &http.Server{Handler: mux}
	go func() { _ = hs.Serve(ln) }()
	defer hs.Close()

	w := openCalls(t, nil)
	writeFlow(t, w, "ship", `nodes:
  - id: ask
    type: a2a_call
    url: "`+base.String()+`"
    instructions: "When does order {{ input }} ship?"
    headers: [{name: Authorization, value: "Bearer {secrets:shop}"}]
`)
	s := newSession(t, w)
	res, err := w.Run(context.Background(), s.ID, api.Turn{Text: "/ship 42", Command: true}, func(api.Event) {})
	require.NoError(t, err)
	assert.Equal(t, "Order 42 ships Friday.", res.Output)
	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, auth)
	assert.Equal(t, "Bearer tok-12345678", auth[0], "the headers go with its requests")
}
