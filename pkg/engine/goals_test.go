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
	"context"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// A goal sends the agent on until the judge says it holds, or can't, or
// the limit is reached (spec_parity_027 PAR-SES-30).
func TestGoals(t *testing.T) {
	tests := []struct {
		name     string
		max      int
		replies  []*genai.Content
		calls    int
		notices  []string
		kept     bool
		errorish bool
	}{
		{
			name: "met after one more turn",
			replies: []*genai.Content{
				text("tests fail"), text(`{"verdict": "unmet", "reason": "a test fails", "next": "Fix TestX."}`),
				text("fixed"), text("```json\n{\"verdict\": \"met\", \"reason\": \"tests pass\"}\n```"),
			},
			calls:   4,
			notices: []string{"Goal not met yet (1/20): a test fails", "✓ Goal met: tests pass"},
		},
		{
			name:    "impossible",
			replies: []*genai.Content{text("I need a key"), text(`{"verdict": "impossible", "reason": "it needs the API key"}`)},
			calls:   2,
			notices: []string{"The goal can't be reached: it needs the API key"},
		},
		{
			name: "the limit",
			max:  1,
			replies: []*genai.Content{
				text("a"), text(`{"verdict": "unmet", "reason": "no"}`), text("b"), text(`{"verdict": "unmet", "reason": "still no"}`),
			},
			calls:   4,
			notices: []string{"Goal not met yet (1/1): no", "The goal isn't met after 1 more turns"},
		},
		{
			name:    "a judge that can't answer",
			replies: []*genai.Content{text("done"), text("I think so")},
			calls:   2,
			notices: []string{"The goal couldn't be judged"},
			kept:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, llm := openTestWith(t, func(c *config.Config) {
				if tt.max > 0 {
					c.Blitz.GoalMaxContinues = tt.max
				}
			}, tt.replies...)
			s, err := w.NewSession()
			require.NoError(t, err)
			_, err = w.Goal()
			assert.ErrorIs(t, err, api.ErrNoGoal)
			g, err := w.SetGoal("  all tests pass ")
			require.NoError(t, err)
			assert.Equal(t, "all tests pass", g.Condition)

			var notices []string
			_, err = w.Run(context.Background(), s.ID, api.Turn{Text: "work"}, func(e api.Event) {
				if e.Notice != nil {
					notices = append(notices, e.Notice.Text)
				}
			})
			require.NoError(t, err)
			assert.Equal(t, tt.calls, llm.Calls())
			require.Len(t, notices, len(tt.notices))
			for i, want := range tt.notices {
				assert.Contains(t, notices[i], want)
			}
			_, err = w.Goal()
			assert.Equal(t, tt.kept, err == nil, "the goal stays only when it couldn't be judged")
		})
	}
}

func TestGoalContinuationIsRecorded(t *testing.T) {
	w, llm := openTestWith(t, nil,
		text("a"), text(`{"verdict": "unmet", "reason": "not yet", "next": "Run the tests."}`), text("b"), text(`{"verdict": "met", "reason": "ok"}`))
	s, _ := w.NewSession()
	_, err := w.SetGoal("green build")
	require.NoError(t, err)
	_, err = w.Run(context.Background(), s.ID, api.Turn{Text: "go"}, func(api.Event) {})
	require.NoError(t, err)
	third := userTextAt(llm.Requests[2].Contents)
	assert.Contains(t, third, "The goal isn't met yet: not yet")
	assert.Contains(t, third, "The goal: green build")
	assert.Contains(t, third, "Run the tests.")
	judge := userTextAt(llm.Requests[1].Contents)
	assert.Contains(t, judge, "The goal:\ngreen build")

	require.ErrorIs(t, w.ClearGoal(), api.ErrNoGoal, "met: already gone")
	_, err = w.SetGoal(" ")
	assert.Error(t, err)
}

// Goals belong to the active session: without one there are none; a goal
// is cleared once; the default limit applies when none is set.
func TestGoalsNeedASession(t *testing.T) {
	w, _ := openTestWith(t, func(c *config.Config) { c.Blitz.GoalMaxContinues = 0 })
	_, err := w.SetGoal("tests pass")
	assert.ErrorIs(t, err, api.ErrNoActiveSession)
	_, err = w.Goal()
	assert.ErrorIs(t, err, api.ErrNoActiveSession)
	assert.ErrorIs(t, w.ClearGoal(), api.ErrNoActiveSession)

	newSession(t, w)
	g, err := w.SetGoal("tests pass")
	require.NoError(t, err)
	assert.Equal(t, 20, g.Max)
	require.NoError(t, w.ClearGoal())
	assert.ErrorIs(t, w.ClearGoal(), api.ErrNoGoal)
}
