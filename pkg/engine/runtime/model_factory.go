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
	"fmt"
	"iter"
	"log/slog"
	"net/http"
	"os"
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
	provider := strings.ToLower(cfg.LLM.Provider)
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
	p, name := ParseModelRef(ref, cfg.LLM.Provider)
	return newProviderModel(ctx, cfg, p, name)
}

// knownProviders are the provider prefixes recognised in model references.
var knownProviders = map[string]bool{"gemini": true, "anthropic": true, "openai": true, "ollama": true}

// ParseModelRef splits "provider/model" into its parts. Without a known
// provider prefix the whole reference is a model of defaultProvider, so
// OpenRouter-style names ("anthropic/claude-…" on the openai provider) need
// the provider spelled out: "openai/anthropic/claude-…".
func ParseModelRef(ref, defaultProvider string) (provider, name string) {
	ref = strings.TrimSpace(ref)
	if p, rest, ok := strings.Cut(ref, "/"); ok && knownProviders[strings.ToLower(p)] && rest != "" {
		return strings.ToLower(p), rest
	}
	return strings.ToLower(defaultProvider), ref
}

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

// newProviderModel builds one model of the given provider, applying its
// [model_settings] on each call.
func newProviderModel(ctx context.Context, cfg *config.Config, provider, modelName string) (model.LLM, error) {
	m, err := buildProviderModel(ctx, cfg, provider, modelName)
	if err != nil {
		return nil, err
	}
	return withModelSettings(m, providerOf(m)), nil
}

// providerOf names the API a built model talks to; with provider "" the
// choice was made from the credentials present.
func providerOf(m model.LLM) string {
	switch m.(type) {
	case *anthropicModel:
		return "anthropic"
	case *toolCallParsingModel:
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
		return gemini.NewModel(ctx, modelName, clientCfg)

	case "openai", "ollama":
		apiKey, baseURL := openAICompatEndpoint(cfg.LLM.OpenAI, provider)
		return newOpenAIModel(ctx, modelName, apiKey, baseURL, pol.openAIOptions()...)

	case "anthropic":
		return newAnthropicModel(ctx, cfg.LLM.Anthropic, modelName, pol.anthropicOptions()...)

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
		// Fallback to Gemini if configured
		if cfg.LLM.Gemini.APIKey != "" {
			return gemini.NewModel(ctx, modelName, pol.geminiConfig(cfg.LLM.Gemini.APIKey))
		}
		return nil, fmt.Errorf("unsupported or unconfigured LLM provider '%s'", provider)
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

// geminiClientConfig is the genai client for Gemini's settings: the Gemini
// API with an API key, or Vertex AI with Application Default Credentials
// (auth = "adc"). genai signs requests itself only on an HTTP client of its
// own; ours (retries, stalls) gets the credentials added here, as genai's
// UseDefaultCredentials would, with the quota project genai would send.
// Credentials that can't be found fail here, when the model is built,
// rather than at the first request.
func geminiClientConfig(ctx context.Context, g config.GeminiConfig, pol retryPolicy) (*genai.ClientConfig, error) {
	switch g.Auth {
	case "", config.AuthAPIKey:
		cc := pol.geminiConfig(g.APIKey)
		cc.Project, cc.Location = g.ProjectID, g.Location
		if g.APIKey != "" {
			cc.Backend = genai.BackendGeminiAPI
		}
		return cc, nil
	case config.AuthADC:
		cc := pol.geminiConfig("")
		cc.Backend = genai.BackendVertexAI
		project, location, err := vertexPlace("gemini", g.ProjectID, g.Location)
		if err != nil {
			return nil, err
		}
		cc.Project, cc.Location = project, location
		creds, err := googleCredentials()
		if err != nil {
			return nil, fmt.Errorf("gemini with Application Default Credentials: %w (sign in with `gcloud auth application-default login` on the machine running Blitz)", err)
		}
		if err := httptransport.AddAuthorizationMiddleware(cc.HTTPClient, creds); err != nil {
			return nil, fmt.Errorf("gemini with Application Default Credentials: %w", err)
		}
		cc.Credentials = creds
		if quota, err := creds.QuotaProjectID(ctx); err == nil && quota != "" {
			cc.HTTPOptions.Headers = http.Header{"X-Goog-User-Project": []string{quota}}
		}
		return cc, nil
	}
	return nil, fmt.Errorf("unknown [llm.gemini] auth %q (api_key or adc)", g.Auth)
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
// not streaming was asked for.
func (m *MockLLM) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		m.mu.Lock()
		idx := m.CallCount
		m.CallCount++
		m.Requests = append(m.Requests, req)
		m.mu.Unlock()

		var content *genai.Content
		if idx < len(m.Responses) {
			content = m.Responses[idx]
		} else {
			content = genai.NewContentFromText("Done.", genai.RoleModel)
		}

		resp := &model.LLMResponse{
			Content:        content,
			UsageMetadata:  m.Usage,
			ModelVersion:   m.ServedBy,
			CustomMetadata: m.Metadata,
		}
		yield(resp, nil)
	}
}

// toolCallParsingModel wraps any model.LLM to detect and convert JSON-encoded tool calls in text
// (such as those emitted by Ollama models) into native Google ADK FunctionCalls.
// newOpenAIModel builds an OpenAI-compatible (Responses API) model with
// image support and text-encoded tool call parsing.
func newOpenAIModel(ctx context.Context, name, apiKey, baseURL string, opts ...option.RequestOption) (model.LLM, error) {
	m, err := openaimodel.NewModel(ctx, name, &openaimodel.ClientConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Options: append([]option.RequestOption{option.WithMiddleware(openAIImageMiddleware)}, opts...),
	})
	if err != nil {
		return nil, err
	}
	return &toolCallParsingModel{inner: m}, nil
}

type toolCallParsingModel struct {
	inner model.LLM
}

func (m *toolCallParsingModel) Name() string {
	return m.inner.Name()
}

func (m *toolCallParsingModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		// Text-encoded tool calls can only be recognised in complete responses,
		// so this wrapper never streams.
		ctx, req := replaceImagesWithMarkers(ctx, req)
		for resp, err := range m.inner.GenerateContent(ctx, req, false) {
			if err != nil {
				if !yield(nil, err) {
					return
				}
				continue
			}
			if resp != nil && resp.Content != nil {
				parseTextToolCalls(resp.Content, offeredTools(req))
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
	for _, part := range content.Parts {
		if part.FunctionCall != nil || part.Text == "" {
			continue
		}
		trimmed := strings.TrimSpace(part.Text)

		// Strip markdown code fences if wrapped
		if strings.HasPrefix(trimmed, "```") {
			lines := strings.Split(trimmed, "\n")
			if len(lines) >= 3 && strings.HasPrefix(lines[0], "```") && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
				trimmed = strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
			}
		}

		if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
			var call struct {
				Name       string         `json:"name"`
				Arguments  map[string]any `json:"arguments"`
				Args       map[string]any `json:"args"`
				Parameters map[string]any `json:"parameters"`
			}
			if err := json.Unmarshal([]byte(trimmed), &call); err == nil && allowed[call.Name] {
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

				part.Text = ""
				part.FunctionCall = &genai.FunctionCall{
					Name: call.Name,
					Args: args,
				}
			}
		}
	}
}
