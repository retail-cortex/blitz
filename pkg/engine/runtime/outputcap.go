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
	"iter"
	"log/slog"
	"regexp"
	"strconv"
	"sync"

	"google.golang.org/adk/v2/model"
)

// outputCapModel keeps one max_tokens default (config.Blitz.MaxTokens)
// workable for models whose maximum output is lower: when the provider
// rejects the limit and says its maximum, the request is sent again with
// that maximum, which the model keeps for later requests. A per-model
// table would go stale; the provider's answer doesn't.
type outputCapModel struct {
	inner model.LLM
	mu    sync.Mutex
	cap   int32 // the maximum learned from a rejection; 0: none yet
}

// capOutput wraps m with outputCapModel.
func capOutput(m model.LLM) model.LLM {
	if m == nil {
		return nil
	}
	return &outputCapModel{inner: m}
}

func (m *outputCapModel) Name() string { return m.inner.Name() }

func (m *outputCapModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		req = m.clamp(req)
		var retry int32
		answered := false
		for resp, err := range m.inner.GenerateContent(ctx, req, stream) {
			if err != nil && !answered && req != nil && req.Config != nil {
				if n, ok := outputCapFrom(err); ok && n > 0 && n < req.Config.MaxOutputTokens {
					retry = n
					break
				}
			}
			answered = true
			if !yield(resp, err) {
				return
			}
		}
		if retry == 0 {
			return
		}
		slog.InfoContext(ctx, "model's output limit is lower than max_tokens: retrying with it", "model", m.inner.Name(), "max_tokens", req.Config.MaxOutputTokens, "limit", retry)
		m.mu.Lock()
		m.cap = retry
		m.mu.Unlock()
		for resp, err := range m.inner.GenerateContent(ctx, m.clamp(req), stream) {
			if !yield(resp, err) {
				return
			}
		}
	}
}

// clamp is req with its output limit no higher than the learned maximum
// (req itself when that changes nothing; the caller's request is never
// modified).
func (m *outputCapModel) clamp(req *model.LLMRequest) *model.LLMRequest {
	m.mu.Lock()
	limit := m.cap
	m.mu.Unlock()
	if limit == 0 || req == nil || req.Config == nil || req.Config.MaxOutputTokens <= limit {
		return req
	}
	r := *req
	c := *req.Config
	c.MaxOutputTokens = limit
	r.Config = &c
	return &r
}

// outputCapPatterns find a model's maximum output in the error a provider
// returns for a max_tokens above it. The last group is the maximum;
// exclusive marks a bound one past it.
var outputCapPatterns = []struct {
	re        *regexp.Regexp
	exclusive bool
}{
	// Anthropic: "max_tokens: 65536 > 64000, which is the maximum allowed number of output tokens for claude-…"
	{re: regexp.MustCompile(`max_tokens: \d+ > (\d+), which is the maximum`)},
	// OpenAI: "max_tokens is too large: 65536. This model supports at most 16384 completion tokens, …"
	{re: regexp.MustCompile(`(?i)(?:max_tokens|max_output_tokens|max_completion_tokens)[^.]*?\. This model supports at most (\d+)`)},
	// Gemini: "… maxOutputTokens value of 70000 but the supported range is from 1 (inclusive) to 65537 (exclusive)"
	{re: regexp.MustCompile(`(?i)max_?output_?tokens.*supported range is from \d+ \(inclusive\) to (\d+) \(exclusive\)`), exclusive: true},
}

// outputCapFrom is the maximum output an error says the model allows.
func outputCapFrom(err error) (int32, bool) {
	msg := err.Error()
	for _, p := range outputCapPatterns {
		m := p.re.FindStringSubmatch(msg)
		if m == nil {
			continue
		}
		n, convErr := strconv.ParseInt(m[len(m)-1], 10, 32)
		if convErr != nil || n <= 0 {
			continue
		}
		if p.exclusive {
			n--
		}
		return int32(n), true
	}
	return 0, false
}
