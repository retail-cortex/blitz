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
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Agent files added, edited and removed in .agents/agents apply without
// reopening the workspace, models included.
func TestAgentFilesReload(t *testing.T) {
	w := openTest(t)
	ctx := context.Background()
	dir := w.ProjectAgentsDir()
	require.Equal(t, filepath.Join(w.Dir(), ".agents", "agents"), dir)
	write := func(front string) {
		t.Helper()
		require.NoError(t, os.MkdirAll(dir, 0o755))
		path := filepath.Join(dir, "quote.md")
		require.NoError(t, os.WriteFile(path, []byte("---\nname: quote\ndescription: Quotes\ntools: []\n"+front+"---\nYou write quotes.\n"), 0o644))
		later := time.Now().Add(time.Duration(len(front)+1) * time.Minute) // a new time even for the same size
		require.NoError(t, os.Chtimes(path, later, later))
	}
	find := func() (api.AgentInfo, bool) {
		i := slices.IndexFunc(w.ListAgents(), func(a api.AgentInfo) bool { return a.Name == "quote" })
		if i < 0 {
			return api.AgentInfo{}, false
		}
		return w.ListAgents()[i], true
	}

	_, ok := find()
	require.False(t, ok)

	write("")
	info, ok := find()
	require.True(t, ok, "a new file's agent is offered")
	assert.Empty(t, info.PinnedModel)

	write("default_model: claude-haiku-4-5\n")
	info, _ = find()
	assert.Equal(t, "claude-haiku-4-5", info.PinnedModel, "its default_model applies")

	_, err := w.SetAgent(ctx, "quote")
	require.NoError(t, err)
	write("")
	info, _ = find()
	assert.Empty(t, info.PinnedModel, "without default_model it's back on the configured model")
	assert.True(t, info.Active)

	require.NoError(t, os.Remove(filepath.Join(dir, "quote.md")))
	_, ok = find()
	assert.False(t, ok, "a removed file's agent is gone")
	assert.Equal(t, "blitz", w.ActiveAgent().Name, "the active agent falls back to blitz")
}
