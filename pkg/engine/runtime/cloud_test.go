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
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

type fakeCredential struct{ err error }

func (f fakeCredential) GetToken(_ context.Context, opts policy.TokenRequestOptions) (azcore.AccessToken, error) {
	if f.err != nil {
		return azcore.AccessToken{}, f.err
	}
	return azcore.AccessToken{Token: "entra-for-" + strings.Join(opts.Scopes, ","), ExpiresOn: time.Now().Add(time.Hour)}, nil
}

// azureServer answers Azure OpenAI's chat completions and Foundry's
// messages, recording the credentials each request carried.
func azureServer(t *testing.T) (*httptest.Server, func() []string) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.Path+" api-key="+r.Header.Get("api-key")+" x-api-key="+r.Header.Get("X-Api-Key")+" auth="+r.Header.Get("Authorization"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/messages") {
			json.NewEncoder(w).Encode(map[string]any{
				"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-x",
				"content":     []any{map[string]any{"type": "text", "text": "hi from claude"}},
				"stop_reason": "end_turn", "usage": map[string]any{"input_tokens": 3, "output_tokens": 2},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id": "resp_1", "object": "response", "created_at": 1, "status": "completed", "model": "gpt-x",
			"output": []any{map[string]any{"type": "message", "id": "m1", "status": "completed", "role": "assistant",
				"content": []any{map[string]any{"type": "output_text", "text": "hi from gpt", "annotations": []any{}}}}},
			"usage": map[string]any{"input_tokens": 3, "output_tokens": 2, "total_tokens": 5},
		})
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string{}, seen...) }
}

func reply(t *testing.T, m model.LLM) string {
	t.Helper()
	req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hello", genai.RoleUser)}, Config: &genai.GenerateContentConfig{}}
	var text string
	for resp, err := range m.GenerateContent(context.Background(), req, false) {
		require.NoError(t, err)
		if resp.Content != nil {
			for _, p := range resp.Content.Parts {
				text += p.Text
			}
		}
	}
	return text
}

func TestAzureModels(t *testing.T) {
	srv, seen := azureServer(t)
	old := azureCredential
	t.Cleanup(func() { azureCredential = old })
	azureCredential = func() (azcore.TokenCredential, error) { return fakeCredential{}, nil }
	pol := policyFrom(config.LLMConfig{})
	base := config.AzureConfig{BaseURL: srv.URL + "/openai/v1/", AnthropicBaseURL: srv.URL + "/anthropic/", APIKey: "k1"}

	for _, tt := range []struct {
		name, model, auth, want, header string
	}{
		{name: "OpenAI with a key", model: "gpt-x", want: "hi from gpt", header: "/openai/v1/responses api-key=k1 x-api-key= auth="},
		{name: "OpenAI with Entra", model: "gpt-x", auth: "entra", want: "hi from gpt", header: "api-key= x-api-key= auth=Bearer entra-for-https://cognitiveservices.azure.com/.default"},
		{name: "Claude with a key", model: "claude-x", want: "hi from claude", header: "/anthropic/v1/messages api-key= x-api-key=k1"},
		{name: "Claude with Entra", model: "claude-x", auth: "entra", want: "hi from claude", header: "auth=Bearer entra-for-https://ai.azure.com/.default"},
		{name: "OpenAI with OAuth, as Entra", model: "gpt-x", auth: "oauth", want: "hi from gpt", header: "api-key= x-api-key= auth=Bearer entra-for-https://cognitiveservices.azure.com/.default"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := base
			c.Auth = tt.auth
			m, err := newAzureModel(context.Background(), c, tt.model, pol)
			require.NoError(t, err)
			assert.Equal(t, tt.want, reply(t, m))
			got := seen()
			assert.Contains(t, got[len(got)-1], tt.header)
		})
	}

	for _, tt := range []struct {
		name string
		cfg  config.AzureConfig
		cred error
		err  string
	}{
		{name: "no model", cfg: config.AzureConfig{Resource: "r"}, err: "[llm.azure] model"},
		{name: "no resource", cfg: config.AzureConfig{Model: "gpt-x", APIKey: "k"}, err: "[llm.azure] resource"},
		{name: "no key", cfg: config.AzureConfig{Model: "gpt-x", Resource: "r"}, err: "no Azure OpenAI key"},
		{name: "bad auth", cfg: config.AzureConfig{Model: "gpt-x", Resource: "r", Auth: "magic"}, err: "unknown [llm.azure] auth"},
		{name: "no Entra token", cfg: config.AzureConfig{Model: "gpt-x", Resource: "r", Auth: "entra"}, cred: errors.New("not logged in"), err: "no Entra ID token"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AZURE_OPENAI_API_KEY", "")
			azureCredential = func() (azcore.TokenCredential, error) { return fakeCredential{err: tt.cred}, nil }
			_, err := newAzureModel(context.Background(), tt.cfg, "", pol)
			assert.ErrorContains(t, err, tt.err)
		})
	}
}

func TestBedrockModel(t *testing.T) {
	// No shared AWS files or environment credentials.
	dir := t.TempDir()
	for k, v := range map[string]string{
		"AWS_CONFIG_FILE": filepath.Join(dir, "config"), "AWS_SHARED_CREDENTIALS_FILE": filepath.Join(dir, "credentials"),
		"AWS_ACCESS_KEY_ID": "", "AWS_SECRET_ACCESS_KEY": "", "AWS_SESSION_TOKEN": "", "AWS_PROFILE": "", "AWS_REGION": "",
		"AWS_DEFAULT_REGION": "", "AWS_BEARER_TOKEN_BEDROCK": "", "AWS_EC2_METADATA_DISABLED": "true",
		"AWS_CONTAINER_CREDENTIALS_FULL_URI": "", "AWS_CONTAINER_CREDENTIALS_RELATIVE_URI": "", "AWS_WEB_IDENTITY_TOKEN_FILE": "",
	} {
		t.Setenv(k, v)
	}
	ctx := context.Background()
	_, err := newBedrockModel(ctx, config.BedrockConfig{}, "")
	assert.ErrorContains(t, err, "[llm.bedrock] model")
	_, err = newBedrockModel(ctx, config.BedrockConfig{Model: "us.anthropic.claude-x"}, "")
	assert.ErrorContains(t, err, "no AWS region")
	_, err = newBedrockModel(ctx, config.BedrockConfig{Model: "us.anthropic.claude-x", Region: "us-east-1"}, "")
	assert.ErrorContains(t, err, "no AWS credentials")

	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "bedrock-key")
	m, err := newBedrockModel(ctx, config.BedrockConfig{Model: "us.anthropic.claude-x", Region: "us-east-1"}, "")
	require.NoError(t, err)
	assert.Equal(t, "us.anthropic.claude-x", m.Name())

	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIDEXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")
	cfg := config.DefaultConfig()
	cfg.LLM.Provider = "bedrock"
	cfg.LLM.Bedrock = config.BedrockConfig{Model: "us.anthropic.claude-x", Region: "eu-west-1"}
	built, err := NewModel(ctx, cfg, "")
	require.NoError(t, err)
	assert.Contains(t, built.Name(), "claude-x")
	assert.Equal(t, "us.anthropic.claude-x", cfg.ModelName())
	p, name := ParseModelRef("bedrock/us.anthropic.claude-y", "gemini")
	assert.Equal(t, []string{"bedrock", "us.anthropic.claude-y"}, []string{p, name})
	assert.Contains(t, ConfiguredProviders(cfg), "bedrock")
}

// Credentials that can't be found fail the model's build: an AWS profile
// that doesn't exist, or no Entra ID credential chain at all.
func TestCloudCredentialFailures(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "credentials"))
	_, err := newBedrockModel(context.Background(), config.BedrockConfig{Model: "us.anthropic.claude-x", Region: "us-east-1", Profile: "nobody"}, "")
	assert.ErrorContains(t, err, "AWS configuration")

	old := azureCredential
	t.Cleanup(func() { azureCredential = old })
	azureCredential = func() (azcore.TokenCredential, error) { return nil, errors.New("no chain") }
	_, err = newAzureModel(context.Background(), config.AzureConfig{Model: "claude-x", Resource: "r", Auth: "entra"}, "", policyFrom(config.LLMConfig{}))
	assert.ErrorContains(t, err, "finding Entra ID credentials")
}
