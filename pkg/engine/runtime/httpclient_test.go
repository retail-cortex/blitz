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
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/genai"
)

func TestStalledBodyFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "data: partial\n")
		w.(http.Flusher).Flush()
		select { // then go quiet
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()

	client := retryPolicy{stall: 200 * time.Millisecond}.httpClient()
	resp, err := client.Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	start := time.Now()
	_, err = io.ReadAll(resp.Body)
	require.ErrorIs(t, err, ErrStalled, "want ErrStalled, got %v", err)
	d := time.Since(start)
	require.LessOrEqual(t, d, 2*time.Second, "stall detected after %v", d)
}

func TestSlowButSteadyStreamIsNotCutOff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for range 8 { // 800 ms in total, never quiet for 300 ms
			io.WriteString(w, "x")
			w.(http.Flusher).Flush()
			time.Sleep(100 * time.Millisecond)
		}
	}))
	defer srv.Close()

	resp, err := retryPolicy{stall: 300 * time.Millisecond}.httpClient().Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "got %q,", b)
	require.Equal(t, "xxxxxxxx", string(b), "got %q, %v", b, err)
}

func TestNoHeadersFailsFast(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()
	start := time.Now()
	_, err := (retryPolicy{stall: 200 * time.Millisecond}).httpClient().Get(srv.URL)
	require.Error(t, err, "request without headers succeeded")
	d := time.Since(start)
	require.LessOrEqual(t, d, 2*time.Second, "header timeout took %v", d)
}

// flaky answers the first `fail` requests with status, then with ok.
func flaky(t *testing.T, fail int, status int, ok string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		if int(calls.Add(1)) <= fail {
			w.WriteHeader(status)
			io.WriteString(w, `{"error":{"message":"try again"}}`)
			return
		}
		io.WriteString(w, ok)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func generateText(t *testing.T, m model.LLM) (string, error) {
	t.Helper()
	req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)}}
	var text string
	for resp, err := range m.GenerateContent(context.Background(), req, false) {
		if err != nil {
			return text, err
		}
		if resp.Content != nil {
			for _, p := range resp.Content.Parts {
				text += p.Text
			}
		}
	}
	return text, nil
}

const openAIOK = `{"id":"resp_1","model":"gpt-test","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":1,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":2}}`

const geminiOK = `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`

func TestProvidersRetryTransientErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		ok     string
		build  func(t *testing.T, url string, retries int) model.LLM
	}{
		{"anthropic 529 overloaded", 529, message("end_turn", `{"type":"text","text":"ok"}`), func(t *testing.T, url string, retries int) model.LLM {
			cfg := config.DefaultConfig()
			cfg.LLM.Provider, cfg.LLM.Anthropic.BaseURL, cfg.LLM.Anthropic.APIKey, cfg.LLM.MaxRetries = "anthropic", url, "sk-ant-test", retries
			cfg.LLM.Anthropic.Fallbacks = "off"
			m, err := NewModel(context.Background(), cfg, "")
			require.NoError(t, err)
			return m
		}},
		{"openai 503", 503, openAIOK, func(t *testing.T, url string, retries int) model.LLM {
			cfg := config.DefaultConfig()
			cfg.LLM.Provider, cfg.LLM.OpenAI.BaseURL, cfg.LLM.OpenAI.APIKey, cfg.LLM.MaxRetries = "openai", url+"/v1", "sk-test", retries
			m, err := NewModel(context.Background(), cfg, "gpt-test")
			require.NoError(t, err)
			return m
		}},
		{"gemini 429", 429, geminiOK, func(t *testing.T, url string, retries int) model.LLM {
			old := geminiInitialDelay
			geminiInitialDelay = 0.01
			t.Cleanup(func() { geminiInitialDelay = old })
			gc := retryPolicy{maxRetries: retries, stall: time.Minute}.geminiConfig("test-key")
			gc.Backend = genai.BackendGeminiAPI
			gc.HTTPOptions.BaseURL = url
			m, err := gemini.NewModel(context.Background(), "gemini-3.8-flash", gc)
			require.NoError(t, err)
			return m
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv, calls := flaky(t, 2, c.status, c.ok)
			text, err := generateText(t, c.build(t, srv.URL, 2))
			require.NoError(t, err, "with 2 retries: %q,", text)
			require.Equal(t, "ok", text, "with 2 retries: %q, %v", text, err)
			n := calls.Load()
			require.Equal(t, int32(3), n, "want 3 attempts, got %d", n)

			srv, calls = flaky(t, 1, c.status, c.ok)
			_, err = generateText(t, c.build(t, srv.URL, 0))
			require.Error(t, err, "max_retries = 0 still retried")
			n = calls.Load()
			require.Equal(t, int32(1), n, "max_retries = 0: want 1 attempt, got %d", n)
		})
	}
}

func TestClientErrorsAreNotRetried(t *testing.T) {
	srv, calls := flaky(t, 5, http.StatusBadRequest, openAIOK)
	cfg := config.DefaultConfig()
	cfg.LLM.Provider, cfg.LLM.OpenAI.BaseURL, cfg.LLM.OpenAI.APIKey = "openai", srv.URL+"/v1", "sk-test"
	m, err := NewModel(context.Background(), cfg, "gpt-test")
	require.NoError(t, err)
	_, err = generateText(t, m)
	require.Error(t, err, "want a 400 error, got")
	require.Contains(t, err.Error(), "400", "want a 400 error, got %v", err)
	n := calls.Load()
	require.Equal(t, int32(1), n, "a 400 was retried: %d attempts", n)
}

func TestPolicyDefaults(t *testing.T) {
	p := policyFrom(config.LLMConfig{MaxRetries: -1})
	require.Equal(t, 0, p.maxRetries, "policy = %+v", p)
	require.Equal(t, defaultStallTimeout, p.stall, "policy = %+v", p)
	d := config.DefaultConfig().LLM
	require.Equal(t, 3, d.MaxRetries, "config defaults = %d retries, %ds stall", d.MaxRetries, d.StallTimeoutSeconds)
	require.Equal(t, 600, d.StallTimeoutSeconds, "config defaults = %d retries, %ds stall", d.MaxRetries, d.StallTimeoutSeconds)
}

// Models built with one stall limit share a client (and its connections);
// another limit gets its own.
func TestModelsShareAClient(t *testing.T) {
	a := retryPolicy{stall: 41 * time.Second}.httpClient()
	require.Same(t, a, retryPolicy{stall: 41 * time.Second, maxRetries: 5}.httpClient())
	require.NotSame(t, a, retryPolicy{stall: 42 * time.Second}.httpClient())
}
