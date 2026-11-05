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
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/flows"
	"github.com/retail-cortex/blitz/pkg/redact"
	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/retail-cortex/blitz/pkg/textutil"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
	"google.golang.org/adk/v2/agent"
	remoteagent "google.golang.org/adk/v2/agent/remoteagent/v2"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// Workflows' calls (spec_043_workflows WF-14): MCP tools, remote agents
// (A2A) and HTTP APIs. {secrets:key} in a call's values is put in as it's
// made, never kept in the workflow's events, and masked wherever it
// shows; requests go through web_fetch's guards and approvals.

// flowResponseLimit is the most of a response a step keeps: a longer one
// fails the step rather than being cut (a cut JSON body no longer parses).
const flowResponseLimit = 256 << 10

// stepSecrets puts secrets' values into a call's values and has them
// masked from then on: only those the step's own settings name
// (flows.Node.Secrets), never one a filled-in template brought in.
type stepSecrets struct {
	w     *Workspace
	named map[string]bool
}

// stepSecrets is n's: the secrets its settings name.
func (w *Workspace) stepSecrets(n flows.Node) stepSecrets {
	names := n.Secrets
	if names == nil { // not filled: its settings are as written
		names = namedSecrets(n)
	}
	s := stepSecrets{w: w, named: map[string]bool{}}
	for _, k := range names {
		s.named[k] = true
	}
	return s
}

// text is text with the step's secrets put in.
func (s stepSecrets) text(text string) string {
	lookup := config.Vault("").Resolve(config.SecretScope("", s.w.Dir()))
	out, used := secrets.Expand(text, func(scope, key string) (string, bool) {
		if !s.named[scope+"\x00"+key] {
			return "", false
		}
		return lookup(scope, key)
	})
	redact.Remember(used...)
	return out
}

// value is v with the step's secrets put into each string.
func (s stepSecrets) value(v any) any {
	switch x := v.(type) {
	case string:
		return s.text(x)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = s.value(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = s.value(e)
		}
		return out
	}
	return v
}

// namedSecrets are the {secrets:…} n's call settings name (scope, NUL,
// key), sorted; empty, never nil.
func namedSecrets(n flows.Node) []string {
	seen := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			secrets.Expand(x, func(scope, key string) (string, bool) {
				seen[scope+"\x00"+key] = true
				return "", false
			})
		case map[string]any:
			for _, e := range x {
				walk(e)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	walk(n.URL)
	walk(n.Body)
	for _, p := range append(slices.Clone(n.Query), n.Headers...) {
		walk(p.Value)
	}
	walk(n.Arguments)
	walk(n.JSON)
	if a := n.Auth; a != nil {
		for _, f := range []string{a.Token, a.Username, a.Password, a.Value, a.TokenURL, a.ClientID, a.ClientSecret, a.Audience} {
			walk(f)
		}
	}
	return append([]string{}, slices.Sorted(maps.Keys(seen))...)
}

func (w *Workspace) mcpStep(ctx context.Context, n flows.Node) (flows.Result, error) {
	args, _ := w.stepSecrets(n).value(n.Arguments).(map[string]any)
	res, err := w.tools.CallMCP(ctx, n.Server, n.Tool, args)
	if err != nil {
		return flows.Result{}, err
	}
	var parts []string
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, t.Text)
		}
	}
	text := w.secretRedactor().String(strings.Join(parts, "\n"))
	if res.IsError {
		return flows.Result{}, fmt.Errorf("%s › %s: %s", n.Server, n.Tool, textutil.Ellipsize(text, 600))
	}
	r := flows.Result{Text: text, Data: res.StructuredContent}
	if r.Data == nil {
		r.Data = flows.DataOf(text)
	}
	return r, nil
}

// apiStep makes an API step's request: its text is the response's body,
// its data {status, headers, body} (the body parsed when it's JSON). A
// status of 400 or more fails the step.
func (w *Workspace) apiStep(ctx context.Context, n flows.Node, input string) (flows.Result, error) {
	sec := w.stepSecrets(n)
	method := strings.ToUpper(cmp.Or(n.Method, http.MethodGet))
	u, err := url.Parse(sec.text(strings.TrimSpace(n.URL)))
	if err != nil {
		return flows.Result{}, fmt.Errorf("the URL: %w", err)
	}
	q := u.Query()
	for _, p := range n.Query {
		q.Add(p.Name, sec.text(p.Value))
	}
	if a := n.Auth; a != nil && a.Type == "api_key" && a.In == "query" {
		q.Set(a.Name, sec.text(a.Value))
	}
	u.RawQuery = q.Encode()
	shown := (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}).String()
	if err := w.approveNetwork(ctx, "api_call", method+" "+shown, u); err != nil {
		return flows.Result{}, err
	}
	client, err := w.tools.NetworkClient(nil)
	if err != nil {
		return flows.Result{}, err
	}
	var body io.Reader
	contentType := ""
	switch {
	case n.JSON != nil:
		data, err := json.Marshal(sec.value(n.JSON))
		if err != nil {
			return flows.Result{}, fmt.Errorf("the JSON: %w", err)
		}
		body, contentType = bytes.NewReader(data), "application/json"
	case n.Body != "":
		body = strings.NewReader(sec.text(n.Body))
	case method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch:
		body = strings.NewReader(input) // nothing set: the step's input
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return flows.Result{}, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for _, p := range n.Headers {
		req.Header.Set(p.Name, sec.text(p.Value))
	}
	if err := w.authorize(ctx, req, n.Auth, sec); err != nil {
		return flows.Result{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return flows.Result{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, flowResponseLimit+1))
	if err != nil {
		return flows.Result{}, fmt.Errorf("reading the response: %w", err)
	}
	if len(data) > flowResponseLimit {
		return flows.Result{}, fmt.Errorf("%s %s answered with more than %d KiB, the most a step keeps", method, shown, flowResponseLimit>>10)
	}
	text := w.secretRedactor().String(string(data))
	headers := map[string]any{}
	for k, v := range resp.Header {
		if len(v) > 0 {
			headers[strings.ToLower(k)] = w.secretRedactor().String(v[0])
		}
	}
	var parsed any = text
	if d := flows.DataOf(text); d != nil {
		parsed = d
	}
	if resp.StatusCode >= 400 {
		return flows.Result{}, fmt.Errorf("%s %s answered %s: %s", method, shown, resp.Status, textutil.Ellipsize(text, 600))
	}
	return flows.Result{Text: text, Data: map[string]any{"status": resp.StatusCode, "headers": headers, "body": parsed}}, nil
}

// oauthSources are OAuth2 client credentials' token sources, by token URL,
// client and scopes: a token is fetched once and kept until it expires.
var oauthSources sync.Map

// authorize puts an API step's auth on its request: a bearer token, basic
// auth, an API key in a header, or an OAuth2 token from client
// credentials (its token URL approved as a request is).
func (w *Workspace) authorize(ctx context.Context, req *http.Request, a *flows.Auth, sec stepSecrets) error {
	if a == nil {
		return nil
	}
	switch a.Type {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+sec.text(a.Token))
	case "basic":
		req.SetBasicAuth(sec.text(a.Username), sec.text(a.Password))
	case "api_key":
		if a.In != "query" {
			req.Header.Set(a.Name, sec.text(a.Value))
		}
	case "oauth2":
		tokenURL := sec.text(a.TokenURL)
		tu, err := url.Parse(tokenURL)
		if err != nil {
			return fmt.Errorf("the token URL: %w", err)
		}
		if err := w.approveNetwork(ctx, "api_call", "POST "+tokenURL+" (an OAuth2 token)", tu); err != nil {
			return err
		}
		client, err := w.tools.NetworkClient(nil)
		if err != nil {
			return err
		}
		cc := clientcredentials.Config{ClientID: sec.text(a.ClientID), ClientSecret: sec.text(a.ClientSecret), TokenURL: tokenURL, Scopes: a.Scopes}
		if a.Audience != "" {
			cc.EndpointParams = url.Values{"audience": {sec.text(a.Audience)}}
		}
		key := strings.Join(append([]string{tokenURL, cc.ClientID, a.Audience}, a.Scopes...), "\x00")
		src, _ := oauthSources.LoadOrStore(key, oauth2.ReuseTokenSource(nil, cc.TokenSource(context.WithValue(context.WithoutCancel(ctx), oauth2.HTTPClient, client))))
		tok, err := src.(oauth2.TokenSource).Token()
		if err != nil {
			oauthSources.Delete(key) // tried again next time
			return fmt.Errorf("getting an OAuth2 token: %w", err)
		}
		redact.Remember(tok.AccessToken)
		tok.SetAuthHeader(req)
	}
	return nil
}

// hostsOnly is a transport that refuses requests to hosts not in hosts:
// an A2A call reaches only the hosts approved for it.
type hostsOnly struct {
	hosts   []string
	headers map[string]string
	base    http.RoundTripper
}

func (t hostsOnly) RoundTrip(req *http.Request) (*http.Response, error) {
	if !slices.Contains(t.hosts, strings.ToLower(req.URL.Hostname())) {
		return nil, fmt.Errorf("%s wasn't approved for this call", req.URL.Hostname())
	}
	req = req.Clone(req.Context())
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	return t.base.RoundTrip(req)
}

// a2aStep sends an A2A step's message (its instructions, else its input)
// to the remote agent whose card is at its URL, and gives back its reply
// (and the JSON in it, as data). The card's host and every host it says
// the agent answers at are approved first; no other is reached.
func (w *Workspace) a2aStep(ctx context.Context, n flows.Node, input string) (flows.Result, error) {
	sec := w.stepSecrets(n)
	base := sec.text(strings.TrimSpace(n.URL))
	u, err := url.Parse(base)
	if err != nil {
		return flows.Result{}, fmt.Errorf("the URL: %w", err)
	}
	if err := w.approveNetwork(ctx, "a2a_call", "A2A "+base, u); err != nil {
		return flows.Result{}, err
	}
	headers := map[string]string{}
	for _, p := range n.Headers {
		headers[p.Name] = sec.text(p.Value)
	}
	only := &hostsOnly{hosts: []string{strings.ToLower(u.Hostname())}, headers: headers}
	client, err := w.tools.NetworkClient(func(rt http.RoundTripper) http.RoundTripper { only.base = rt; return only })
	if err != nil {
		return flows.Result{}, err
	}
	card, err := agentcard.NewResolver(client).Resolve(ctx, base)
	if err != nil {
		return flows.Result{}, fmt.Errorf("the agent card at %s: %w", base, err)
	}
	for _, it := range card.SupportedInterfaces {
		su, err := url.Parse(it.URL)
		if err != nil || slices.Contains(only.hosts, strings.ToLower(su.Hostname())) {
			continue
		}
		if err := w.approveNetwork(ctx, "a2a_call", "A2A "+it.URL, su); err != nil {
			return flows.Result{}, err
		}
		only.hosts = append(only.hosts, strings.ToLower(su.Hostname()))
	}
	factory := a2aclient.NewFactory(a2aclient.WithJSONRPCTransport(client), a2aclient.WithRESTTransport(client))
	remote, err := remoteagent.NewA2A(remoteagent.A2AConfig{Name: "a2a_" + strings.ReplaceAll(n.ID, "-", "_"), Description: card.Name, AgentCard: card, ClientProvider: remoteagent.NewA2AClientProvider(factory)})
	if err != nil {
		return flows.Result{}, err
	}
	r, err := runner.New(runner.Config{AppName: "blitz-a2a", Agent: remote, SessionService: adksession.InMemoryService(), AutoCreateSession: true})
	if err != nil {
		return flows.Result{}, err
	}
	msg := cmp.Or(strings.TrimSpace(n.Instructions), input)
	var reply strings.Builder
	for ev, err := range r.Run(ctx, "blitz", "a2a-"+uuid.NewString(), genai.NewContentFromText(msg, genai.RoleUser), agent.RunConfig{}) {
		if err != nil {
			return flows.Result{}, fmt.Errorf("%s: %w", card.Name, err)
		}
		// ADK's remote agent reports a call that failed as an event, not an
		// error: the step fails, rather than answering nothing.
		if ev != nil && ev.ErrorMessage != "" {
			return flows.Result{}, fmt.Errorf("%s: %s", card.Name, ev.ErrorMessage)
		}
		if ev == nil || ev.Partial || ev.Content == nil {
			continue
		}
		for _, p := range ev.Content.Parts {
			if p.Text != "" && !p.Thought {
				reply.WriteString(p.Text)
			}
		}
	}
	text := w.secretRedactor().String(reply.String())
	return flows.Result{Text: text, Data: flows.DataOf(text)}, nil
}
