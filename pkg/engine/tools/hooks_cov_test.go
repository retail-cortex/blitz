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

package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/audit"
)

// cmdReq is a shell command's approval request.
func cmdReq(key string) api.ApprovalRequest {
	return api.ApprovalRequest{Tool: "run_shell_command", Kind: api.ActionCommand, Detail: "ls", Key: key, Targets: []string{"ls"}}
}

// TestHooksSessionRulesAndStore checks approvals remembered for the session
// are listed sorted, and the attached store is returned.
func TestHooksSessionRulesAndStore(t *testing.T) {
	h, _ := decisionHooks(api.DecisionSession)
	assert.Nil(t, h.Store())
	require.NoError(t, h.Approve(context.Background(), cmdReq("b")))
	require.NoError(t, h.Approve(context.Background(), cmdReq("a")))
	assert.Equal(t, []string{"a", "b"}, h.SessionRules())

	store, err := OpenApprovalStore(filepath.Join(t.TempDir(), "a.json"))
	require.NoError(t, err)
	h.SetStore(store)
	assert.Same(t, store, h.Store())

	var nilHooks *Hooks
	assert.Nil(t, nilHooks.Audit())
	assert.ErrorIs(t, nilHooks.Approve(context.Background(), cmdReq("")), ErrNotApproved)
}

// TestHooksAlwaysSaveFails checks an "always" answer whose rule can't be
// saved still allows the action, for the session, and the store doesn't
// claim a rule it couldn't save.
func TestHooksAlwaysSaveFails(t *testing.T) {
	h, reqs := decisionHooks(api.DecisionAlways)
	parent := filepath.Join(t.TempDir(), "later-a-file")
	store, err := OpenApprovalStore(filepath.Join(parent, "a.json"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(parent, nil, 0o600)) // saving can't make the directory
	h.SetStore(store)
	var notices []string
	ctx := WithNotices(context.Background(), func(m string) { notices = append(notices, m) })
	require.NoError(t, h.Approve(ctx, cmdReq("k")), "the user approved it")
	require.Len(t, notices, 1, "the user is told (N26 engine-tools-1#5)")
	assert.Contains(t, notices[0], "saving the rule failed")
	assert.False(t, store.Has("k"), "a rule that wasn't saved")
	assert.Empty(t, store.Rules())
	require.NoError(t, h.Approve(context.Background(), cmdReq("k")))
	assert.Len(t, *reqs, 1, "remembered for the session")
}

// TestHooksNotify checks notification hooks fire for Notify and when the
// user is about to be asked.
func TestHooksNotify(t *testing.T) {
	h, _ := decisionHooks(api.DecisionOnce)
	h.Notify(context.Background(), "x", "nothing attached") // no hooks: no-op
	var got []string
	h.SetEventHooks(nil, func(_ context.Context, typ, msg string) { got = append(got, typ+":"+msg) })
	h.Notify(context.Background(), "idle", "waiting")
	require.NoError(t, h.Approve(context.Background(), cmdReq("")))
	assert.Equal(t, []string{"idle:waiting", "permission_prompt:ls"}, got)
}

// TestHooksPermissionRequestHook checks a permission_request hook's allow and
// deny answer for the user, and that no answer falls through to the user.
func TestHooksPermissionRequestHook(t *testing.T) {
	for _, tc := range []struct {
		out     Outcome
		mustAsk bool
		wantErr string
		asked   int
	}{
		{Outcome{Decision: "allow"}, false, "", 0},
		{Outcome{Decision: "deny", Reason: "not today"}, false, "not today", 0},
		{Outcome{Decision: "deny"}, false, "a permission_request hook denied it", 0},
		{Outcome{}, false, "", 1},
		// What an ask rule says the user must see, the hook may refuse but
		// not allow (N26 engine-tools-1#3).
		{Outcome{Decision: "allow"}, true, "", 1},
		{Outcome{Decision: "deny"}, true, "a permission_request hook denied it", 0},
	} {
		t.Run(fmt.Sprintf("%s%s must-ask=%v", tc.out.Decision, tc.out.Reason, tc.mustAsk), func(t *testing.T) {
			h, reqs := decisionHooks(api.DecisionOnce)
			h.SetEventHooks(func(context.Context, api.ApprovalRequest) Outcome { return tc.out }, nil)
			req := cmdReq("")
			req.MustAsk = tc.mustAsk
			err := h.Approve(context.Background(), req)
			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Len(t, *reqs, tc.asked)
		})
	}
}

// TestHooksContextRouting checks a context's own mode, task asker and
// unattended permissions are what decide.
func TestHooksContextRouting(t *testing.T) {
	h, reqs := decisionHooks(api.DecisionDeny)
	ctx := context.Background()

	assert.NoError(t, h.Approve(WithMode(ctx, api.ModeBypass), cmdReq("")), "the context's bypass mode")
	assert.Empty(t, *reqs)

	asked := 0
	asker := TaskAsker{Approve: func(context.Context, api.ApprovalRequest) (api.Decision, error) {
		asked++
		return api.DecisionOnce, nil
	}}
	assert.NoError(t, h.Approve(WithTaskAsker(Background(ctx), asker), cmdReq("")), "the task's asker answers")
	assert.Equal(t, 1, asked)
	assert.Empty(t, *reqs, "the workspace's approver isn't asked")

	permit := func(context.Context, api.ApprovalRequest) (api.Decision, error) { return api.DecisionOnce, nil }
	assert.NoError(t, h.Approve(Unattended(ctx, permit), cmdReq("")), "an unattended run's permissions")
}

// TestHooksAskRuleWithoutApprover checks an ask rule with nobody to ask
// is refused, saying why.
func TestHooksAskRuleWithoutApprover(t *testing.T) {
	h := NewHooks(Policy{})
	rules, err := NewPermissionRules(config.PermissionsConfig{Ask: []string{"write(*.lock)"}}, "test")
	require.NoError(t, err)
	h.SetRules(rules)
	req := api.ApprovalRequest{Tool: "create_file", Kind: api.ActionWrite, Detail: "go.lock", Targets: []string{"go.lock"}}
	assert.ErrorContains(t, h.Approve(context.Background(), req), "must be asked about")
}

// TestHooksRefusalsAuditedAsDenials checks every refusing decision is
// logged as a denial (N26 engine-tools-1#4).
func TestHooksRefusalsAuditedAsDenials(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mode     api.PermissionMode
		hook     string
		ctx      func(context.Context) context.Context
		decision string
	}{
		{"dont-ask", api.ModeDontAsk, "", nil, "mode-dont-ask"},
		{"hook deny", api.ModeDefault, "deny", nil, "hook-deny"},
		{"background", api.ModeDefault, "", Background, "background-refused"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := decisionHooks(api.DecisionOnce)
			if tc.hook != "" {
				h.SetEventHooks(func(context.Context, api.ApprovalRequest) Outcome { return Outcome{Decision: tc.hook} }, nil)
			}
			var got []audit.Entry
			ctx := WithDecisionObserver(WithMode(context.Background(), tc.mode), func(e audit.Entry) { got = append(got, e) })
			if tc.ctx != nil {
				ctx = tc.ctx(ctx)
			}
			require.ErrorIs(t, h.Approve(ctx, cmdReq("")), ErrNotApproved)
			require.Len(t, got, 1)
			assert.Equal(t, tc.decision, got[0].Decision)
			assert.Equal(t, audit.KindDenial, got[0].Kind)
		})
	}
}
