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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// The maximum output each provider's rejection states, and none from other
// errors.
func TestOutputCapFrom(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want int32
		ok   bool
	}{
		{"Anthropic", "POST /v1/messages: 400 invalid_request_error: max_tokens: 65536 > 64000, which is the maximum allowed number of output tokens for claude-sonnet-4-5", 64000, true},
		{"OpenAI", "max_tokens is too large: 65536. This model supports at most 16384 completion tokens, whereas you provided 65536.", 16384, true},
		{"Gemini", "Error 400: Unable to submit request because it has a maxOutputTokens value of 70000 but the supported range is from 1 (inclusive) to 65537 (exclusive).", 65536, true},
		{"another 400", "max_tokens must be a positive integer", 0, false},
		{"unrelated", "connection reset by peer", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := outputCapFrom(errors.New(tt.msg))
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

// capLimitedModel rejects max_tokens above limit as Anthropic does, and
// records what each request asked for; failAfter, if set, fails after a
// first response.
type capLimitedModel struct {
	limit     int32
	asked     []int32
	failAfter bool
}

func (m *capLimitedModel) Name() string { return "capped" }

func (m *capLimitedModel) GenerateContent(_ context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		asked := req.Config.MaxOutputTokens
		m.asked = append(m.asked, asked)
		if m.failAfter {
			if !yield(&model.LLMResponse{Content: genai.NewContentFromText("part", genai.RoleModel)}, nil) {
				return
			}
		}
		if asked > m.limit {
			yield(nil, fmt.Errorf("400: max_tokens: %d > %d, which is the maximum allowed number of output tokens", asked, m.limit))
			return
		}
		yield(&model.LLMResponse{Content: genai.NewContentFromText("ok", genai.RoleModel)}, nil)
	}
}

func generate(t *testing.T, m model.LLM, max int32) (string, error) {
	t.Helper()
	req := &model.LLMRequest{Config: &genai.GenerateContentConfig{MaxOutputTokens: max}}
	var text string
	var err error
	for resp, e := range m.GenerateContent(context.Background(), req, false) {
		if e != nil {
			err = e
			continue
		}
		text += resp.Content.Parts[0].Text
	}
	assert.Equal(t, max, req.Config.MaxOutputTokens, "the caller's request was changed")
	return text, err
}

// A max_tokens above the model's maximum is sent again at the maximum the
// rejection states, which later requests use from the start; a request
// within it goes as asked.
func TestOutputCapRetriesAtTheModelsMaximum(t *testing.T) {
	inner := &capLimitedModel{limit: 64000}
	m := capOutput(inner)
	text, err := generate(t, m, 65536)
	require.NoError(t, err)
	assert.Equal(t, "ok", text)
	assert.Equal(t, []int32{65536, 64000}, inner.asked)

	_, err = generate(t, m, 65536)
	require.NoError(t, err)
	assert.Equal(t, []int32{65536, 64000, 64000}, inner.asked[:3], "learned: no second rejection")

	_, err = generate(t, m, 1000)
	require.NoError(t, err)
	assert.Equal(t, int32(1000), inner.asked[3], "within the maximum: as asked")
}

// Nothing is retried once a response has come (the caller has it), nor
// when the error states no lower maximum.
func TestOutputCapLeavesOtherFailuresAlone(t *testing.T) {
	inner := &capLimitedModel{limit: 64000, failAfter: true}
	_, err := generate(t, capOutput(inner), 65536)
	assert.ErrorContains(t, err, "maximum allowed")
	assert.Equal(t, []int32{65536}, inner.asked)

	assert.Nil(t, capOutput(nil))
	assert.Equal(t, "capped", capOutput(&capLimitedModel{}).Name())
}
