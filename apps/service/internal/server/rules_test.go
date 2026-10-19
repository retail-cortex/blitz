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

package server

import (
	"context"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Studio's rules over the API: save gates and an instruction rule, list
// them in the order they run, reorder, try one (a judge that can't answer
// is a problem), delete (spec_gates_041).
func TestRulesOverTheAPI(t *testing.T) {
	c, _ := serve(t, nil)
	w := c.workspaces
	ctx := context.Background()
	dir, _ := filepath.EvalSymlinks(t.TempDir())

	bad, err := w.SaveRule(ctx, connect.NewRequest(&pb.SaveRuleRequest{Workspace: dir, Rule: &pb.RuleSpec{Name: "Bad", When: "later"}}))
	require.NoError(t, err)
	assert.Len(t, bad.Msg.Problems, 4)
	for _, s := range []*pb.RuleSpec{
		{Name: "polite", When: "before_agent", Action: "warn", Text: "Be polite."},
		{Name: "tested", When: "after_agent", Action: "revise", Agents: []string{"qa"}, Text: "Say how it was tested."},
		{Name: "no-prod", When: "before_agent", Action: "block", Text: "Never touch production."},
		{Name: "go-style", Paths: []string{"*.go"}, Text: "Go style."},
	} {
		got, err := w.SaveRule(ctx, connect.NewRequest(&pb.SaveRuleRequest{Workspace: dir, Rule: s}))
		require.NoError(t, err)
		require.Empty(t, got.Msg.Problems, "%v", s)
		assert.True(t, got.Msg.Rule.Editable)
	}
	require.NoError(t, unary(w.ReorderRules, &pb.ReorderRulesRequest{Workspace: dir, Names: []string{"polite", "no-prod"}}))
	list, err := w.ListRules(ctx, connect.NewRequest(&pb.ListRulesRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, ".agents", "rules"), list.Msg.Dir)
	var names []string
	for _, r := range list.Msg.Rules {
		names = append(names, r.Name)
	}
	assert.Equal(t, []string{"polite", "no-prod", "tested", "go-style"}, names)
	assert.Equal(t, int32(10), list.Msg.Rules[0].Order)
	assert.Equal(t, []string{"*.go"}, list.Msg.Rules[3].Paths)

	tried, err := w.TryRule(ctx, connect.NewRequest(&pb.TryRuleRequest{Workspace: dir, Rule: &pb.RuleSpec{Name: "polite", When: "before_agent", Text: "Be polite."}, Sample: "do it"}))
	require.NoError(t, err)
	assert.NotEmpty(t, tried.Msg.Problem, "the mock judge has nothing to say")
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(unary(w.TryRule, &pb.TryRuleRequest{Workspace: dir, Rule: &pb.RuleSpec{Name: "x", Text: "x"}})), "an instruction rule isn't judged")

	require.NoError(t, unary(w.DeleteRule, &pb.DeleteRuleRequest{Workspace: dir, Name: "polite"}))
	assert.Equal(t, "RULE_NOT_FOUND", reason(t, unary(w.DeleteRule, &pb.DeleteRuleRequest{Workspace: dir, Name: "polite"})))
	assert.Equal(t, "RULE_NOT_FOUND", reason(t, unary(w.ReorderRules, &pb.ReorderRulesRequest{Workspace: dir, Names: []string{"polite"}})))
}
