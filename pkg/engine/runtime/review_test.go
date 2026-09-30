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
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func TestParseVerdict(t *testing.T) {
	cases := []struct {
		name    string
		answer  string
		allowed bool
		reason  string
		bad     bool
	}{
		{name: "allow", answer: `{"decision": "allow", "reason": "it's the task"}`, allowed: true, reason: "it's the task"},
		{name: "deny", answer: `{"decision":"deny","reason":"deletes the home directory"}`, reason: "deletes the home directory"},
		{name: "in a code fence", answer: "```json\n{\"decision\": \"Allow\", \"reason\": \"ok\"}\n```", allowed: true, reason: "ok"},
		{name: "not JSON", answer: "sure, go ahead", bad: true},
		{name: "another word", answer: `{"decision": "maybe"}`, bad: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			allowed, reason, err := parseVerdict(c.answer)
			if c.bad {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.allowed, allowed)
			assert.Equal(t, c.reason, reason)
		})
	}
}

// The reviewer sees the request and the environment, and its tokens count
// for the session.
func TestReviewAsksTheModel(t *testing.T) {
	reviewer := NewMockLLM("reviewer", genai.NewContentFromText(`{"decision":"deny","reason":"outside the task"}`, genai.RoleModel))
	reviewer.Usage = &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 200, CandidatesTokenCount: 12}
	f := newEngineWith(t, fixtureOpts{
		cfg:  func(c *config.Config) { c.Permissions.Auto.Environment = "Pushing to github.com/acme is fine." },
		opts: []Option{WithReviewModel(reviewer)},
	})
	ctx := context.WithValue(context.Background(), runStateKey{}, &runState{sessionID: "s"})
	allowed, reason, err := f.eng.review(ctx, api.ApprovalRequest{Tool: "run_shell_command", Kind: api.ActionCommand, Detail: "rm -rf ~/src"})
	require.NoError(t, err)
	assert.False(t, allowed)
	assert.Equal(t, "outside the task", reason)
	require.Len(t, reviewer.Requests, 1)
	sent := reviewer.Requests[0].Contents[0].Parts[0].Text
	assert.Contains(t, sent, "rm -rf ~/src")
	assert.Contains(t, sent, "Pushing to github.com/acme is fine.")
	assert.Equal(t, 1, f.eng.Usage("s").Calls, "the review isn't in the session's usage")
}
