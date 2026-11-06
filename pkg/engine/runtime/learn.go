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
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/modelfacts"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// Learning a model's limits from its refusals (spec_056 CA-13–CA-15): we
// can't know every model's, so a call refused for something its record let
// through teaches the record, is sent again without it once, and the
// person is told once per model and fact. A refusal not recognised here
// stays an error.

// LearnedKey is the CustomMetadata key of what a call learned about its
// model, as lines for the person ([]string).
const LearnedKey = "blitz_learned"

// refusalPatterns recognise a refusal in each API's words (and the words
// the servers behind them use: Ollama, vLLM, gateways), giving the fact
// and, for the kinds that name one, the group holding its value.
var refusalPatterns = []struct {
	kind string
	re   *regexp.Regexp
}{
	{modelfacts.NoTools, regexp.MustCompile(`(?i)does not support (?:tools|tool use|function calling)|(?:tools?|tool use|function calling) (?:is|are) not (?:supported|enabled)`)},
	{modelfacts.NoImages, regexp.MustCompile(`(?i)does not support (?:image|vision)|(?:image|vision)(?: input)? (?:is |are )?not supported|image input modality is not enabled|model does not support images`)},
	{modelfacts.NoStream, regexp.MustCompile(`(?i)does not support streaming|stream(?:ing)? (?:is )?not supported|Unsupported (?:parameter|value): 'stream'`)},
	{modelfacts.NoSystem, regexp.MustCompile(`(?i)(?:system|developer) (?:messages?|prompts?|instructions?|role) (?:is|are) not (?:supported|enabled)`)},
	{modelfacts.NoStructured, regexp.MustCompile(`(?i)(?:response_?schema|json_schema|structured outputs?) (?:is |are )?not supported`)},
	{modelfacts.ContextWindow, regexp.MustCompile(`(?i)maximum context length is (\d+) tokens|prompt is too long: \d+ tokens > (\d+) maximum|exceeds the maximum number of tokens allowed \((\d+)\)|context (?:window|length) of (\d+) tokens`)},
	{modelfacts.NoParam, regexp.MustCompile(`(?i)Unsupported (?:parameter|value): '([a-z_.]+)'|Unknown name "([A-Za-z_]+)"|([a-z_]+): Extra inputs are not permitted|'?([a-z_]+)'? is not supported (?:with|for) this model`)},
}

// settingOf names the [model_settings] key a refused request field is
// (Gemini's camelCase and the APIs' nesting included); "" for a field no
// setting sends.
func settingOf(field string) string {
	switch f := strings.ToLower(field); {
	case f == "temperature":
		return "temperature"
	case f == "top_p" || f == "topp":
		return "top_p"
	case f == "top_k" || f == "topk":
		return "top_k"
	case f == "seed":
		return "seed"
	case strings.Contains(f, "reasoning") || strings.Contains(f, "thinking") || strings.Contains(f, "effort") || f == "output_config":
		return "thinking"
	}
	return ""
}

// recogniseRefusal is the fact err teaches about its model, if it's a
// refusal Blitz knows.
func recogniseRefusal(err error) (modelfacts.Fact, bool) {
	if err == nil {
		return modelfacts.Fact{}, false
	}
	msg := err.Error()
	for _, p := range refusalPatterns {
		m := p.re.FindStringSubmatch(msg)
		if m == nil {
			continue
		}
		value := ""
		for _, g := range m[1:] {
			if g != "" {
				value = g
				break
			}
		}
		f := modelfacts.Fact{Kind: p.kind, Why: oneLine(msg), At: time.Now()}
		switch p.kind {
		case modelfacts.ContextWindow:
			f.Value = value
		case modelfacts.NoParam:
			switch s := settingOf(value); s {
			case "":
				if value == "stream" {
					f.Kind = modelfacts.NoStream
					break
				}
				return modelfacts.Fact{}, false // a field no setting sends: the request is wrong, not the model
			case "thinking":
				f.Kind = modelfacts.NoThinking
			default:
				f.Value = s
			}
		}
		return f, true
	}
	return modelfacts.Fact{}, false
}

// oneLine is s on one line, cut to a sentence's length, for a fact's why.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 300 {
		s = string(r[:300]) + "…"
	}
	return s
}

// retryable reports whether sending the call again without what f says the
// model refuses can succeed (a context window can't be met by resending:
// the conversation is compacted first, CA-111).
func retryable(f modelfacts.Fact) bool {
	return f.Kind != modelfacts.ContextWindow && f.Kind != modelfacts.NoStructured
}

// learnedLine is what the person is told about f, once.
func learnedLine(model string, f modelfacts.Fact) string {
	switch f.Kind {
	case modelfacts.NoTools:
		return i18n.T("model.learned.no_tools", "model", model)
	case modelfacts.NoImages:
		return i18n.T("model.learned.no_images", "model", model)
	case modelfacts.NoStream:
		return i18n.T("model.learned.no_stream", "model", model)
	case modelfacts.NoParam:
		return i18n.T("model.learned.no_param", "model", model, "setting", f.Value)
	case modelfacts.NoThinking:
		return i18n.T("model.learned.no_thinking", "model", model)
	case modelfacts.NoSystem:
		return i18n.T("model.learned.no_system", "model", model)
	case modelfacts.ContextWindow:
		return i18n.T("model.learned.context", "model", model, "tokens", f.Value)
	}
	return i18n.T("model.learned.other", "model", model)
}

// learn records f for provider's model and reports the line to tell the
// person ("" when they were told already).
func learn(provider, model string, f modelfacts.Fact) string {
	fresh, _ := learnedStore().Learn(provider, model, f)
	key := modelfacts.Key(provider, model) + "\x00" + f.Kind + "\x00" + f.Value
	models.mu.Lock()
	told := models.told[key]
	models.told[key] = true
	models.mu.Unlock()
	if told && !fresh {
		return ""
	}
	return learnedLine(model, f)
}

// applyFacts is req, and whether to stream it, without what the model's
// record says it refuses: tools, images, settings, reasoning settings,
// system instructions, a stream. req is shared with a fallback chain's
// other members, so a changed one is a copy.
func applyFacts(info ModelInfo, req *model.LLMRequest, stream bool) (*model.LLMRequest, bool) {
	if req == nil {
		return req, stream
	}
	cp := *req
	changed := false
	if info.Tools == ToolsNone && offersFunctions(req) {
		cp.Config = withoutFunctions(cp.Config)
		cp.Contents = callsAsText(cp.Contents)
		changed = true
	}
	if !info.Vision && hasImages(req.Contents) {
		cp.Contents = imagesAsNotes(cp.Contents)
		changed = true
	}
	if cp.Config != nil {
		gc := *cp.Config
		touched := false
		for _, s := range info.Refused {
			switch s {
			case "temperature":
				touched = touched || gc.Temperature != nil
				gc.Temperature = nil
			case "top_p":
				touched = touched || gc.TopP != nil
				gc.TopP = nil
			case "top_k":
				touched = touched || gc.TopK != nil
				gc.TopK = nil
			case "seed":
				touched = touched || gc.Seed != nil
				gc.Seed = nil
			}
		}
		if info.Thinking == ThinkingNone && slices.ContainsFunc(info.Learned, func(f modelfacts.Fact) bool { return f.Kind == modelfacts.NoThinking }) && gc.ThinkingConfig != nil {
			gc.ThinkingConfig, touched = nil, true
		}
		if slices.ContainsFunc(info.Learned, func(f modelfacts.Fact) bool { return f.Kind == modelfacts.NoSystem }) && gc.SystemInstruction != nil {
			cp.Contents = foldSystem(gc.SystemInstruction, cp.Contents)
			gc.SystemInstruction, touched = nil, true
		}
		if touched {
			cp.Config, changed = &gc, true
		}
	}
	if !info.Streams {
		stream = false
	}
	if !changed {
		return req, stream
	}
	return &cp, stream
}

// withFact is info with f learned (for the call sent again at once).
func withFact(info ModelInfo, f modelfacts.Fact) ModelInfo {
	info.Learned = append(slices.Clone(info.Learned), f)
	switch f.Kind {
	case modelfacts.NoTools:
		info.Tools = ToolsNone
	case modelfacts.NoImages:
		info.Vision = false
	case modelfacts.NoStream:
		info.Streams = false
	case modelfacts.NoThinking:
		info.Thinking, info.Effort = ThinkingNone, false
	case modelfacts.NoParam:
		info.Refused = append(slices.Clone(info.Refused), f.Value)
	}
	return info
}

func offersFunctions(req *model.LLMRequest) bool {
	return req.Config != nil && slices.ContainsFunc(req.Config.Tools, func(t *genai.Tool) bool { return t != nil && len(t.FunctionDeclarations) > 0 })
}

// withoutFunctions is cfg without its function declarations (a copy);
// a tool that's more than functions keeps the rest.
func withoutFunctions(cfg *genai.GenerateContentConfig) *genai.GenerateContentConfig {
	cp := *cfg
	cp.Tools = nil
	for _, t := range cfg.Tools {
		if t == nil {
			continue
		}
		nt := *t
		nt.FunctionDeclarations = nil
		if !reflect.ValueOf(nt).IsZero() {
			cp.Tools = append(cp.Tools, &nt)
		}
	}
	cp.ToolConfig = nil
	return &cp
}

// callsAsText is cs with every tool call and result written as text, for a
// model that doesn't call tools.
func callsAsText(cs []*genai.Content) []*genai.Content {
	out := make([]*genai.Content, len(cs))
	for i, c := range cs {
		if c != nil && slices.ContainsFunc(c.Parts, func(p *genai.Part) bool { return p != nil && (p.FunctionCall != nil || p.FunctionResponse != nil) }) {
			out[i] = asText(c)
			continue
		}
		out[i] = c
	}
	return out
}

func isImage(p *genai.Part) bool {
	return p != nil && (p.InlineData != nil && strings.HasPrefix(p.InlineData.MIMEType, "image/") ||
		p.FileData != nil && strings.HasPrefix(p.FileData.MIMEType, "image/"))
}

func hasImages(cs []*genai.Content) bool {
	return slices.ContainsFunc(cs, func(c *genai.Content) bool { return c != nil && slices.ContainsFunc(c.Parts, isImage) })
}

// imagesAsNotes is cs with each image a note (CA-64).
func imagesAsNotes(cs []*genai.Content) []*genai.Content {
	out := make([]*genai.Content, len(cs))
	for i, c := range cs {
		if c == nil || !slices.ContainsFunc(c.Parts, isImage) {
			out[i] = c
			continue
		}
		nc := *c
		nc.Parts = make([]*genai.Part, len(c.Parts))
		for j, p := range c.Parts {
			if isImage(p) {
				nc.Parts[j] = &genai.Part{Text: i18n.T("model.image_note")}
				continue
			}
			nc.Parts[j] = p
		}
		out[i] = &nc
	}
	return out
}

// foldSystem puts the system instructions at the head of the first user
// turn, for a model that refuses them.
func foldSystem(sys *genai.Content, cs []*genai.Content) []*genai.Content {
	text := contentText(sys)
	if text == "" {
		return cs
	}
	out := slices.Clone(cs)
	for i, c := range out {
		if c != nil && c.Role == genai.RoleUser {
			nc := *c
			nc.Parts = append([]*genai.Part{{Text: text}}, c.Parts...)
			out[i] = &nc
			return out
		}
	}
	return append([]*genai.Content{{Role: genai.RoleUser, Parts: []*genai.Part{{Text: text}}}}, out...)
}

// tellLearned adds lines to resp's learned lines.
func tellLearned(resp *model.LLMResponse, lines []string) {
	if resp == nil || len(lines) == 0 {
		return
	}
	if resp.CustomMetadata == nil {
		resp.CustomMetadata = map[string]any{}
	}
	prev, _ := resp.CustomMetadata[LearnedKey].([]string)
	resp.CustomMetadata[LearnedKey] = append(prev, lines...)
}
