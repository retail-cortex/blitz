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
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The workspace lists every provider's chat models with their facts, keeps
// the list until Refresh or a provider change, and names the providers
// not set up (spec_061_models MOD-01, MOD-04, MOD-06).
func TestWorkspaceListModels(t *testing.T) {
	w := openTest(t)
	ctx := context.Background()
	calls := 0
	was := listModels
	t.Cleanup(func() { listModels = was })
	listModels = func(context.Context, *config.Config, ...string) []runtime.ProviderModels {
		calls++
		return []runtime.ProviderModels{
			{Provider: "gemini", Models: []runtime.ListedModel{
				{ID: "gemini-3.8-flash", Price: &config.ModelPrice{InputPerMTok: 0.75, OutputPerMTok: 3.75}},
				{ID: "imagen-4.0-generate-001"},
				{ID: "gemini-4-carbon"},
			}},
			{Provider: "anthropic", Err: errors.New("no API key")},
		}
	}
	list, err := w.ListModels(ctx, false)
	require.NoError(t, err)
	require.Len(t, list.Providers, 2)
	g := list.Providers[0]
	assert.Equal(t, "gemini", g.Provider)
	require.Len(t, g.Models, 2, "chat models only")
	assert.Equal(t, "gemini-4-carbon", g.Models[0].ID, "newest first")
	assert.Equal(t, api.ListedModel{ID: "gemini-3.8-flash", HasPrice: true, InputPerMTok: 0.75, OutputPerMTok: 3.75}, g.Models[1])
	assert.Equal(t, "gemini/gemini-3.8-flash", g.Models[1].Ref("gemini"))
	assert.Equal(t, "no API key", list.Providers[1].Error)
	assert.Equal(t, []string{"openai", "ollama"}, list.NotSetUp)

	_, err = w.ListModels(ctx, false)
	require.NoError(t, err)
	assert.Equal(t, 1, calls, "kept")
	_, err = w.ListModels(ctx, true)
	require.NoError(t, err)
	assert.Equal(t, 2, calls, "Refresh asks again")
	require.NoError(t, w.ReloadProviders(ctx, w.settings()))
	_, err = w.ListModels(ctx, false)
	require.NoError(t, err)
	assert.Equal(t, 3, calls, "a provider change asks again")
}
