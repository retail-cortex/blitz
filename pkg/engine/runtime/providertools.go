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

package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// Provider tools (spec_056_conversation_adapters §10): a provider's own
// web search, web fetch and code execution, sent in place of Blitz's client
// tool of the same purpose wherever the model offers one and Blitz's rules
// would offer the client tool, never beside it.
//
// Their calls and results are stored neutrally, as genai's server-side
// ToolCall and ToolResponse parts (which ADK never runs), named in
// PartMetadata by the client tool they stand for, with the provider's own
// blocks kept as provider state (tagged with their origin in
// ThoughtSignature). The model that made them gets them back in its own
// form; any other reads an ordinary call of that client tool and its result
// (asClientCalls).

// The provider tools, by purpose.
const (
	ptWebSearch     = "web_search"
	ptWebFetch      = "web_fetch"
	ptCodeExecution = "code_execution"
)

// Keys a model's answer carries for its provider tools.
const (
	// CitationsKey (CustomMetadata) holds the answer's citations: a list
	// of {"title", "url", "text"} (CA-84).
	CitationsKey = "blitz_citations"
	// ProviderToolRequestsKey (CustomMetadata) counts the provider tool
	// requests the answer made, by purpose ({"web_search": 2}), for
	// pricing (CA-83).
	ProviderToolRequestsKey = "blitz_provider_tool_requests"
	// providerToolKey (PartMetadata) names the provider tool a stored call
	// or result is (its purpose), and providerOriginKey the origin of one
	// that carries no provider state.
	providerToolKey   = "blitz_provider_tool"
	providerOriginKey = "blitz_origin"
)

// providerToolName is the client tool a provider tool's calls are stored
// and read as: its client equivalent, or provider_tool (CA-82).
func providerToolName(kind string) string {
	switch kind {
	case ptWebSearch, ptWebFetch:
		return kind
	}
	return "provider_tool"
}

// gateTool is the client tool whose being offered lets kind be (CA-81):
// what Blitz's rules allow the client tool, they allow the provider's.
// Code execution runs code, as the shell does.
func gateTool(kind string) string {
	if kind == ptCodeExecution {
		return "run_shell_command"
	}
	return kind
}

// providerToolTable is which models offer which provider tools: by wire
// API, the providers that serve them (a cloud's copy of a model may not),
// and model name prefixes. Gemini mixes its built-in tools with function
// calling from Gemini 3; Claude on Bedrock has no server tools.
var providerToolTable = []struct {
	api       string
	providers []string
	prefixes  []string
	kinds     []string
}{
	{"gemini", []string{"gemini", "vertex"}, []string{"gemini-3"}, []string{ptWebSearch, ptWebFetch, ptCodeExecution}},
	{"anthropic", []string{"anthropic", "vertex-anthropic"}, []string{"claude-opus-4", "claude-sonnet-4", "claude-haiku-4-5", "claude-opus-5", "claude-sonnet-5", "claude-fable"}, []string{ptWebSearch, ptWebFetch, ptCodeExecution}},
	{"openai", []string{"openai"}, []string{"gpt-5", "gpt-4.1", "gpt-4o", "o3", "o4"}, []string{ptWebSearch}},
}

// providerToolsFor is the provider tools the model at o offers.
func providerToolsFor(o Origin) []string {
	for _, row := range providerToolTable {
		if row.api != o.API || !slices.Contains(row.providers, o.Provider) {
			continue
		}
		for _, p := range row.prefixes {
			if strings.HasPrefix(o.Model, p) {
				return row.kinds
			}
		}
	}
	return nil
}

// ProviderToolPolicy is what Blitz's rules say about provider tools beyond
// the client tools a request offers (CA-81): all off ([tools] provider =
// false), or some off (an agent's -provider:<kind>, the organization's
// tools.off).
type ProviderToolPolicy struct {
	Off      bool
	Disabled map[string]bool
	// Wants is the client tools the calling agent's tools list: its
	// web_search lets the model search with no search provider set.
	Wants map[string]bool
}

type providerToolPolicyKey struct{}

// WithProviderToolPolicy puts p in ctx for the model calls made with it.
func WithProviderToolPolicy(ctx context.Context, p ProviderToolPolicy) context.Context {
	return context.WithValue(ctx, providerToolPolicyKey{}, p)
}

// ParseProviderToolsOff reads the provider tools a list of tool names
// turns off: "-provider:<kind>" (an agent's tools), "provider:<kind>" (the
// organization's tools.off), "*" for all of them.
func ParseProviderToolsOff(names []string, prefix string) (all bool, off map[string]bool) {
	for _, n := range names {
		kind, ok := strings.CutPrefix(strings.TrimSpace(n), prefix+"provider:")
		if !ok {
			continue
		}
		if kind == "*" {
			all = true
			continue
		}
		if off == nil {
			off = map[string]bool{}
		}
		off[kind] = true
	}
	return all, off
}

func providerToolPolicyFrom(ctx context.Context) ProviderToolPolicy {
	p, _ := ctx.Value(providerToolPolicyKey{}).(ProviderToolPolicy)
	if own, ok := ctx.Value(agentProviderToolsKey{}).(ProviderToolPolicy); ok {
		p.Off = p.Off || own.Off
		disabled := map[string]bool{}
		for k := range p.Disabled {
			disabled[k] = true
		}
		for k := range own.Disabled {
			disabled[k] = true
		}
		p.Disabled, p.Wants = disabled, own.Wants
	}
	return p
}

// agentProviderToolsKey carries the calling agent's own provider tools
// switched off (its tools' -provider:<kind>).
type agentProviderToolsKey struct{}

// declared reports whether cfg declares a function named name.
func declared(cfg *genai.GenerateContentConfig, name string) bool {
	if cfg == nil {
		return false
	}
	for _, t := range cfg.Tools {
		if t != nil && slices.ContainsFunc(t.FunctionDeclarations, func(d *genai.FunctionDeclaration) bool { return d != nil && d.Name == name }) {
			return true
		}
	}
	return false
}

// providerToolsToUse is the provider tools a call to the model at o
// sends (CA-81): those it offers whose client tool Blitz's rules allow —
// offered on the request, or for search, listed in the calling agent's
// tools (no search provider is needed) — that nothing switched off, and,
// in plan mode or a read-only run, only those that change nothing.
func providerToolsToUse(ctx context.Context, o Origin, req *model.LLMRequest) []string {
	if req == nil {
		return nil
	}
	pol := providerToolPolicyFrom(ctx)
	if pol.Off {
		return nil
	}
	planning := tools.PlanGateFrom(ctx).Planning()
	if st := stateFrom(ctx); st != nil && st.planOnly {
		planning = true
	}
	var use []string
	for _, kind := range providerToolsFor(o) {
		gate := gateTool(kind)
		switch {
		case pol.Disabled[kind], planning && !PlanAllows(gate):
		case declared(req.Config, gate), kind == ptWebSearch && pol.Wants[gate]:
			use = append(use, kind)
		}
	}
	return use
}

// withProviderTools returns req offering kinds as provider tools (genai's
// built-in tool fields, which each adapter sends in its API's form) in
// place of their client tools. A client tool the history calls stays
// declared, saying the provider's tool is the one to use now (CA-82). req
// is shared, so a changed one is a copy.
func withProviderTools(req *model.LLMRequest, kinds []string) *model.LLMRequest {
	if len(kinds) == 0 {
		return req
	}
	cp := *req
	cfg := *req.Config
	called := calledTools(req.Contents)
	cfg.Tools = nil
	for _, t := range req.Config.Tools {
		if t == nil || len(t.FunctionDeclarations) == 0 {
			cfg.Tools = append(cfg.Tools, t)
			continue
		}
		nt := *t
		nt.FunctionDeclarations = nil
		for _, d := range t.FunctionDeclarations {
			if d == nil || !slices.Contains(kinds, d.Name) || gateTool(d.Name) != d.Name {
				nt.FunctionDeclarations = append(nt.FunctionDeclarations, d)
				continue
			}
			if called[d.Name] {
				nd := *d
				nd.Description = fmt.Sprintf("Results of %s earlier in this conversation. Don't call it: use the provider's own %s tool.", d.Name, d.Name)
				nt.FunctionDeclarations = append(nt.FunctionDeclarations, &nd)
			}
		}
		if len(nt.FunctionDeclarations) > 0 {
			cfg.Tools = append(cfg.Tools, &nt)
		}
	}
	for _, kind := range kinds {
		cfg.Tools = append(cfg.Tools, builtinTool(kind))
	}
	cp.Config = &cfg
	return &cp
}

// builtinTool is genai's built-in tool for kind.
func builtinTool(kind string) *genai.Tool {
	switch kind {
	case ptWebSearch:
		return &genai.Tool{GoogleSearch: &genai.GoogleSearch{}}
	case ptWebFetch:
		return &genai.Tool{URLContext: &genai.URLContext{}}
	default:
		return &genai.Tool{CodeExecution: &genai.ToolCodeExecution{}}
	}
}

// builtinKinds is the provider tools cfg offers, in genai's fields.
func builtinKinds(cfg *genai.GenerateContentConfig) []string {
	if cfg == nil {
		return nil
	}
	var kinds []string
	for _, t := range cfg.Tools {
		switch {
		case t == nil:
		case t.GoogleSearch != nil:
			kinds = append(kinds, ptWebSearch)
		case t.URLContext != nil:
			kinds = append(kinds, ptWebFetch)
		case t.CodeExecution != nil:
			kinds = append(kinds, ptCodeExecution)
		}
	}
	return kinds
}

// withoutBuiltins is cfg without genai's built-in tools (for an adapter
// that sends them its own way).
func withoutBuiltins(cfg *genai.GenerateContentConfig) *genai.GenerateContentConfig {
	if len(builtinKinds(cfg)) == 0 {
		return cfg
	}
	cp := *cfg
	cp.Tools = slices.DeleteFunc(slices.Clone(cfg.Tools), func(t *genai.Tool) bool {
		return t != nil && (t.GoogleSearch != nil || t.URLContext != nil || t.CodeExecution != nil)
	})
	return &cp
}

func calledTools(cs []*genai.Content) map[string]bool {
	called := map[string]bool{}
	for _, c := range cs {
		if c == nil {
			continue
		}
		for _, p := range c.Parts {
			if p == nil {
				continue
			}
			if p.FunctionCall != nil {
				called[p.FunctionCall.Name] = true
			}
			if kind, ok := p.PartMetadata[providerToolKey].(string); ok {
				called[providerToolName(kind)] = true
			}
		}
	}
	return called
}

// providerCall is a provider tool's call as a stored part: state is the
// provider's own block (JSON) when it has one.
func providerCall(kind, id string, args map[string]any, state []byte) *genai.Part {
	return &genai.Part{
		ToolCall:         &genai.ToolCall{ID: id, Args: args},
		PartMetadata:     map[string]any{providerToolKey: kind},
		ThoughtSignature: state,
	}
}

// providerResult is a provider tool's result as a stored part: the result
// as text, for any model, and the provider's own block.
func providerResult(kind, id, text string, state []byte) *genai.Part {
	return &genai.Part{
		ToolResponse:     &genai.ToolResponse{ID: id, Response: map[string]any{"result": text}},
		PartMetadata:     map[string]any{providerToolKey: kind},
		ThoughtSignature: state,
	}
}

// isProviderPart reports whether p is a provider tool's call or result, or
// Gemini's code execution (its code and result).
func isProviderPart(p *genai.Part) bool {
	return p != nil && (p.ToolCall != nil || p.ToolResponse != nil || p.ExecutableCode != nil || p.CodeExecutionResult != nil)
}

// ownProviderPart reports whether the model at o made p, in a form its
// API takes back: Claude's server tool blocks (its own, signed), Gemini's
// code execution. Gemini's search leaves no parts of its own (the stored
// ones are made from its grounding), and OpenAI's can't be sent back.
func ownProviderPart(o Origin, p *genai.Part) bool {
	switch o.API {
	case "anthropic":
		key, payload, ok := untagState(p.ThoughtSignature)
		return ok && key == o.key() && len(payload) > 0
	case "gemini":
		key, _ := p.PartMetadata[providerOriginKey].(string)
		return (p.ExecutableCode != nil || p.CodeExecutionResult != nil) && key == o.key()
	}
	return false
}

// asClientCalls is cs as the model at o reads it (CA-82): the provider
// tool calls and results another model made become ordinary calls of the
// client tool of the same purpose, each paired with its result in a user
// turn of its own; the ones o made stay as they are; Gemini's own search
// parts, made from its grounding, are dropped for Gemini. It returns the
// tools the calls name, which the request must declare.
func asClientCalls(o Origin, cs []*genai.Content) ([]*genai.Content, map[string]bool) {
	if !slices.ContainsFunc(cs, func(c *genai.Content) bool { return c != nil && slices.ContainsFunc(c.Parts, isProviderPart) }) {
		return cs, nil
	}
	named := map[string]bool{}
	var out []*genai.Content
	for _, c := range cs {
		if c == nil || c.Role != genai.RoleModel || !slices.ContainsFunc(c.Parts, isProviderPart) {
			out = append(out, c)
			continue
		}
		cur := &genai.Content{Role: c.Role}
		flush := func() {
			if len(cur.Parts) > 0 {
				out = append(out, cur)
			}
			cur = &genai.Content{Role: c.Role}
		}
		for _, p := range c.Parts {
			switch {
			case !isProviderPart(p) || ownProviderPart(o, p):
				cur.Parts = append(cur.Parts, p)
			case o.API == "gemini" && p.PartMetadata[providerOriginKey] == o.key():
				// Gemini's own search, as its grounding: it doesn't take it back.
			case p.ToolCall != nil || p.ExecutableCode != nil:
				name, id, args := clientCall(p)
				named[name] = true
				cur.Parts = append(cur.Parts, &genai.Part{FunctionCall: &genai.FunctionCall{ID: id, Name: name, Args: args}})
			default:
				name, id, resp := clientResult(p)
				named[name] = true
				flush()
				out = append(out, &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: id, Name: name, Response: resp}}}})
			}
		}
		flush()
	}
	return out, named
}

// callIDKey (PartMetadata) pairs Gemini's code and its result, which
// carry no IDs of their own, by an ID of Blitz's.
const callIDKey = "blitz_call_id"

func kindOf(p *genai.Part) string {
	if k, ok := p.PartMetadata[providerToolKey].(string); ok {
		return k
	}
	return ptCodeExecution
}

func clientCall(p *genai.Part) (name, id string, args map[string]any) {
	kind := kindOf(p)
	name = providerToolName(kind)
	if p.ExecutableCode != nil {
		id, _ := p.PartMetadata[callIDKey].(string)
		return name, id, map[string]any{"tool": kind, "language": string(p.ExecutableCode.Language), "code": p.ExecutableCode.Code}
	}
	args = p.ToolCall.Args
	if name == "provider_tool" {
		args = map[string]any{"tool": kind, "input": p.ToolCall.Args}
	}
	return name, bzID(p.ToolCall.ID), args
}

func clientResult(p *genai.Part) (name, id string, resp map[string]any) {
	kind := kindOf(p)
	name = providerToolName(kind)
	if r := p.CodeExecutionResult; r != nil {
		id, _ := p.PartMetadata[callIDKey].(string)
		return name, id, map[string]any{"outcome": string(r.Outcome), "output": r.Output}
	}
	return name, bzID(p.ToolResponse.ID), p.ToolResponse.Response
}

// ProviderToolPart reads a stored part as a provider tool's call (call
// true) or result, as the client tool it stands for, for showing it as
// that tool's call and result; ok is false for any other part.
func ProviderToolPart(p *genai.Part) (name, id string, args map[string]any, call, ok bool) {
	if !isProviderPart(p) {
		return "", "", nil, false, false
	}
	if p.ToolCall != nil || p.ExecutableCode != nil {
		name, id, args = clientCall(p)
		return name, id, args, true, true
	}
	name, id, args = clientResult(p)
	return name, id, args, false, true
}

// bzID is a provider's call ID as one of Blitz's, which every API takes.
func bzID(id string) string {
	if strings.HasPrefix(id, bzPrefix) {
		return id
	}
	return bzPrefix + "p-" + id
}

// declareCalled returns cfg declaring each tool in names it doesn't: an
// API may check a history's calls against the tools declared (CA-82).
func declareCalled(cfg *genai.GenerateContentConfig, names map[string]bool) *genai.GenerateContentConfig {
	var missing []string
	for name := range names {
		if !declared(cfg, name) {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return cfg
	}
	slices.Sort(missing)
	cp := genai.GenerateContentConfig{}
	if cfg != nil {
		cp = *cfg
	}
	t := &genai.Tool{}
	for _, name := range missing {
		t.FunctionDeclarations = append(t.FunctionDeclarations, &genai.FunctionDeclaration{
			Name:                 name,
			Description:          fmt.Sprintf("Results of %s earlier in this conversation, kept for reference. Don't call it.", name),
			ParametersJsonSchema: map[string]any{"type": "object"},
		})
	}
	cp.Tools = append(slices.Clone(cp.Tools), t)
	return &cp
}

// Citation is one source an answer cites (CA-84).
type Citation struct {
	Title string `json:"title,omitempty"`
	URL   string `json:"url"`
	Text  string `json:"text,omitempty"`
}

// Citations is what resp cites.
func Citations(resp *model.LLMResponse) []Citation {
	if resp == nil {
		return nil
	}
	switch v := resp.CustomMetadata[CitationsKey].(type) {
	case []Citation:
		return v
	case []any:
		// Read back from a stored event.
		b, _ := json.Marshal(v)
		var out []Citation
		_ = json.Unmarshal(b, &out)
		return out
	}
	return nil
}

// addCitations records cs on resp, after any it has, once each.
func addCitations(resp *model.LLMResponse, cs []Citation) {
	if len(cs) == 0 {
		return
	}
	if resp.CustomMetadata == nil {
		resp.CustomMetadata = map[string]any{}
	}
	all := Citations(resp)
	for _, c := range cs {
		if c.URL != "" && !slices.Contains(all, c) {
			all = append(all, c)
		}
	}
	resp.CustomMetadata[CitationsKey] = all
}

// countRequests records n requests of kind on resp, for its price.
func countRequests(resp *model.LLMResponse, kind string, n int) {
	if n <= 0 {
		return
	}
	if resp.CustomMetadata == nil {
		resp.CustomMetadata = map[string]any{}
	}
	counts, _ := resp.CustomMetadata[ProviderToolRequestsKey].(map[string]int)
	if counts == nil {
		counts = map[string]int{}
	}
	counts[kind] += n
	resp.CustomMetadata[ProviderToolRequestsKey] = counts
}

// geminiProviderTools stores what Gemini's built-in tools did in resp
// (CA-80, CA-84): its search, from the grounding (the queries as the
// call, the sources as the result, and the citations), and its code
// execution, tagged with its origin.
func geminiProviderTools(o Origin, resp *model.LLMResponse) {
	if resp == nil || resp.Partial || resp.Content == nil {
		return
	}
	code, id := 0, ""
	for _, p := range resp.Content.Parts {
		if p != nil && (p.ExecutableCode != nil || p.CodeExecutionResult != nil) {
			if p.PartMetadata == nil {
				p.PartMetadata = map[string]any{}
			}
			if p.ExecutableCode != nil {
				code++
				id = bzPrefix + uuid.NewString()
			}
			p.PartMetadata[providerToolKey] = ptCodeExecution
			p.PartMetadata[providerOriginKey] = o.key()
			p.PartMetadata[callIDKey] = id
		}
	}
	countRequests(resp, ptCodeExecution, code)
	g := resp.GroundingMetadata
	if g == nil || len(g.WebSearchQueries) == 0 && len(g.GroundingChunks) == 0 {
		return
	}
	var sources []string
	var cites []Citation
	for _, ch := range g.GroundingChunks {
		if ch == nil || ch.Web == nil {
			continue
		}
		sources = append(sources, fmt.Sprintf("%s — %s", ch.Web.Title, ch.Web.URI))
	}
	for _, s := range g.GroundingSupports {
		if s == nil {
			continue
		}
		text := ""
		if s.Segment != nil {
			text = s.Segment.Text
		}
		for _, i := range s.GroundingChunkIndices {
			if int(i) < len(g.GroundingChunks) && g.GroundingChunks[i] != nil && g.GroundingChunks[i].Web != nil {
				w := g.GroundingChunks[i].Web
				cites = append(cites, Citation{Title: w.Title, URL: w.URI, Text: text})
			}
		}
	}
	if len(cites) == 0 {
		for _, ch := range g.GroundingChunks {
			if ch != nil && ch.Web != nil {
				cites = append(cites, Citation{Title: ch.Web.Title, URL: ch.Web.URI})
			}
		}
	}
	id = bzPrefix + uuid.NewString()
	call := providerCall(ptWebSearch, id, map[string]any{"query": strings.Join(g.WebSearchQueries, "; ")}, nil)
	result := providerResult(ptWebSearch, id, strings.Join(sources, "\n"), nil)
	for _, p := range []*genai.Part{call, result} {
		p.PartMetadata[providerOriginKey] = o.key()
	}
	resp.Content.Parts = append([]*genai.Part{call, result}, resp.Content.Parts...)
	addCitations(resp, cites)
	countRequests(resp, ptWebSearch, max(len(g.WebSearchQueries), 1))
}
