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
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"google.golang.org/adk/v2/model"

	"github.com/retail-cortex/blitz/pkg/redact"
)

// ollamaAuto is the ollama provider with no model chosen: the first model
// Ollama lists, looked up on the first call (so a stopped Ollama doesn't
// stop Blitz starting) and kept once found. It never asks for a model
// Ollama doesn't have, as OpenAI's default name would.
type ollamaAuto struct {
	key, baseURL string
	opts         []option.RequestOption

	mu    sync.Mutex
	inner model.LLM
	// list and build are Ollama's model list and newOpenAIModel; tests
	// replace them.
	list  func(ctx context.Context) ([]string, error)
	build func(ctx context.Context, name string) (model.LLM, error)
}

func newOllamaAuto(key, baseURL string, opts []option.RequestOption) *ollamaAuto {
	a := &ollamaAuto{key: key, baseURL: baseURL, opts: opts}
	a.list = func(ctx context.Context) ([]string, error) {
		client := openai.NewClient(append(append([]option.RequestOption{}, opts...), option.WithAPIKey(key), option.WithBaseURL(baseURL))...)
		var ids []string
		pager := client.Models.ListAutoPaging(ctx)
		for pager.Next() {
			ids = append(ids, pager.Current().ID)
		}
		return ids, pager.Err()
	}
	a.build = func(ctx context.Context, name string) (model.LLM, error) {
		m, err := newOpenAIModel(ctx, name, key, baseURL, opts...)
		if t, ok := m.(*toolCallParsingModel); ok {
			t.provider, t.key = "ollama", key
		}
		return m, err
	}
	return a
}

// Name is the model found, or "ollama" before the first call.
func (a *ollamaAuto) Name() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.inner != nil {
		return a.inner.Name()
	}
	return "ollama"
}

// resolve finds the model on the first call, and again after a failure.
func (a *ollamaAuto) resolve(ctx context.Context) (model.LLM, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.inner != nil {
		return a.inner, nil
	}
	ids, err := a.list(ctx)
	if err != nil {
		return nil, fmt.Errorf("ollama isn't answering at %s (%w): open the Ollama app or run `ollama serve`", a.baseURL, err)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("ollama at %s has no models: pull one (`ollama pull qwen3:1.7b`), then choose it in Settings › Providers", a.baseURL)
	}
	m, err := a.build(ctx, ids[0])
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "no Ollama model chosen: using the first it has", "model", ids[0])
	a.inner = m
	return m, nil
}

// GenerateContent runs the call on the model found.
func (a *ollamaAuto) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		m, err := a.resolve(ctx)
		if err != nil {
			yield(nil, err)
			return
		}
		for resp, err := range m.GenerateContent(ctx, req, stream) {
			if !yield(resp, err) {
				return
			}
		}
	}
}

// providerError adds what an OpenAI-compatible provider said to a failed
// call's error, which the SDK leaves out ("OpenAI API error: 404 Not
// Found"), with the key and remembered secrets masked; for Ollama's 404,
// which model it doesn't have and what to do.
func providerError(err error, provider, name, key string) error {
	var apiErr *openai.Error
	if provider == "" || !errors.As(err, &apiErr) {
		return err
	}
	if provider == "ollama" && apiErr.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: Ollama doesn't have the model %q: choose one it has in Settings › Providers, or run `ollama pull %s`", err, name, name)
	}
	msg := strings.TrimSpace(redact.New(key).String(redact.Mask(apiErr.Message)))
	if msg == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, msg)
}
