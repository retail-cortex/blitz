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

package session

import (
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A session's metadata is written by the storage that has it active: the
// workspace's storage, asked to record a worker run's turn or usage, hands
// it to the run's, so neither loses the other's writes (BL-WK-20).
func TestOnlyTheOwnerWritesMetadata(t *testing.T) {
	dir := t.TempDir()
	workspace, err := NewStorage(dir)
	require.NoError(t, err)
	run, err := NewStorage(dir)
	require.NoError(t, err)
	rec, err := run.CreateSession("", "⏰ deps", "blitz")
	require.NoError(t, err)
	require.NoError(t, run.AddMessage("user", "check the modules"))

	// The workspace's storage records the run's turn and usage...
	require.NoError(t, workspace.SetLastTurn(rec.ID, "00-trace-span-01", 1))
	require.NoError(t, workspace.SetUsage(rec.ID, api.Usage{Calls: 2, Priced: true}))
	// ...while the run goes on writing its own.
	require.NoError(t, run.AddMessage("model", "all current"))

	tp, index := workspace.LastTurn(rec.ID)
	assert.Equal(t, "00-trace-span-01", tp)
	assert.Equal(t, 1, index)
	u, ok := run.Usage(rec.ID)
	require.True(t, ok)
	assert.Equal(t, 2, u.Calls)

	// On disk, everything is there together.
	fresh, err := NewStorage(dir)
	require.NoError(t, err)
	loaded, err := fresh.Load(rec.ID)
	require.NoError(t, err)
	require.NotNil(t, loaded.LastTurn)
	assert.Equal(t, 1, loaded.LastTurn.Index)
	require.NotNil(t, loaded.Usage)
	assert.Equal(t, 2, loaded.Usage.Calls)
	assert.Equal(t, 2, loaded.MessageCount)
	assert.Equal(t, "⏰ deps", loaded.Title)
}
