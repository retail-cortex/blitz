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

package engine

import (
	"testing"

	"github.com/retail-cortex/blitz/pkg/redact"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	adkmodel "google.golang.org/adk/v2/model"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// Reasoning a provider signed is kept as it was written, for the model to
// read back; a secret in it is masked where it's shown (spec_056 CA-29).
func TestEventsMaskReasoning(t *testing.T) {
	redact.Remember("sk-event-test-secret-1")
	part := &genai.Part{Text: "the key is sk-event-test-secret-1", Thought: true, ThoughtSignature: []byte("signed")}
	ev := &adksession.Event{Author: "blitz", LLMResponse: adkmodel.LLMResponse{Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{part}}}}
	out := events(ev)
	require.Len(t, out, 1)
	require.NotNil(t, out[0].Text)
	assert.True(t, out[0].Text.Thought)
	assert.NotContains(t, out[0].Text.Text, "sk-event-test-secret-1")
	assert.Equal(t, "the key is sk-event-test-secret-1", part.Text, "the stored reasoning is unchanged")
}
