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
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"strings"

	"cloud.google.com/go/auth/oauth2adapt"
	"github.com/anthropics/anthropic-sdk-go"
	anthropicconfig "github.com/anthropics/anthropic-sdk-go/config"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
	"github.com/anthropics/anthropic-sdk-go/vertex"
	"github.com/retail-cortex/blitz/pkg/config"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// DefaultAnthropicModel is used when no model is configured for the provider.
const DefaultAnthropicModel = "claude-opus-5"

const (
	anthropicDefaultMaxTokens = 16000
	// redactedPrefix marks a redacted_thinking block stored in a genai
	// ThoughtSignature, so it can be sent back unchanged.
	redactedPrefix = "redacted:"
)

// anthropicModel adapts the Claude Messages API to the ADK model.LLM
// interface, translating genai contents, tools and usage both ways.
type anthropicModel struct {
	client    anthropic.Client
	name      string
	fallbacks string // "default", "off", or a model ID
}

// newAnthropicModel builds the adapter. With auth = "oauth" it uses an `ant
// auth login` profile (the one named, else ant's active one) and nothing
// from the environment, so a stray ANTHROPIC_API_KEY can't take its place.
// With auth = "adc" it runs on Vertex AI with Application Default
// Credentials, where server-side refusal fallback doesn't exist (so it's
// off). Otherwise, with no api_key the SDK resolves credentials itself
// (ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN, `ant auth login` profiles,
// workload identity).
func newAnthropicModel(ctx context.Context, cfg config.AnthropicConfig, name string, opts ...option.RequestOption) (*anthropicModel, error) {
	fb := cmp.Or(cfg.Fallbacks, "default")
	switch cfg.Auth {
	case "", config.AuthAPIKey:
		if cfg.APIKey != "" {
			opts = append(opts, option.WithAPIKey(cfg.APIKey))
		}
	case config.AuthOAuth:
		profile, err := anthropicProfile(cfg)
		if err != nil {
			return nil, err
		}
		opts = append(opts, option.WithoutEnvironmentDefaults(), option.WithConfig(profile))
	case config.AuthADC:
		vertexOpts, err := claudeOnVertex(ctx, cfg)
		if err != nil {
			return nil, err
		}
		opts = append(opts, vertexOpts...)
		fb = "off"
	default:
		return nil, fmt.Errorf("unknown [llm.anthropic] auth %q (api_key, oauth or adc)", cfg.Auth)
	}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	if name == "" {
		name = DefaultAnthropicModel
	}
	return &anthropicModel{client: anthropic.NewClient(opts...), name: name, fallbacks: fb}, nil
}

// claudeOnVertex are the client options for Claude on Vertex AI: the
// Application Default Credentials (found here, so that missing ones fail
// the model's build instead of panicking in the SDK's WithGoogleAuth),
// the project and location, and the quota project to bill. They come
// after the HTTP client, which the SDK then wraps rather than replaces.
func claudeOnVertex(ctx context.Context, cfg config.AnthropicConfig) ([]option.RequestOption, error) {
	project, location, err := vertexPlace("anthropic", cfg.ProjectID, cfg.Location)
	if err != nil {
		return nil, err
	}
	creds, err := googleCredentials()
	if err != nil {
		return nil, fmt.Errorf("claude on Vertex AI with Application Default Credentials: %w (sign in with `gcloud auth application-default login` on the machine running Blitz)", err)
	}
	opts := []option.RequestOption{vertex.WithCredentials(ctx, location, project, oauth2adapt.Oauth2CredentialsFromAuthCredentials(creds))}
	if quota, err := creds.QuotaProjectID(ctx); err == nil && quota != "" {
		opts = append(opts, option.WithHeader("X-Goog-User-Project", quota))
	}
	return opts, nil
}

// anthropicProfile loads the `ant auth login` profile Claude signs in with.
func anthropicProfile(cfg config.AnthropicConfig) (*anthropicconfig.Config, error) {
	dir, name := cfg.OAuthProfile()
	profile, err := anthropicconfig.LoadProfile(dir, name)
	if err != nil {
		return nil, fmt.Errorf("no Anthropic OAuth profile %q (sign in with `ant auth login`): %w", name, err)
	}
	return profile, nil
}

func (m *anthropicModel) Name() string { return m.name }

// GenerateContent sends one request. When streaming, text arrives as partial
// responses followed by one complete, non-partial response (the same shape
// the Gemini model produces), which is what the engine persists.
func (m *anthropicModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		params, err := m.buildParams(req, effortFrom(ctx))
		if err != nil {
			yield(nil, err)
			return
		}
		if !stream {
			msg, err := m.client.Beta.Messages.New(ctx, params)
			if err != nil {
				yield(nil, anthropicError(err))
				return
			}
			yield(messageToResponse(msg), nil)
			return
		}

		s := m.client.Beta.Messages.NewStreaming(ctx, params)
		defer s.Close()
		var msg anthropic.BetaMessage
		for s.Next() {
			ev := s.Current()
			if err := msg.Accumulate(ev); err != nil {
				yield(nil, fmt.Errorf("anthropic stream: %w", err))
				return
			}
			if delta, ok := ev.AsAny().(anthropic.BetaRawContentBlockDeltaEvent); ok {
				if td, ok := delta.Delta.AsAny().(anthropic.BetaTextDelta); ok && td.Text != "" {
					partial := &model.LLMResponse{Content: genai.NewContentFromText(td.Text, genai.RoleModel), Partial: true}
					if !yield(partial, nil) {
						return
					}
				}
			}
		}
		if err := s.Err(); err != nil {
			yield(nil, anthropicError(err))
			return
		}
		yield(messageToResponse(&msg), nil)
	}
}

// supportsSampling reports whether a model accepts temperature. Current
// models (Opus 5, Sonnet 5, Fable, Opus 4.7+) reject sampling parameters with
// a 400, so only older families receive them.
func supportsSampling(model string) bool {
	for _, p := range []string{"claude-haiku-4-5", "claude-sonnet-4-6", "claude-opus-4-6", "claude-sonnet-4-5", "claude-opus-4-5", "claude-opus-4-1", "claude-sonnet-4-", "claude-opus-4-0", "claude-3"} {
		if strings.HasPrefix(model, p) {
			return true
		}
	}
	return false
}

// supportsFallbacks reports whether server-side refusal fallback applies.
func supportsFallbacks(model string) bool {
	return strings.HasPrefix(model, "claude-opus-5") || strings.HasPrefix(model, "claude-fable-5")
}

// supportsEffort reports whether a model takes output_config.effort (the
// Claude 4.5 generation's Opus and later).
func supportsEffort(model string) bool {
	for _, p := range []string{"claude-opus-4-5", "claude-opus-4-6", "claude-sonnet-4-6", "claude-opus-4-7", "claude-opus-4-8"} {
		if strings.HasPrefix(model, p) {
			return true
		}
	}
	return !supportsSampling(model) && strings.HasPrefix(model, "claude-")
}

// supportsThinking reports whether a model has extended thinking (Claude
// 3.7 Sonnet and later).
func supportsThinking(model string) bool {
	for _, p := range []string{"claude-3-5", "claude-3-haiku", "claude-3-opus", "claude-3-sonnet"} {
		if strings.HasPrefix(model, p) {
			return false
		}
	}
	return strings.HasPrefix(model, "claude-")
}

// anthropicEfforts maps Blitz's efforts to Claude's (which have no minimal).
var anthropicEfforts = map[string]anthropic.BetaOutputConfigEffort{
	"minimal": anthropic.BetaOutputConfigEffortLow, "low": anthropic.BetaOutputConfigEffortLow,
	"medium": anthropic.BetaOutputConfigEffortMedium, "high": anthropic.BetaOutputConfigEffortHigh,
	"max": anthropic.BetaOutputConfigEffortMax,
}

// minThinkingBudget is the smallest budget Claude accepts.
const minThinkingBudget = 1024

func (m *anthropicModel) buildParams(req *model.LLMRequest, effort string) (anthropic.BetaMessageNewParams, error) {
	p := anthropic.BetaMessageNewParams{Model: m.name, MaxTokens: anthropicDefaultMaxTokens}
	if req.Model != "" {
		p.Model = req.Model
	}
	cfg := req.Config
	if cfg != nil {
		if cfg.MaxOutputTokens > 0 {
			p.MaxTokens = int64(cfg.MaxOutputTokens)
		}
		if cfg.Temperature != nil && supportsSampling(string(p.Model)) {
			p.Temperature = anthropic.Float(float64(*cfg.Temperature))
		}
		if sys := contentText(cfg.SystemInstruction); sys != "" {
			// The system prompt (plus the tools rendered before it) is large
			// and stable across turns: cache it.
			p.System = []anthropic.BetaTextBlockParam{{Text: sys, CacheControl: anthropic.NewBetaCacheControlEphemeralParam()}}
		}
		// Reasoning: the effort level, and a thinking budget (which must be
		// below max_tokens, and rules out temperature).
		if e, ok := anthropicEfforts[effort]; ok && supportsEffort(string(p.Model)) {
			p.OutputConfig.Effort = e
		}
		if tc := cfg.ThinkingConfig; tc != nil && tc.ThinkingBudget != nil && supportsThinking(string(p.Model)) {
			switch b := int64(*tc.ThinkingBudget); {
			case b == 0:
				p.Thinking = anthropic.BetaThinkingConfigParamUnion{OfDisabled: &anthropic.BetaThinkingConfigDisabledParam{}}
			case b > 0:
				b = max(b, minThinkingBudget)
				p.Thinking = anthropic.BetaThinkingConfigParamUnion{OfEnabled: &anthropic.BetaThinkingConfigEnabledParam{BudgetTokens: b}}
				if p.MaxTokens <= b {
					p.MaxTokens = b + anthropicDefaultMaxTokens
				}
				p.Temperature = param.Opt[float64]{}
			}
		}
		tools, err := convertTools(cfg.Tools)
		if err != nil {
			return p, err
		}
		p.Tools = tools
	}

	msgs, err := convertContents(req.Contents)
	if err != nil {
		return p, err
	}
	if len(msgs) == 0 {
		return p, errors.New("anthropic: request has no messages")
	}
	p.Messages = msgs

	switch fb := m.fallbacks; {
	case fb == "off" || !supportsFallbacks(string(p.Model)):
	case fb == "default":
		p.Betas = append(p.Betas, anthropic.AnthropicBetaServerSideFallback2026_07_01)
		p.Fallbacks = anthropic.BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()}
	default:
		p.Betas = append(p.Betas, anthropic.AnthropicBetaServerSideFallback2026_06_01)
		p.Fallbacks = anthropic.BetaFallbacksParamUnion{OfBetaFallbackArray: []anthropic.BetaFallbackParam{{Model: fb}}}
	}
	return p, nil
}

func contentText(c *genai.Content) string {
	if c == nil {
		return ""
	}
	var parts []string
	for _, p := range c.Parts {
		if p != nil && p.Text != "" && !p.Thought {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

// convertContents maps genai contents to Messages API turns. Consecutive
// contents with the same role are merged, which also keeps all parallel
// tool results in one user message as the API expects.
func convertContents(contents []*genai.Content) ([]anthropic.BetaMessageParam, error) {
	var out []anthropic.BetaMessageParam
	for _, c := range contents {
		if c == nil {
			continue
		}
		role := anthropic.BetaMessageParamRoleUser
		if c.Role == genai.RoleModel {
			role = anthropic.BetaMessageParamRoleAssistant
		}
		var blocks []anthropic.BetaContentBlockParamUnion
		for _, p := range c.Parts {
			if p == nil {
				continue
			}
			switch {
			case p.Thought:
				// Thinking blocks go back unchanged (signature intact) so tool
				// loops with thinking keep working; unsigned ones are dropped.
				if role != anthropic.BetaMessageParamRoleAssistant || len(p.ThoughtSignature) == 0 {
					continue
				}
				sig := string(p.ThoughtSignature)
				if strings.HasPrefix(sig, redactedPrefix) {
					blocks = append(blocks, anthropic.NewBetaRedactedThinkingBlock(strings.TrimPrefix(sig, redactedPrefix)))
				} else {
					blocks = append(blocks, anthropic.NewBetaThinkingBlock(sig, p.Text))
				}
			case p.FunctionCall != nil:
				args := p.FunctionCall.Args
				if args == nil {
					args = map[string]any{}
				}
				blocks = append(blocks, anthropic.NewBetaToolUseBlock(p.FunctionCall.ID, args, p.FunctionCall.Name))
			case p.FunctionResponse != nil:
				blocks = append(blocks, toolResult(p.FunctionResponse))
			case p.InlineData != nil:
				img, ok := imageSource(p.InlineData)
				if !ok {
					blocks = append(blocks, anthropic.NewBetaTextBlock(fmt.Sprintf("[%s attachment omitted: unsupported type]", p.InlineData.MIMEType)))
					continue
				}
				// An image right after a tool result belongs to that result
				// (view_image); elsewhere it is a plain image block.
				if n := len(blocks); n > 0 && blocks[n-1].OfToolResult != nil {
					tr := blocks[n-1].OfToolResult
					tr.Content = append(tr.Content, anthropic.BetaToolResultBlockParamContentUnion{OfImage: &anthropic.BetaImageBlockParam{
						Source: anthropic.BetaImageBlockParamSourceUnion{OfBase64: &img}}})
					continue
				}
				blocks = append(blocks, anthropic.NewBetaImageBlock(img))
			case p.Text != "":
				blocks = append(blocks, anthropic.NewBetaTextBlock(p.Text))
			}
		}
		if len(blocks) == 0 {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Role == role {
			out[n-1].Content = append(out[n-1].Content, blocks...)
			continue
		}
		out = append(out, anthropic.BetaMessageParam{Role: role, Content: blocks})
	}
	if len(out) > 0 && out[0].Role != anthropic.BetaMessageParamRoleUser {
		return nil, errors.New("anthropic: conversation must start with a user message")
	}
	return out, nil
}

// imageSource converts inline image bytes; other media types aren't
// accepted as images by the API.
func imageSource(b *genai.Blob) (anthropic.BetaBase64ImageSourceParam, bool) {
	mt := anthropic.BetaBase64ImageSourceMediaType(b.MIMEType)
	switch mt {
	case anthropic.BetaBase64ImageSourceMediaTypeImageJPEG, anthropic.BetaBase64ImageSourceMediaTypeImagePNG,
		anthropic.BetaBase64ImageSourceMediaTypeImageGIF, anthropic.BetaBase64ImageSourceMediaTypeImageWebP:
		return anthropic.BetaBase64ImageSourceParam{Data: base64.StdEncoding.EncodeToString(b.Data), MediaType: mt}, true
	}
	return anthropic.BetaBase64ImageSourceParam{}, false
}

// toolResult encodes an ADK function response. Tools report failures in an
// "error" field, which becomes an error tool_result so the model sees it.
func toolResult(fr *genai.FunctionResponse) anthropic.BetaContentBlockParamUnion {
	body, err := json.Marshal(fr.Response)
	if err != nil {
		body = []byte(fmt.Sprintf(`{"error":%q}`, err.Error()))
	}
	isErr := false
	if msg, ok := fr.Response["error"].(string); ok && msg != "" {
		isErr = true
	}
	return anthropic.NewBetaToolResultBlock(fr.ID, string(body), isErr)
}

// convertTools maps function declarations to tools with JSON Schema input.
func convertTools(tools []*genai.Tool) ([]anthropic.BetaToolUnionParam, error) {
	var out []anthropic.BetaToolUnionParam
	for _, t := range tools {
		if t == nil {
			continue
		}
		for _, fd := range t.FunctionDeclarations {
			if fd == nil {
				continue
			}
			schema, err := declarationSchema(fd)
			if err != nil {
				return nil, fmt.Errorf("tool %s: %w", fd.Name, err)
			}
			tp := anthropic.BetaToolParam{Name: fd.Name, InputSchema: toInputSchema(schema)}
			if fd.Description != "" {
				tp.Description = anthropic.String(fd.Description)
			}
			out = append(out, anthropic.BetaToolUnionParam{OfTool: &tp})
		}
	}
	return out, nil
}

// declarationSchema returns a JSON Schema object for a declaration, using
// the raw JSON schema when present and converting genai.Schema otherwise.
func declarationSchema(fd *genai.FunctionDeclaration) (map[string]any, error) {
	var src any = fd.ParametersJsonSchema
	if src == nil && fd.Parameters != nil {
		src = genaiSchemaToJSON(fd.Parameters)
	}
	if src == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}, nil
	}
	raw, err := json.Marshal(src)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("schema is not an object: %w", err)
	}
	return m, nil
}

func toInputSchema(m map[string]any) anthropic.BetaToolInputSchemaParam {
	s := anthropic.BetaToolInputSchemaParam{Properties: m["properties"], ExtraFields: map[string]any{}}
	if s.Properties == nil {
		s.Properties = map[string]any{}
	}
	if req, ok := m["required"].([]any); ok {
		for _, r := range req {
			if name, ok := r.(string); ok {
				s.Required = append(s.Required, name)
			}
		}
	}
	for k, v := range m {
		switch k {
		case "type", "properties", "required", "$schema":
		default:
			s.ExtraFields[k] = v
		}
	}
	return s
}

// genaiSchemaToJSON converts genai's Schema (upper-case OpenAPI types) to
// standard JSON Schema.
func genaiSchemaToJSON(s *genai.Schema) map[string]any {
	if s == nil {
		return nil
	}
	out := map[string]any{}
	if s.Type != "" {
		out["type"] = strings.ToLower(string(s.Type))
	}
	if s.Description != "" {
		out["description"] = s.Description
	}
	if len(s.Enum) > 0 {
		out["enum"] = s.Enum
	}
	if s.Format != "" {
		out["format"] = s.Format
	}
	if s.Items != nil {
		out["items"] = genaiSchemaToJSON(s.Items)
	}
	if len(s.Properties) > 0 {
		props := map[string]any{}
		for k, v := range s.Properties {
			props[k] = genaiSchemaToJSON(v)
		}
		out["properties"] = props
	}
	if len(s.Required) > 0 {
		out["required"] = s.Required
	}
	if s.Nullable != nil && *s.Nullable {
		if t, ok := out["type"].(string); ok {
			out["type"] = []string{t, "null"}
		}
	}
	return out
}

// messageToResponse converts a Messages API response to an ADK response.
func messageToResponse(msg *anthropic.BetaMessage) *model.LLMResponse {
	content := &genai.Content{Role: genai.RoleModel}
	for _, block := range msg.Content {
		switch b := block.AsAny().(type) {
		case anthropic.BetaTextBlock:
			if b.Text != "" {
				content.Parts = append(content.Parts, &genai.Part{Text: b.Text})
			}
		case anthropic.BetaThinkingBlock:
			content.Parts = append(content.Parts, &genai.Part{Text: b.Thinking, Thought: true, ThoughtSignature: []byte(b.Signature)})
		case anthropic.BetaRedactedThinkingBlock:
			content.Parts = append(content.Parts, &genai.Part{Thought: true, ThoughtSignature: []byte(redactedPrefix + b.Data)})
		case anthropic.BetaToolUseBlock:
			args := map[string]any{}
			if raw := b.JSON.Input.Raw(); raw != "" {
				_ = json.Unmarshal([]byte(raw), &args)
			}
			content.Parts = append(content.Parts, &genai.Part{FunctionCall: &genai.FunctionCall{ID: b.ID, Name: b.Name, Args: args}})
		}
	}

	resp := &model.LLMResponse{Content: content, TurnComplete: true, ModelVersion: string(msg.Model)}
	switch msg.StopReason {
	case anthropic.BetaStopReasonMaxTokens:
		resp.FinishReason = genai.FinishReasonMaxTokens
	case anthropic.BetaStopReasonRefusal:
		resp.FinishReason = genai.FinishReasonSafety
		note := "The model declined to continue this request."
		if cat := string(msg.StopDetails.Category); cat != "" {
			note += " (" + cat + ")"
		}
		resp.ErrorCode, resp.ErrorMessage = "refusal", note
		content.Parts = append(content.Parts, &genai.Part{Text: "\n\n" + note})
	default:
		resp.FinishReason = genai.FinishReasonStop
	}
	if len(content.Parts) == 0 {
		// The engine expects content on a final response.
		content.Parts = []*genai.Part{{Text: ""}}
	}

	u := msg.Usage
	prompt := u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
	resp.CustomMetadata = map[string]any{CacheWriteTokensKey: u.CacheCreationInputTokens}
	resp.UsageMetadata = &genai.GenerateContentResponseUsageMetadata{
		PromptTokenCount:        int32(prompt),
		CachedContentTokenCount: int32(u.CacheReadInputTokens),
		CandidatesTokenCount:    int32(u.OutputTokens),
		TotalTokenCount:         int32(prompt + u.OutputTokens),
	}
	return resp
}

// anthropicError adds the status code to API errors for clearer messages.
func anthropicError(err error) error {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 401, 403:
			return fmt.Errorf("anthropic: authentication failed (%d); check llm.anthropic.api_key or ANTHROPIC_API_KEY: %w", apiErr.StatusCode, err)
		case 429:
			if q := asQuota(err); q != err {
				return fmt.Errorf("anthropic: %w", q)
			}
			return fmt.Errorf("anthropic: rate limited (429): %w", err)
		default:
			return fmt.Errorf("anthropic: API error (%d): %w", apiErr.StatusCode, err)
		}
	}
	return fmt.Errorf("anthropic: %w", err)
}
