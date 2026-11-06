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
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// Where each part of a conversation came from (spec_056_conversation_adapters
// §3, §5). A conversation is stored as genai contents, which any model's
// adapter reads; what only its writer may read back (reasoning and its
// signatures) is tagged with its origin as it comes in, and each request
// keeps only the state its model wrote.

// OriginKey is the CustomMetadata key of the origin a model's answer
// records (CA-02).
const OriginKey = "blitz_origin"

// Origin is the model a part came from: the wire API it speaks, the
// provider as configured, and the model.
type Origin struct {
	API, Provider, Model string
}

func (o Origin) key() string { return o.API + "|" + o.Provider + "|" + o.Model }

// stateTag starts a tagged ThoughtSignature: the tag, the origin's key, a
// NUL, then the provider's own signature (empty for unsigned reasoning).
const stateTag = "blitz-state:"

func tagState(o Origin, sig []byte) []byte {
	if bytes.HasPrefix(sig, []byte(stateTag)) {
		return sig // already tagged (a partial reply's part, seen again)
	}
	return append([]byte(stateTag+o.key()+"\x00"), sig...)
}

// untagState splits a tagged signature into its origin's key and the
// provider's signature; ok is false for an untagged one (written before
// this spec: CA-06).
func untagState(sig []byte) (key string, payload []byte, ok bool) {
	rest, found := bytes.CutPrefix(sig, []byte(stateTag))
	if !found {
		return "", nil, false
	}
	k, payload, found := bytes.Cut(rest, []byte{0})
	if !found {
		return "", nil, false
	}
	return string(k), payload, true
}

// originOf is the origin resp records, if any.
func originOf(resp *model.LLMResponse) (Origin, bool) {
	if resp == nil {
		return Origin{}, false
	}
	m, ok := resp.CustomMetadata[OriginKey].(map[string]any)
	if !ok {
		return Origin{}, false
	}
	str := func(k string) string { s, _ := m[k].(string); return s }
	return Origin{API: str("api"), Provider: str("provider"), Model: str("model")}, true
}

// isFallback reports whether a fallback model gave resp.
func isFallback(resp *model.LLMResponse) bool {
	_, ok := resp.CustomMetadata[FallbackFromKey]
	return ok
}

// bzPrefix starts the IDs Blitz gives tool calls (CA-03): ADK blanks only
// its own (adk-…) before each call, so these survive to every adapter.
const bzPrefix = "bz-"

// stampResponse tags the provider state in resp with o, records o as the
// answer's origin, and gives each tool call without an ID one of Blitz's.
func stampResponse(o Origin, resp *model.LLMResponse) {
	if resp == nil {
		return
	}
	if resp.CustomMetadata == nil {
		resp.CustomMetadata = map[string]any{}
	}
	if _, set := resp.CustomMetadata[OriginKey]; !set {
		resp.CustomMetadata[OriginKey] = map[string]any{"api": o.API, "provider": o.Provider, "model": o.Model}
	}
	if u := resp.UsageMetadata; u != nil && o.API == "openai" && u.ThoughtsTokenCount > 0 {
		// OpenAI's output tokens include its reasoning; Gemini's candidates,
		// the form usage is kept in, don't (CA-122).
		u.CandidatesTokenCount = max(u.CandidatesTokenCount-u.ThoughtsTokenCount, 0)
	}
	if resp.Content == nil {
		return
	}
	for _, p := range resp.Content.Parts {
		if p == nil {
			continue
		}
		if p.Thought || len(p.ThoughtSignature) > 0 {
			p.ThoughtSignature = tagState(o, p.ThoughtSignature)
		}
		if fc := p.FunctionCall; fc != nil && fc.ID == "" && !resp.Partial {
			fc.ID = bzPrefix + uuid.NewString()
		}
	}
}

// adaptRequest returns req as the model at o may read it (CA-20–CA-23):
// reasoning and signatures only from o itself, restored to the provider's
// own; tool calls and their results paired by IDs the API takes; and, for
// Anthropic, a conversation that opens with the user. req is shared with a
// fallback chain's other members, so a changed one is a copy.
func adaptRequest(o Origin, req *model.LLMRequest) *model.LLMRequest {
	if req == nil {
		return nil
	}
	cp := *req
	// ADK names the agent's model on every request, which for a fallback
	// chain is its primary: each member asks for itself (MDL-60).
	cp.Model = o.Model
	cp.Config = wireTools(req.Config)
	cp.Contents = make([]*genai.Content, 0, len(req.Contents))
	var pending []*genai.FunctionCall // calls of the previous content without an ID
	legacy := 0
	for _, c := range req.Contents {
		if c == nil {
			continue
		}
		nc := *c
		nc.Parts = make([]*genai.Part, 0, len(c.Parts))
		var calls []*genai.FunctionCall
		for _, p := range c.Parts {
			if p == nil {
				continue
			}
			np := *p
			if p.Thought || len(p.ThoughtSignature) > 0 {
				key, payload, ok := untagState(p.ThoughtSignature)
				if ok && key == o.key() {
					np.ThoughtSignature = payload
				} else {
					np.ThoughtSignature = nil
					if p.Thought {
						continue // another model's reasoning
					}
				}
				if len(np.ThoughtSignature) == 0 {
					np.ThoughtSignature = nil
				}
			}
			if fc := p.FunctionCall; fc != nil {
				call := *fc
				call.Name = wireName(call.Name)
				switch {
				case o.API == "gemini" && strings.HasPrefix(call.ID, bzPrefix):
					call.ID = "" // Gemini pairs by position; it gave these calls no ID
				case o.API != "gemini" && call.ID == "":
					legacy++
					call.ID = bzPrefix + "legacy-" + strconv.Itoa(legacy)
					calls = append(calls, &call)
				}
				np.FunctionCall = &call
			}
			if fr := p.FunctionResponse; fr != nil {
				resp := *fr
				resp.Name = wireName(resp.Name)
				switch {
				case o.API == "gemini" && strings.HasPrefix(resp.ID, bzPrefix):
					resp.ID = ""
				case o.API != "gemini" && resp.ID == "":
					resp.ID = takeCall(&pending, resp.Name)
				}
				np.FunctionResponse = &resp
			}
			if reflect.ValueOf(np).IsZero() {
				continue
			}
			nc.Parts = append(nc.Parts, &np)
		}
		pending = calls
		if len(nc.Parts) > 0 {
			cp.Contents = append(cp.Contents, &nc)
		}
	}
	if o.API == "anthropic" && len(cp.Contents) > 0 && cp.Contents[0].Role != genai.RoleUser {
		cp.Contents = append([]*genai.Content{summaryAsUser(cp.Contents[0])}, cp.Contents[1:]...)
	}
	cp.Contents = placeMedia(o, cp.Contents)
	switch o.API {
	case "gemini":
		signCurrentCalls(cp.Contents)
	case "anthropic":
		cp.Contents = flattenForeignLoop(cp.Contents)
	}
	return &cp
}

// wireTool is the pattern a tool's name matches for every API Blitz
// speaks: Anthropic's and OpenAI's (letters, digits, _ and -, at most 64)
// and Gemini's, which also wants a letter or _ first.
var wireTool = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]{0,63}$`)

// wireName is name as every API takes it (CA-26): as it is when it can
// be, else its other characters as _, cut to fit, with a hash of the name
// so two names never map to one. MCP servers may name tools with dots or
// at length.
func wireName(name string) string {
	if name == "" || wireTool.MatchString(name) {
		return name
	}
	var b strings.Builder
	for i, r := range name {
		switch {
		case r == '_' || r == '-' && i > 0 || r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) && i > 0):
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	sum := sha256.Sum256([]byte(name))
	safe := b.String()
	if len(safe) > 55 {
		safe = safe[:55]
	}
	return safe + "_" + hex.EncodeToString(sum[:4])
}

// wireTools is cfg with its tools named as every API takes them; cfg
// itself is shared, so a changed one is a copy.
func wireTools(cfg *genai.GenerateContentConfig) *genai.GenerateContentConfig {
	if cfg == nil || !slices.ContainsFunc(cfg.Tools, func(t *genai.Tool) bool {
		return t != nil && slices.ContainsFunc(t.FunctionDeclarations, func(d *genai.FunctionDeclaration) bool { return d != nil && wireName(d.Name) != d.Name })
	}) {
		return cfg
	}
	cp := *cfg
	cp.Tools = make([]*genai.Tool, len(cfg.Tools))
	for i, t := range cfg.Tools {
		if t == nil {
			continue
		}
		nt := *t
		nt.FunctionDeclarations = make([]*genai.FunctionDeclaration, len(t.FunctionDeclarations))
		for j, d := range t.FunctionDeclarations {
			if d == nil {
				continue
			}
			nd := *d
			nd.Name = wireName(d.Name)
			nt.FunctionDeclarations[j] = &nd
		}
		cp.Tools[i] = &nt
	}
	return &cp
}

// namesBack maps each tool name of req the API was sent changed back to
// the tool's own (nil when none was changed).
func namesBack(req *model.LLMRequest) map[string]string {
	if req == nil || req.Config == nil {
		return nil
	}
	var back map[string]string
	for _, t := range req.Config.Tools {
		if t == nil {
			continue
		}
		for _, d := range t.FunctionDeclarations {
			if d != nil && wireName(d.Name) != d.Name {
				if back == nil {
					back = map[string]string{}
				}
				back[wireName(d.Name)] = d.Name
			}
		}
	}
	return back
}

// nameCallsBack gives resp's tool calls their tools' own names.
func nameCallsBack(resp *model.LLMResponse, back map[string]string) {
	if resp == nil || resp.Content == nil || back == nil {
		return
	}
	for _, p := range resp.Content.Parts {
		if p != nil && p.FunctionCall != nil {
			if own, ok := back[p.FunctionCall.Name]; ok {
				p.FunctionCall.Name = own
			}
		}
	}
}

// currentTurn is where the turn under way starts: the last user content
// with text (tool results are the user's too, but go on a turn).
func currentTurn(cs []*genai.Content) int {
	for i := len(cs) - 1; i >= 0; i-- {
		if cs[i].Role != genai.RoleUser {
			continue
		}
		for _, p := range cs[i].Parts {
			if p.Text != "" {
				return i
			}
		}
	}
	return 0
}

// geminiSkipSignature is the signature Gemini documents for function calls
// it didn't make (another model's, before a fallback took over), in the
// turn under way, where it otherwise requires its own (CA-91).
const geminiSkipSignature = "skip_thought_signature_validator"

// signCurrentCalls gives each of the current turn's steps whose first call
// has no signature Gemini's placeholder, in place (contents are copies).
func signCurrentCalls(cs []*genai.Content) {
	for _, c := range cs[currentTurn(cs):] {
		if c.Role != genai.RoleModel {
			continue
		}
		for _, p := range c.Parts {
			if p.FunctionCall != nil {
				if len(p.ThoughtSignature) == 0 {
					p.ThoughtSignature = []byte(geminiSkipSignature)
				}
				break
			}
		}
	}
}

// flattenForeignLoop is cs for Claude when the turn under way goes on
// from another model's tool calls (a fallback took over, CA-91): with
// thinking on, Claude needs its own thinking at the head of the assistant
// message a tool loop goes on from, which another model's calls don't
// have, and a model that always thinks can't have it turned off. The other
// model's calls and their results go as text instead.
func flattenForeignLoop(cs []*genai.Content) []*genai.Content {
	last := len(cs) - 1
	for last >= 0 && cs[last].Role != genai.RoleModel {
		last--
	}
	if last < currentTurn(cs) || !hasCalls(cs[last]) || hasThought(cs[last]) {
		return cs
	}
	out := slices.Clone(cs)
	for i := currentTurn(cs); i < len(out); i++ {
		c := out[i]
		if c.Role != genai.RoleModel || !hasCalls(c) || hasThought(c) {
			continue
		}
		out[i] = asText(c)
		if i+1 < len(out) && out[i+1].Role == genai.RoleUser {
			out[i+1] = asText(out[i+1])
		}
	}
	return out
}

func hasCalls(c *genai.Content) bool {
	return slices.ContainsFunc(c.Parts, func(p *genai.Part) bool { return p.FunctionCall != nil })
}

func hasThought(c *genai.Content) bool {
	return slices.ContainsFunc(c.Parts, func(p *genai.Part) bool { return p.Thought })
}

// asText is c with its tool calls and results written out as text.
func asText(c *genai.Content) *genai.Content {
	out := &genai.Content{Role: c.Role}
	for _, p := range c.Parts {
		switch {
		case p.FunctionCall != nil:
			args, _ := json.Marshal(p.FunctionCall.Args)
			out.Parts = append(out.Parts, &genai.Part{Text: fmt.Sprintf("[Called %s with %s]", p.FunctionCall.Name, args)})
		case p.FunctionResponse != nil:
			res, _ := json.Marshal(p.FunctionResponse.Response)
			out.Parts = append(out.Parts, &genai.Part{Text: fmt.Sprintf("[%s returned %s]", p.FunctionResponse.Name, res)})
		default:
			out.Parts = append(out.Parts, p)
		}
	}
	return out
}

// signedState reports whether sig carries a provider's signature (not
// only an origin): reasoning a provider signed, which mustn't be edited.
func signedState(sig []byte) bool {
	if _, payload, ok := untagState(sig); ok {
		return len(payload) > 0
	}
	return len(sig) > 0
}

// takeCall returns the ID of the first pending call of name, removing it.
func takeCall(pending *[]*genai.FunctionCall, name string) string {
	for i, c := range *pending {
		if c.Name == name {
			*pending = append((*pending)[:i:i], (*pending)[i+1:]...)
			return c.ID
		}
	}
	return ""
}

// summaryAsUser is a conversation's opening turn of the model's (a
// compaction summary, CA-04) as context the user gives.
func summaryAsUser(c *genai.Content) *genai.Content {
	out := &genai.Content{Role: genai.RoleUser}
	for _, p := range c.Parts {
		if p != nil && p.Text != "" && !p.Thought {
			out.Parts = append(out.Parts, &genai.Part{Text: "[Earlier in this conversation]\n" + p.Text})
		}
	}
	if len(out.Parts) == 0 {
		out.Parts = []*genai.Part{{Text: "[Earlier in this conversation]"}}
	}
	return out
}
