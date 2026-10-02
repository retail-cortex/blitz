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

package api

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestErrors checks each error type's message and that the wrapping ones
// unwrap to their reason.
func TestErrors(t *testing.T) {
	reason := errors.New("why")
	cases := map[string]struct {
		err  error
		want string
	}{
		"unknown agent":   {&UnknownAgentError{Name: "qa"}, `unknown agent "qa"`},
		"invalid setting": {&InvalidSettingError{Err: reason}, "why"},
		"unknown setting": {&UnknownSettingError{Key: "k"}, `unknown setting "k"`},
		"blocked":         {&BlockedError{Reason: "no"}, "prompt blocked by hook: no"},
		"resume":          {&ResumeError{Err: reason}, "why"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.EqualError(t, tc.err, tc.want)
		})
	}
	assert.ErrorIs(t, &InvalidSettingError{Err: reason}, reason)
	assert.ErrorIs(t, &ResumeError{Err: reason}, reason)
}

// TestIsLimit checks the three turn limits, wrapped or not, and nothing else.
func TestIsLimit(t *testing.T) {
	assert.True(t, IsLimit(ErrMaxTurns))
	assert.True(t, IsLimit(fmt.Errorf("%w ($0.50)", ErrCostLimit)))
	assert.True(t, IsLimit(ErrTimeLimit))
	assert.False(t, IsLimit(errors.New("other")))
	assert.False(t, IsLimit(nil))
}

// TestUsageAdd checks counts add up, the last prompt size follows the
// newest call that has one, and the total is priced only if both are.
func TestUsageAdd(t *testing.T) {
	u := Usage{Calls: 1, Input: 10, Cached: 2, CacheWrite: 1, Output: 5, LastPrompt: 10, CostUSD: 0.5, Priced: true, SearchQueries: 1, SearchCostUSD: 0.1}
	u.Add(Usage{Calls: 2, Input: 20, Cached: 3, CacheWrite: 2, Output: 7, LastPrompt: 30, CostUSD: 0.25, Priced: true, SearchQueries: 2, SearchCostUSD: 0.2})
	assert.Equal(t, Usage{Calls: 3, Input: 30, Cached: 5, CacheWrite: 3, Output: 12, LastPrompt: 30, CostUSD: 0.75, Priced: true, SearchQueries: 3, SearchCostUSD: 0.30000000000000004}, u)
	u.Add(Usage{Calls: 1})
	assert.Equal(t, int64(30), u.LastPrompt, "a call without a prompt size keeps the last one")
	assert.False(t, u.Priced, "an unpriced call makes the total unpriced")
}

// TestDecisionString checks each decision's name.
func TestDecisionString(t *testing.T) {
	for d, want := range map[Decision]string{DecisionDeny: "deny", DecisionOnce: "once", DecisionSession: "session", DecisionAlways: "always", Decision(9): "deny"} {
		t.Run(want, func(t *testing.T) {
			assert.Equal(t, want, d.String())
		})
	}
}

// TestMultiSelect checks the context mark.
func TestMultiSelect(t *testing.T) {
	assert.False(t, IsMultiSelect(context.Background()))
	assert.True(t, IsMultiSelect(WithMultiSelect(context.Background())))
}

// TestParsePermissionMode checks every spelling, Claude Code's included,
// and that an unknown name is refused.
func TestParsePermissionMode(t *testing.T) {
	cases := map[string]PermissionMode{
		"": ModeDefault, "ask": ModeDefault, "manual": ModeDefault,
		"accept-edits": ModeAcceptEdits, "acceptEdits": ModeAcceptEdits, "edits": ModeAcceptEdits,
		"plan": ModePlan, "dont-ask": ModeDontAsk, "dontAsk": ModeDontAsk, "deny": ModeDontAsk,
		"auto": ModeAuto, " bypass ": ModeBypass, "bypassPermissions": ModeBypass,
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			got, err := ParsePermissionMode(in)
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
	_, err := ParsePermissionMode("yolo")
	assert.ErrorIs(t, err, ErrUnknownMode)
}

// TestSmallHelpers checks the small methods on the API's values.
func TestSmallHelpers(t *testing.T) {
	assert.False(t, SkillInfo{Scripts: []SkillScript{{Allowed: false}}}.Runnable())
	assert.True(t, SkillInfo{Scripts: []SkillScript{{}, {Allowed: true}}}.Runnable())

	assert.True(t, BackgroundRun{State: "waiting"}.Active())
	assert.False(t, BackgroundRun{State: "done"}.Active())

	assert.Equal(t, []string{"a", "b"}, WebSearch{Links: []Link{{URL: "a"}, {URL: "b"}}}.URLs())

	assert.True(t, TaskInfo{State: TaskWaiting}.Active())
	assert.False(t, TaskInfo{State: TaskDone}.Active())
	start := time.Now().Add(-time.Hour)
	assert.Equal(t, time.Minute, TaskInfo{Started: start, Ended: start.Add(time.Minute)}.Runtime())
	assert.GreaterOrEqual(t, TaskInfo{Started: start}.Runtime(), time.Hour, "a running task runs until now")

	assert.Equal(t, "qa · task-3", TaskRequest{Agent: "qa", TaskID: "task-3"}.Label())
}
