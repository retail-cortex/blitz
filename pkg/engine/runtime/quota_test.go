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
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

const vertexQuota = "Quota exceeded for aiplatform.googleapis.com/online_prediction_requests_per_base_model with base model: anthropic-claude-opus-5. Please submit a quota increase request."

func TestAsQuota(t *testing.T) {
	plain := errors.New("boom")
	cases := []struct {
		name  string
		err   error
		quota string // Google's message; "" when it isn't a quota error
	}{
		{"nil", nil, ""},
		{"other", plain, ""},
		{"Gemini quota", genai.APIError{Code: 429, Status: "RESOURCE_EXHAUSTED", Message: "Quota exceeded for metric generate_content_requests"}, "Quota exceeded for metric generate_content_requests"},
		{"Gemini quota, wrapped", fmt.Errorf("model: %w", genai.APIError{Code: 429, Status: "RESOURCE_EXHAUSTED", Message: "Quota exceeded"}), "Quota exceeded"},
		{"Gemini overloaded", genai.APIError{Code: 503, Status: "UNAVAILABLE", Message: "overloaded"}, ""},
		{"already a quota", &QuotaError{Message: "x"}, "x"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := asQuota(c.err)
			q, ok := errors.AsType[*QuotaError](got)
			if c.quota == "" {
				assert.False(t, ok, "%v", got)
				assert.Equal(t, c.err, got)
				return
			}
			require.True(t, ok, "%v", got)
			assert.Equal(t, c.quota, q.Message)
			assert.Contains(t, got.Error(), "quota exceeded: "+c.quota)
			assert.Contains(t, got.Error(), "IAM & Admin › Quotas")
			assert.NotContains(t, got.Error(), "rate limited")
		})
	}
}

func TestGoogleExhausted(t *testing.T) {
	cases := []struct {
		name, body, want string
		ok               bool
	}{
		{"Vertex AI list", `[{"error":{"code":429,"message":"` + vertexQuota + `","status":"RESOURCE_EXHAUSTED"}}]`, vertexQuota, true},
		{"one error", `{"error":{"code":429,"message":"Quota exceeded","status":"RESOURCE_EXHAUSTED"}}`, "Quota exceeded", true},
		{"Anthropic rate limit", `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`, "", false},
		{"not JSON", `too many requests`, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := googleExhausted(c.body)
			assert.Equal(t, c.ok, ok)
			assert.Equal(t, c.want, got)
		})
	}
}

// Claude on Vertex AI: a 429 with Google's RESOURCE_EXHAUSTED is a quota,
// Anthropic's own 429 a rate limit.
func TestAnthropicQuotaError(t *testing.T) {
	reply := func(body string) func(http.ResponseWriter) {
		return func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(429)
			io.WriteString(w, body)
		}
	}
	_, opts := newFake(t,
		reply(`[{"error":{"code":429,"message":"`+vertexQuota+`","status":"RESOURCE_EXHAUSTED"}}]`),
		reply(`{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`),
	)
	m := withModelSettings(mustAnthropicModel(t, config.AnthropicConfig{}, "claude-opus-5", opts...), "anthropic")
	req := &model.LLMRequest{Contents: []*genai.Content{userText("x")}}

	_, err := collectResponses(t, m, req, false)
	q, ok := errors.AsType[*QuotaError](err)
	require.True(t, ok, "not a quota error: %v", err)
	assert.Equal(t, vertexQuota, q.Message)
	assert.NotContains(t, err.Error(), "rate limited")

	_, err = collectResponses(t, m, req, false)
	assert.Contains(t, err.Error(), "rate limited", "%v", err)
	_, ok = errors.AsType[*QuotaError](err)
	assert.False(t, ok, "a rate limit reported as quota: %v", err)
}
