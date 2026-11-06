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

package client

import (
	"context"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// A conversation's own model and its ledger, through the service
// (spec_056 CA-100, CA-125, CA-130).
func TestRemoteSessionModelAndLedger(t *testing.T) {
	r := attach(t, func(c *config.Config) {
		c.Pricing = map[string]config.ModelPrice{"claude-x": {InputPerMTok: 3, OutputPerMTok: 15}}
	})
	ctx := context.Background()
	s, err := r.NewSession()
	require.NoError(t, err)
	_, err = r.Run(ctx, s.ID, api.Turn{Text: "hi"}, func(api.Event) {})
	require.NoError(t, err)

	p, err := r.PreviewSessionModel(ctx, s.ID, "anthropic/claude-x")
	require.NoError(t, err)
	assert.Equal(t, "anthropic/claude-x", p.Model)
	assert.True(t, p.Priced)
	moved, err := r.SetSessionModel(ctx, s.ID, "anthropic/claude-x")
	require.NoError(t, err)
	assert.Equal(t, p, moved)
	cur, ok := r.ActiveSession()
	require.True(t, ok)
	assert.Equal(t, "anthropic/claude-x", cur.Model)

	_, err = r.SetSessionModel(ctx, "session-20200101-000000-00000000", "x")
	assert.ErrorIs(t, err, api.ErrSessionNotFound)
	_, err = r.PreviewSessionModel(ctx, "../bad", "x")
	assert.ErrorIs(t, err, api.ErrSessionNotFound)

	_, err = r.Run(ctx, s.ID, api.Turn{Text: "again"}, func(api.Event) {})
	require.NoError(t, err)
	calls, totals, err := r.SessionLedger()
	require.NoError(t, err)
	require.NotEmpty(t, calls)
	assert.NotEmpty(t, totals.ByModel)
	assert.NotEmpty(t, totals.ByPurpose)
	_, _, err = r.SessionLedgerOf("session-20200101-000000-00000000")
	assert.Error(t, err)
}

// The conversions keep every field, and nothing breaks on what's missing.
func TestLedgerConversions(t *testing.T) {
	assert.Equal(t, api.ModelPreview{}, modelPreview(nil))
	assert.Equal(t, api.ModelPreview{Model: "m", Tokens: 9, CostUSD: 0.5, Priced: true, Compacts: true, Unseen: []string{"a.mp4"}, NoTools: true, ActivePin: "p"},
		modelPreview(&pb.ModelPreview{Model: "m", Tokens: 9, CostUsd: 0.5, Priced: true, Compacts: true, Unseen: []string{"a.mp4"}, NoTools: true, ActivePin: "p"}))

	when := timestamppb.Now()
	c := usageCall(&pb.UsageCall{Api: "anthropic", Provider: "bedrock", Model: "claude-x", Purpose: "turn", Task: "t1", Time: when,
		Price: &pb.Price{Input: 3, CachedInput: 0.3, CacheWrite: 3.75, Output: 15}, Usage: &pb.Usage{}})
	assert.Equal(t, "bedrock", c.Provider)
	assert.Equal(t, when.AsTime(), c.Time)
	require.NotNil(t, c.Price)
	assert.Equal(t, 3.75, c.Price.CacheWrite)
	bare := usageCall(&pb.UsageCall{Model: "m"})
	assert.Nil(t, bare.Price)
	assert.True(t, bare.Time.IsZero())

	groups := usageGroups([]*pb.UsageGroup{{Name: "claude-x", Usage: &pb.Usage{Calls: 2}}})
	require.Len(t, groups, 1)
	assert.Equal(t, "claude-x", groups[0].Name)
	assert.EqualValues(t, 2, groups[0].Usage.Calls)

	assert.Equal(t, "anthropic/claude-x", session(&pb.SessionInfo{Id: "s", Model: "anthropic/claude-x"}).Model)
}
