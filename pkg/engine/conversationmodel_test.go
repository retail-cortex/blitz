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
	"errors"
	"sync"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
)

// A conversation moved to a model runs on it from its next turn; the
// workspace's other conversations keep the workspace's model (spec_056
// CA-100, CA-101).
func TestConversationModel(t *testing.T) {
	w, workspaceModel := openTestWith(t, func(c *config.Config) {
		c.Pricing = map[string]config.ModelPrice{"anthropic/claude-x": {InputPerMTok: 3}}
	})
	var mu sync.Mutex
	built := map[string][]*runtime.MockLLM{}
	w.newModel = func(_ context.Context, _ *config.Config, ref string) (model.LLM, error) {
		if ref == "broken" {
			return nil, errors.New("no such model")
		}
		m := runtime.NewMockLLM(ref)
		mu.Lock()
		built[ref] = append(built[ref], m)
		mu.Unlock()
		return m, nil
	}
	calls := func(ref string) int {
		mu.Lock()
		defer mu.Unlock()
		n := 0
		for _, m := range built[ref] {
			n += len(m.Requests)
		}
		return n
	}
	ctx := context.Background()
	moved, err := w.NewSession()
	require.NoError(t, err)
	p, err := w.SetSessionModel(ctx, moved.ID, "anthropic/claude-x") // the open conversation, before its first turn
	require.NoError(t, err)
	other, err := w.NewSession()
	require.NoError(t, err)

	assert.Equal(t, "anthropic/claude-x", p.Model)
	assert.True(t, p.Priced)
	assert.False(t, p.Compacts)
	assert.Equal(t, "anthropic/claude-x", w.SessionModel(moved.ID))
	assert.Empty(t, w.SessionModel(other.ID))

	before := len(workspaceModel.Requests)
	_, err = w.Run(ctx, moved.ID, api.Turn{Text: "hi"}, func(api.Event) {})
	require.NoError(t, err)
	assert.Positive(t, calls("anthropic/claude-x"), "the moved conversation ran on its model")
	assert.Len(t, workspaceModel.Requests, before)

	_, err = w.Run(ctx, other.ID, api.Turn{Text: "hi"}, func(api.Event) {})
	require.NoError(t, err)
	assert.Greater(t, len(workspaceModel.Requests), before, "the other conversation ran on the workspace's model")

	_, err = w.SetSessionModel(ctx, moved.ID, "broken")
	require.Error(t, err)
	assert.Equal(t, "anthropic/claude-x", w.SessionModel(moved.ID), "a model that can't be built doesn't move it")

	_, err = w.SetSessionModel(ctx, moved.ID, "")
	require.NoError(t, err)
	assert.Empty(t, w.SessionModel(moved.ID), "back on the workspace's model")

	_, err = w.SetSessionModel(ctx, "../bad", "x")
	assert.Error(t, err)
}
