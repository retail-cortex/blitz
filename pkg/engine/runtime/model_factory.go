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
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"

	"cloud.google.com/go/auth"
	"cloud.google.com/go/auth/credentials"
	"cloud.google.com/go/auth/httptransport"
	"github.com/openai/openai-go/v3/option"
	"github.com/retail-cortex/blitz/pkg/config"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/adk/v2/model/openaimodel"
	"google.golang.org/genai"
)

// NewModel builds an ADK model.LLM based on configuration.
//
// The model (overrideModel, else the configured one) may name its provider,
// "anthropic/claude-sonnet-5", to use a provider other than llm.provider.
// Bare fallback names use llm.provider.
func NewModel(ctx context.Context, cfg *config.Config, overrideModel string) (model.LLM, error) {
	ref := cfg.ModelName()
	if overrideModel != "" {
		ref = overrideModel
	}
	provider := defaultProvider(cfg)
	p, modelName := ParseModelRef(ref, provider)
	primary, err := newProviderModel(ctx, cfg, p, modelName)
	if err != nil || len(cfg.LLM.FallbackModels) == 0 {
		return primary, err
	}
	chain := []model.LLM{primary}
	for _, ref := range cfg.LLM.FallbackModels {
		p, name := ParseModelRef(ref, provider)
		m, err := newProviderModel(ctx, cfg, p, name)
		if err != nil {
			// Usually missing credentials; `blitz doctor` lists each fallback.
			slog.WarnContext(ctx, "fallback model unavailable", "model", ref, "error", err)
			continue
		}
		chain = append(chain, m)
	}
	return newFallbackModel(chain), nil
}

// NewModelRef builds the single model a fallback reference names (no
// chain), e.g. for `doctor` to check each fallback on its own.
func NewModelRef(ctx context.Context, cfg *config.Config, ref string) (model.LLM, error) {
	p, name := ParseModelRef(ref, defaultProvider(cfg))
	return newProviderModel(ctx, cfg, p, name)
}

// defaultProvider is the provider a bare model name is: the
// organization's while Blitz is connected to one, else llm.provider.
func defaultProvider(cfg *config.Config) string {
	if cfg.Enterprise.URL != "" {
		return "enterprise"
	}
	return strings.ToLower(cfg.LLM.Provider)
}

// knownProviders are the provider prefixes recognised in model
// references: the settings' (config.Providers) and an organization's.
var knownProviders = func() map[string]bool {
	m := map[string]bool{"enterprise": true}
	for _, p := range config.Providers {
		m[p] = true
	}
	return m
}()

// ParseModelRef splits "provider/model" into its parts. Without a known
// provider prefix the whole reference is a model of defaultProvider, so
// OpenRouter-style names ("anthropic/claude-…" on the openai provider) need
// the provider spelled out: "openai/anthropic/claude-…".
func ParseModelRef(ref, defaultProvider string) (provider, name string) {
	ref = strings.TrimSpace(ref)
	provider, name = strings.ToLower(defaultProvider), ref
	if p, rest, ok := strings.Cut(ref, "/"); ok && knownProviders[strings.ToLower(p)] && rest != "" {
		provider, name = strings.ToLower(p), rest
	}
	if lowercaseNames[provider] {
		name = strings.ToLower(name) // "Gemini-3.8-flash" is a 400 from the API
	}
	return provider, name
}

// lowercaseNames are the providers whose model names are always lowercase,
// so a name typed with capitals is lowered rather than refused. Others
// (Ollama tags, OpenAI-compatible servers, Azure deployments, Bedrock IDs)
// may differ by case and are kept as written.
var lowercaseNames = map[string]bool{"gemini": true, "anthropic": true, "vertex-anthropic": true}

const (
	defaultOpenAIBaseURL = "https://api.openai.com/v1"
	defaultOllamaBaseURL = "http://localhost:11434/v1"
)

// openAICompatEndpoint returns the key and base URL for the openai and
// ollama providers, which share the [llm.openai] section. Its defaults point
// at OpenAI, so Ollama uses localhost unless another base_url was set, and
// never receives the OpenAI key.
func openAICompatEndpoint(c config.OpenAIConfig, provider string) (apiKey, baseURL string) {
	apiKey, baseURL = c.APIKey, c.BaseURL
	if provider == "ollama" {
		if baseURL == "" || baseURL == defaultOpenAIBaseURL {
			baseURL = defaultOllamaBaseURL
		}
		apiKey = "ollama"
	}
	if apiKey == "" {
		apiKey = "ollama"
	}
	return apiKey, baseURL
}

// openAIKey is the openai or ollama provider's key, address and client
// options: with api_key_command, each request carries the command's
// current key. Ollama's own base_url wins over [llm.openai]'s.
func openAIKey(ctx context.Context, llm config.LLMConfig, provider string, pol retryPolicy) (string, string, []option.RequestOption, error) {
	c := llm.OpenAI
	if provider == "ollama" && llm.Ollama.BaseURL != "" {
		c.BaseURL = llm.Ollama.BaseURL
	}
	apiKey, baseURL := openAICompatEndpoint(c, provider)
	opts := pol.openAIOptions()
	if provider != "openai" || c.APIKey != "" || c.APIKeyCommand == "" {
		return apiKey, baseURL, opts, nil
	}
	k, key, err := commandKeyNow(ctx, c.APIKeyCommand, c.APIKeyTTL)
	if err != nil {
		return "", "", nil, fmt.Errorf("[llm.openai] %w", err)
	}
	opts = append(opts, option.WithMiddleware(func(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		key, err := k.get(req.Context())
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+key)
		return next(req)
	}))
	return key, baseURL, opts, nil
}

// newProviderModel builds one model of the given provider, applying its
// [model_settings] on each call.
func newProviderModel(ctx context.Context, cfg *config.Config, provider, modelName string) (model.LLM, error) {
	if cfg.Enterprise.URL != "" && !(cfg.Org.Policy.Models.OwnProviders && provider != "") {
		// Connected to an organization, its models are the models
		// (spec_053_enterprise EN-20), unless its policy lets the person
		// name one of their own (EN-35).
		provider = "enterprise"
	}
	m, err := buildProviderModel(ctx, cfg, provider, modelName)
	if err != nil {
		return nil, err
	}
	// The settings' max_tokens is applied first, then capped at what the
	// model turns out to allow (outputcap.go).
	capped := &outputCapModel{inner: &streamingModel{inner: m}, provider: cmp.Or(provider, providerOf(m))}
	_, vertexAI := m.(onVertexAI)
	return &settingsModel{inner: capped, provider: providerOf(m), configured: provider, vertexAI: vertexAI}, nil
}

// onVertexAI is a Gemini model [llm.gemini] reaches through Vertex AI
// (Google Cloud's credentials, no key): the API it's on, not the
// provider's name, says which request fields it takes.
type onVertexAI struct{ model.LLM }

// providerOf names the API a built model talks to; with provider "" the
// choice was made from the credentials present.
func providerOf(m model.LLM) string {
	switch m := m.(type) {
	case *anthropicModel:
		return "anthropic"
	case *apiModel:
		// The settings the API the gateway's model speaks takes (MDL-107).
		switch {
		case m.p.format == formatMessages:
			return "anthropic"
		case m.p.format.google():
			return "gemini"
		}
		return "openai"
	case *toolCallParsingModel, *ollamaAuto:
		return "openai"
	}
	return "gemini"
}

func buildProviderModel(ctx context.Context, cfg *config.Config, provider, modelName string) (model.LLM, error) {
	pol := policyFrom(cfg.LLM)
	switch provider {
	case "gemini":
		clientCfg, err := geminiClientConfig(ctx, cfg.LLM.Gemini, pol)
		if err != nil {
			return nil, err
		}
		m, err := gemini.NewModel(ctx, modelName, clientCfg)
		if err != nil || clientCfg.Backend != genai.BackendVertexAI {
			return m, err
		}
		return onVertexAI{m}, nil

	case "openai", "ollama":
		apiKey, baseURL, opts, err := openAIKey(ctx, cfg.LLM, provider, pol)
		if err != nil {
			return nil, err
		}
		if provider == "ollama" && modelName == "" {
			return newOllamaAuto(apiKey, baseURL, opts, !cfg.LLM.Ollama.AllTools), nil
		}
		m, err := newOpenAIModel(ctx, modelName, apiKey, baseURL, opts...)
		if t, ok := m.(*toolCallParsingModel); ok {
			t.provider, t.key = provider, apiKey
			t.coreTools = provider == "ollama" && !cfg.LLM.Ollama.AllTools
		}
		if err == nil && provider == "ollama" {
			go listOllama(baseURL, modelName) // what Ollama says of it (spec_056 CA-11)
		}
		return m, err

	case "anthropic":
		return newAnthropicModel(ctx, cfg.LLM.Anthropic, modelName, pol.anthropicOptions()...)

	case "vertex-anthropic": // Claude on Vertex AI: [llm.anthropic] with adc
		a := cfg.LLM.Anthropic
		a.Auth = config.AuthADC
		return newAnthropicModel(ctx, a, modelName, pol.anthropicOptions()...)

	case "bedrock":
		return newBedrockModel(ctx, cfg.LLM.Bedrock, modelName, pol.anthropicOptions()...)

	case "azure":
		return newAzureModel(ctx, cfg.LLM.Azure, modelName, pol)

	case "api":
		return newAPIModel(ctx, cfg.LLM.API, modelName, pol)

	case "enterprise":
		return newEnterpriseModel(ctx, cfg, modelName, pol)

	case "":
		// If Gemini key is set and provider is empty, try gemini, else fallback to openai/ollama
		if cfg.LLM.Gemini.APIKey != "" {
			gc := pol.geminiConfig(cfg.LLM.Gemini.APIKey)
			gc.Backend = genai.BackendGeminiAPI
			return gemini.NewModel(ctx, modelName, gc)
		}
		if cfg.LLM.Anthropic.APIKey != "" {
			if cfg.Blitz.DefaultModel == "" {
				modelName = cfg.LLM.Anthropic.Model
			}
			return newAnthropicModel(ctx, cfg.LLM.Anthropic, modelName, pol.anthropicOptions()...)
		}
		if cfg.LLM.OpenAI.APIKey != "" || cfg.LLM.OpenAI.BaseURL != "" {
			apiKey := cfg.LLM.OpenAI.APIKey
			if apiKey == "" {
				apiKey = "ollama"
			}
			return newOpenAIModel(ctx, modelName, apiKey, cfg.LLM.OpenAI.BaseURL, pol.openAIOptions()...)
		}
		// Default to local Ollama if available
		return newOpenAIModel(ctx, modelName, "ollama", defaultOllamaBaseURL, pol.openAIOptions()...)

	default:
		// A misspelt provider is that, whatever keys are set: never
		// another provider's model under its name.
		return nil, errUnknownProvider(provider)
	}
}

// googleCredentials finds Application Default Credentials: a service
// account's key file (GOOGLE_APPLICATION_CREDENTIALS), gcloud's
// application-default login, or a Google Cloud machine's metadata server.
// A variable so that tests can supply their own.
var googleCredentials = func() (*auth.Credentials, error) {
	return credentials.DetectDefault(&credentials.DetectOptions{Scopes: []string{"https://www.googleapis.com/auth/cloud-platform"}})
}

// vertexPlace is the Vertex AI project and location for provider's
// settings: as set, else GOOGLE_CLOUD_PROJECT and GOOGLE_CLOUD_LOCATION,
// the location "global" by default. A project is required.
func vertexPlace(provider, project, location string) (string, string, error) {
	project = cmp.Or(project, os.Getenv("GOOGLE_CLOUD_PROJECT"))
	if project == "" {
		return "", "", fmt.Errorf("%s on Vertex AI with Application Default Credentials needs a Google Cloud project: set [llm.%s] project_id or GOOGLE_CLOUD_PROJECT", provider, provider)
	}
	return project, cmp.Or(location, os.Getenv("GOOGLE_CLOUD_LOCATION"), "global"), nil
}

// geminiClientConfig is the genai client for Gemini's settings, with one
// credential, the first there is: an API key (set, from its command, or
// GEMINI_API_KEY), alone, to the Gemini API; else Google Cloud's
// credentials, to Vertex AI: Application Default Credentials (a service
// account's key file, GOOGLE_APPLICATION_CREDENTIALS), else the Google
// account signed in with gcloud (OAuth), else, with auth = "adc", a Google
// Cloud machine's.
// The auth setting doesn't change the order: a key always comes first. An
// unknown auth is an error. genai signs requests itself only on an HTTP
// client of its own; ours (retries, stalls) gets the credentials here, on
// a copy, so no other model's requests carry them. Credentials that can't
// be found fail here, when the model is built, rather than at the first
// request.
func geminiClientConfig(ctx context.Context, g config.GeminiConfig, pol retryPolicy) (*genai.ClientConfig, error) {
	switch g.Auth {
	case "", config.AuthAPIKey, config.AuthADC, config.AuthOAuth:
	default:
		return nil, fmt.Errorf("unknown [llm.gemini] auth %q (api_key, adc or oauth)", g.Auth)
	}
	cc := pol.geminiConfig(g.APIKey)
	if g.APIKey == "" && g.APIKeyCommand != "" {
		k, key, err := commandKeyNow(ctx, g.APIKeyCommand, g.APIKeyTTL)
		if err != nil {
			return nil, fmt.Errorf("[llm.gemini] %w", err)
		}
		cc.APIKey = key
		cc.HTTPClient = withKeyCommand(cc.HTTPClient, k, "x-goog-api-key", false)
	}
	if cc.APIKey == "" { // set after the settings were read
		cc.APIKey = cmp.Or(os.Getenv("GEMINI_API_KEY"), os.Getenv("GOOGLE_API_KEY"))
	}
	if cc.APIKey != "" {
		// The Gemini API refuses a project and location, which are
		// Vertex AI's (left in the settings when switching from adc).
		cc.Backend = genai.BackendGeminiAPI
		return cc, nil
	}

	// Google Cloud only when there's a sign of it: a credentials file, or
	// auth asking (a Google Cloud machine's own are found by asking its
	// metadata server, which elsewhere only waits).
	if !g.UsesADC() && config.ADCFile() == "" {
		return nil, errors.New("gemini needs an API key, or Google Cloud credentials (sign in with `gcloud auth application-default login` on the machine running Blitz, or set [llm.gemini] auth = \"adc\")")
	}
	creds, err := googleCredentials()
	if err != nil {
		return nil, fmt.Errorf("gemini needs an API key or Google Cloud credentials: %w (set a key, or sign in with `gcloud auth application-default login` on the machine running Blitz)", err)
	}
	if g.ProjectID == "" && os.Getenv("GOOGLE_CLOUD_PROJECT") == "" {
		// A service account's key file names its project.
		if p, err := creds.ProjectID(ctx); err == nil {
			g.ProjectID = p
		}
	}
	project, location, err := vertexPlace("gemini", g.ProjectID, g.Location)
	if err != nil {
		return nil, err
	}
	cc.Backend = genai.BackendVertexAI
	cc.Project, cc.Location = project, location
	signed := *cc.HTTPClient // the models' shared client stays unsigned
	cc.HTTPClient = &signed
	if err := httptransport.AddAuthorizationMiddleware(cc.HTTPClient, creds); err != nil {
		return nil, fmt.Errorf("gemini with Google Cloud credentials: %w", err)
	}
	cc.Credentials = creds
	if quota, err := creds.QuotaProjectID(ctx); err == nil && quota != "" {
		cc.HTTPOptions.Headers = http.Header{"X-Goog-User-Project": []string{quota}}
	}
	return cc, nil
}

// MockLLM provides an in-memory LLM implementation for tests and offline validation.
// It is safe for concurrent use; read CallCount via Calls() while runs are in flight.
type MockLLM struct {
	ModelName string
	Responses []*genai.Content
	CallCount int

	mu       sync.Mutex
	Requests []*model.LLMRequest
	// Usage, if set, is attached to every response.
	Usage *genai.GenerateContentResponseUsageMetadata
	// ServedBy and Metadata, if set, become each response's ModelVersion and
	// CustomMetadata.
	ServedBy string
	Metadata map[string]any
	// FinishReasons, if set, are the responses' finish reasons, in order
	// (MAX_TOKENS: a reply cut off at the output limit).
	FinishReasons []genai.FinishReason
	// Chunk, if set, streams a reply that's only text when streaming is
	// asked for, as a streaming API does: pieces of Chunk bytes as
	// partial responses, then the whole text as the final one.
	Chunk int
}

// Calls returns the number of GenerateContent calls so far.
func (m *MockLLM) Calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.CallCount
}

// NewMockLLM returns a scripted model for tests: it answers each call with
// the next of responses, then "Done.", and is named name ("mock-llm" if
// empty).
func NewMockLLM(name string, responses ...*genai.Content) *MockLLM {
	if name == "" {
		name = "mock-llm"
	}
	return &MockLLM{
		ModelName: name,
		Responses: responses,
	}
}

// Name is the model's name.
func (m *MockLLM) Name() string {
	return m.ModelName
}

// GenerateContent records the request and answers with the next scripted
// response ("Done." once they run out), as one final response whether or
// not streaming was asked for. As strict as the APIs it stands for: a
// call whose context is done fails with its error, and a request with a
// tool result whose call isn't before it is refused.
func (m *MockLLM) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		m.mu.Lock()
		idx := m.CallCount
		m.CallCount++
		m.Requests = append(m.Requests, req)
		m.mu.Unlock()
		if err := ctx.Err(); err != nil {
			yield(nil, err)
			return
		}
		if req != nil {
			if err := unpairedResult(req.Contents); err != nil {
				yield(nil, err)
				return
			}
		}

		var content *genai.Content
		if idx < len(m.Responses) {
			content = m.Responses[idx]
		} else {
			content = genai.NewContentFromText("Done.", genai.RoleModel)
		}

		var usage *genai.GenerateContentResponseUsageMetadata
		if m.Usage != nil { // each response its own, as a real one's
			u := *m.Usage
			usage = &u
		}
		resp := &model.LLMResponse{
			Content:        content,
			UsageMetadata:  usage,
			ModelVersion:   m.ServedBy,
			CustomMetadata: m.Metadata,
		}
		if idx < len(m.FinishReasons) {
			resp.FinishReason = m.FinishReasons[idx]
		}
		if text, ok := onlyText(content); ok && stream && m.Chunk > 0 {
			for i := 0; i < len(text); i += m.Chunk {
				piece := genai.NewContentFromText(text[i:min(i+m.Chunk, len(text))], genai.RoleModel)
				if !yield(&model.LLMResponse{Content: piece, Partial: true}, nil) {
					return
				}
			}
			resp.Content = genai.NewContentFromText(text, genai.RoleModel) // its own, as the aggregate is
		}
		yield(resp, nil)
	}
}

// onlyText is content's text when it has nothing else.
func onlyText(content *genai.Content) (string, bool) {
	if content == nil || len(content.Parts) == 0 {
		return "", false
	}
	var b strings.Builder
	for _, p := range content.Parts {
		if p == nil || p.Text == "" || p.Thought || p.FunctionCall != nil {
			return "", false
		}
		b.WriteString(p.Text)
	}
	return b.String(), true
}

// unpairedResult is the error the model APIs give a request with a tool
// result whose call (by ID when it has one, else by name) isn't in an
// earlier message.
func unpairedResult(contents []*genai.Content) error {
	calls := map[string]bool{}
	for _, c := range contents {
		if c == nil {
			continue
		}
		for _, p := range c.Parts {
			switch {
			case p.FunctionCall != nil:
				calls[p.FunctionCall.ID] = true
				calls["name:"+p.FunctionCall.Name] = true
			case p.FunctionResponse != nil:
				r := p.FunctionResponse
				if r.ID != "" && !calls[r.ID] || r.ID == "" && !calls["name:"+r.Name] {
					return fmt.Errorf("400 Bad Request: a tool result (%s %q) has no tool call before it", r.Name, r.ID)
				}
			}
		}
	}
	return nil
}

// toolCallParsingModel wraps any model.LLM to detect and convert JSON-encoded tool calls in text
// (such as those emitted by Ollama models) into native Google ADK FunctionCalls.
// newOpenAIModel builds an OpenAI-compatible (Responses API) model with
// image support and text-encoded tool call parsing.
func newOpenAIModel(ctx context.Context, name, apiKey, baseURL string, opts ...option.RequestOption) (model.LLM, error) {
	return newOpenAIModelFor(ctx, name, apiKey, baseURL, openaimodel.APIResponses, opts...)
}

// newOpenAIModelFor is newOpenAIModel speaking api: OpenAI's Responses or
// Chat Completions (an OpenAI-compatible server's, MDL-112).
func newOpenAIModelFor(ctx context.Context, name, apiKey, baseURL string, api openaimodel.API, opts ...option.RequestOption) (model.LLM, error) {
	m, err := openaimodel.NewModel(ctx, name, &openaimodel.ClientConfig{
		API:     api,
		APIKey:  apiKey,
		BaseURL: baseURL,
		// The last is innermost and reads the reply first: provider tools take
		// their items out (CA-82) before the output middleware keeps what the
		// adapter can't read (CA-30).
		Options: append([]option.RequestOption{option.WithMiddleware(openAIImageMiddleware, openAIOutputMiddleware, openAIProviderToolsMiddleware)}, opts...),
	})
	if err != nil {
		return nil, err
	}
	return &toolCallParsingModel{inner: m}, nil
}

type toolCallParsingModel struct {
	inner model.LLM
	// coreTools offers the model only the core tools (a local model's,
	// MOD-50).
	coreTools bool
	// provider and key, when set, let a failed call say what the provider
	// said (providerError), with the key masked.
	provider, key string
}

func (m *toolCallParsingModel) Name() string {
	return m.inner.Name()
}

func (m *toolCallParsingModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		// A streamed reply's text goes on as it's written; text-encoded tool
		// calls can only be recognised in the whole reply, which is parsed
		// (spec_056 CA-50). What the adapter can't read is taken out of the
		// reply on its way in and kept with the answer (CA-30).
		if m.coreTools {
			req = withCoreTools(req)
		}
		ctx, req := replaceImagesWithMarkers(ctx, req)
		ctx, unshown := withUnshown(ctx)
		for resp, err := range m.inner.GenerateContent(ctx, req, stream) {
			if err != nil {
				if !yield(nil, providerError(err, m.provider, m.inner.Name(), m.key)) {
					return
				}
				continue
			}
			if resp != nil && resp.Content != nil && !resp.Partial {
				parseTextToolCalls(resp.Content, offeredTools(req))
			}
			if resp != nil && !resp.Partial {
				attachUnshown(resp, unshown.take())
			}
			if !yield(resp, nil) {
				return
			}
		}
	}
}

// offeredTools returns the names of tools declared in the request. Only these
// may be synthesised from text, so a model that merely quotes a JSON snippet
// (for example, echoing a file it read) cannot trigger an arbitrary tool.
func offeredTools(req *model.LLMRequest) map[string]bool {
	names := make(map[string]bool)
	if req == nil {
		return names
	}
	for name := range req.Tools {
		names[name] = true
	}
	if req.Config != nil {
		for _, t := range req.Config.Tools {
			if t == nil {
				continue
			}
			for _, fd := range t.FunctionDeclarations {
				if fd != nil {
					names[fd.Name] = true
				}
			}
		}
	}
	return names
}

func parseTextToolCalls(content *genai.Content, allowed map[string]bool) {
	if len(allowed) == 0 {
		return
	}
	var parts []*genai.Part
	for _, part := range content.Parts {
		if part.FunctionCall != nil || part.Text == "" {
			parts = append(parts, part)
			continue
		}
		trimmed := strings.TrimSpace(part.Text)
		if fc, ok := parseTextCall(unfence(trimmed), allowed); ok {
			part.Text = ""
			part.FunctionCall = fc
			parts = append(parts, part)
			continue
		}
		// Small models often say what they're about to do and write the
		// calls among the prose, fenced or in <tool_call> tags.
		pieces, ok := textCalls(trimmed, allowed)
		if !ok {
			// Or one call a line, a whole plan at once, the later ones
			// written before any result came back: the first runs.
			pieces, ok = firstLineCall(trimmed, allowed)
		}
		if !ok {
			parts = append(parts, part)
			continue
		}
		parts = append(parts, pieces...)
	}
	content.Parts = parts
}

// unfence is s without the Markdown code fence around all of it.
func unfence(s string) string {
	if strings.HasPrefix(s, "```") {
		lines := strings.Split(s, "\n")
		if len(lines) >= 3 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
			return strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
		}
	}
	return s
}

// callBlockRE finds the blocks a model writes tool calls in: a Markdown
// code fence (with or without a language tag) or <tool_call> tags.
var callBlockRE = regexp.MustCompile("(?s)```[A-Za-z]*[ \\t]*\\n?(.*?)```|<tool_call>(.*?)</tool_call>")

// textCalls splits text into its prose and its tool calls, when every
// block in it (a code fence or <tool_call> tags) is a call of an offered
// tool; a reply that also quotes code or a file, or names a tool that
// wasn't offered, is left as it is (ok false).
func textCalls(text string, allowed map[string]bool) (_ []*genai.Part, ok bool) {
	var out []*genai.Part
	prose := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, &genai.Part{Text: s})
		}
	}
	at := 0
	for _, m := range callBlockRE.FindAllStringSubmatchIndex(text, -1) {
		body := ""
		if m[2] >= 0 {
			body = text[m[2]:m[3]]
		} else {
			body = text[m[4]:m[5]]
		}
		fc, isCall := parseTextCall(strings.TrimSpace(body), allowed)
		if !isCall {
			return nil, false
		}
		prose(text[at:m[0]])
		out = append(out, &genai.Part{FunctionCall: fc})
		ok, at = true, m[1]
	}
	prose(text[at:])
	return out, ok
}

// firstLineCall finds the first line of text that is a JSON tool call of
// an offered tool, as a small local model writes a plan of calls one a
// line (qwen2.5-coder on Ollama, 2026-11-08): the prose before it, and the
// call. What follows is dropped: the calls after it, and words about
// results, were written before the first one ran, and the model is asked
// again once it has.
func firstLineCall(text string, allowed map[string]bool) ([]*genai.Part, bool) {
	if strings.Contains(text, "```") { // quoted code: a call in it isn't one
		return nil, false
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		fc, ok := parseTextCall(strings.TrimSpace(line), allowed)
		if !ok {
			continue
		}
		var out []*genai.Part
		if before := strings.TrimSpace(strings.Join(lines[:i], "\n")); before != "" {
			out = append(out, &genai.Part{Text: before})
		}
		return append(out, &genai.Part{FunctionCall: fc}), true
	}
	return nil, false
}

// parseTextCall reads s as a JSON tool call {"name", "arguments"} of an
// offered tool ("args" and "parameters" do too).
func parseTextCall(s string, allowed map[string]bool) (*genai.FunctionCall, bool) {
	if !strings.HasPrefix(s, "{") || !strings.HasSuffix(s, "}") {
		return nil, false
	}
	var call struct {
		Name       string         `json:"name"`
		Arguments  map[string]any `json:"arguments"`
		Args       map[string]any `json:"args"`
		Parameters map[string]any `json:"parameters"`
	}
	if err := json.Unmarshal([]byte(s), &call); err != nil || !allowed[call.Name] {
		return nil, false
	}
	args := call.Arguments
	if len(args) == 0 {
		args = call.Args
	}
	if len(args) == 0 {
		args = call.Parameters
	}
	if args == nil {
		args = make(map[string]any)
	}
	// Normalize aliases: "file_path" -> "path"
	if fp, ok := args["file_path"]; ok && args["path"] == nil {
		args["path"] = fp
	}
	return &genai.FunctionCall{Name: call.Name, Args: args}, true
}

// coreTools are the tools a local model is offered (spec_061_models
// MOD-50): enough to read, search, write, run and look things up, ask the
// user, and use the workspace's connections (db__…) and Blitz's own
// commands. Given every tool, a 7B model writes its calls as text, or
// answers without them; given these, it calls them (2026-11-08).
var coreTools = map[string]bool{
	"read_file": true, "list_files": true, "glob": true, "grep": true, "search_workspace": true,
	"create_file": true, "edit": true, "replace_in_file": true, "delete_file": true,
	"run_shell_command": true, "web_fetch": true, "web_search": true,
	"ask_user_question": true, "todo": true, "open_file": true, "open_page": true,
	"blitz": true, "recall_session": true, "generate_image": true,
	"exit_plan_mode": true, "enter_plan_mode": true,
}

// coreTool reports whether a local model is offered the tool name.
func coreTool(name string) bool { return coreTools[name] || strings.HasPrefix(name, "db__") }

// withCoreTools is req offering only the core tools: its declarations,
// and the tools it can call, the rest left out (a copy; req isn't changed).
func withCoreTools(req *model.LLMRequest) *model.LLMRequest {
	if req == nil {
		return req
	}
	cp := *req
	if req.Tools != nil {
		cp.Tools = make(map[string]any, len(req.Tools))
		for name, t := range req.Tools {
			if coreTool(name) {
				cp.Tools[name] = t
			}
		}
	}
	if req.Config != nil {
		cfg := *req.Config
		cfg.Tools = nil
		for _, t := range req.Config.Tools {
			if t == nil {
				continue
			}
			if len(t.FunctionDeclarations) == 0 {
				cfg.Tools = append(cfg.Tools, t) // a provider's own tool (search): kept
				continue
			}
			kept := *t
			kept.FunctionDeclarations = nil
			for _, fd := range t.FunctionDeclarations {
				if fd != nil && coreTool(fd.Name) {
					kept.FunctionDeclarations = append(kept.FunctionDeclarations, fd)
				}
			}
			if len(kept.FunctionDeclarations) > 0 {
				cfg.Tools = append(cfg.Tools, &kept)
			}
		}
		cfg.SystemInstruction = leanInstruction(req.Config.SystemInstruction, offeredTools(&model.LLMRequest{Config: &cfg}))
		cp.Config = &cfg
	}
	return &cp
}

// leanInstruction is a local model's instructions for its core tools
// (MOD-50): without the skills list (it has no activate_skill) and the
// agent kit's handoff section (no transfer_to_agent), which name tools it
// isn't offered; and with what its tools are and that a file is written
// with them. A copy; si isn't changed.
func leanInstruction(si *genai.Content, tools map[string]bool) *genai.Content {
	if si == nil {
		return nil
	}
	out := &genai.Content{Role: si.Role}
	for _, p := range si.Parts {
		if p == nil || p.Text == "" {
			out.Parts = append(out.Parts, p)
			continue
		}
		cp := *p
		cp.Text = withoutSection(withoutHandoff(p.Text), "## Available Agent Skills")
		out.Parts = append(out.Parts, &cp)
	}
	names := slices.Sorted(maps.Keys(tools))
	out.Parts = append(out.Parts, &genai.Part{Text: "Your tools are " + strings.Join(names, ", ") + ". Use them to do the work: " +
		"to write or change a file, call create_file (or edit, replace_in_file) with its whole content, never paste the content in your answer instead, " +
		"and never say a file is written until the tool said so. Make one call at a time and read its result before the next."})
	return out
}

// withoutSection is text without the Markdown section headed heading (to
// the next heading of its level or above, or the end).
func withoutSection(text, heading string) string {
	i := strings.Index(text, heading)
	if i < 0 {
		return text
	}
	rest := text[i+len(heading):]
	end := len(text)
	if j := strings.Index(rest, "\n## "); j >= 0 {
		end = i + len(heading) + j + 1
	} else if j := strings.Index(rest, "\n# "); j >= 0 {
		end = i + len(heading) + j + 1
	}
	return text[:i] + text[end:]
}

// handoffStart and handoffNote begin and end the agent kit's section
// about handing a question to another agent with transfer_to_agent.
const (
	handoffStart = "You have a list of other agents to transfer to"
	handoffNote  = "**NOTE**: the only available agents for `transfer_to_agent`"
)

// withoutHandoff is text without the handoff section.
func withoutHandoff(text string) string {
	i := strings.Index(text, handoffStart)
	if i < 0 {
		return text
	}
	end := len(text)
	if n := strings.Index(text[i:], handoffNote); n >= 0 {
		if j := strings.Index(text[i+n:], "\n\n"); j >= 0 {
			end = i + n + j
		}
	}
	return strings.TrimRight(text[:i], "\n ") + text[end:]
}
