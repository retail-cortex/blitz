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
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"google.golang.org/genai"
)

// A turn whose last reply stopped at the output limit says so, rather than
// ending as if nothing happened (the reply, or the tool call it was
// writing, was cut off); a cut-off reply the turn went on from doesn't.
func TestTurnCutOffAtTheOutputLimit(t *testing.T) {
	listFiles := toolCall("list_files", map[string]any{})
	nothing := &genai.Content{Role: genai.RoleModel}
	tests := []struct {
		name    string
		replies []*genai.Content
		reasons []genai.FinishReason
		cut     bool
	}{
		{"a tool call cut off, so nothing", []*genai.Content{listFiles, nothing}, []genai.FinishReason{genai.FinishReasonStop, genai.FinishReasonMaxTokens}, true},
		{"text cut off", []*genai.Content{textContent("The spec is")}, []genai.FinishReason{genai.FinishReasonMaxTokens}, true},
		{"cut off, then went on", []*genai.Content{listFiles, textContent("done")}, []genai.FinishReason{genai.FinishReasonMaxTokens, genai.FinishReasonStop}, false},
		{"finished", []*genai.Content{textContent("done")}, []genai.FinishReason{genai.FinishReasonStop}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newEngineWith(t, fixtureOpts{}, tt.replies...)
			f.llm.FinishReasons = tt.reasons
			_, err := collect(t, f.eng, "s", "write the spec")
			if tt.cut {
				assert.ErrorIs(t, err, api.ErrOutputLimit)
				assert.ErrorContains(t, err, "max_tokens")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
