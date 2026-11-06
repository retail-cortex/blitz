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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A session's ledger keeps each call as it was charged, survives a torn
// line, goes with the session, and isn't a fork's or a snapshot's (what
// was spent getting there is the source's): spec_056 CA-120.
func TestLedger(t *testing.T) {
	s, dir := newStorage(t)
	rec, err := s.CreateSession("ledger-1", "t", "blitz")
	require.NoError(t, err)
	require.NoError(t, s.AddMessage("user", "hello"))

	calls, err := s.Calls(rec.ID)
	require.NoError(t, err)
	assert.Empty(t, calls, "a session from before the ledger has none")

	when := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	first := api.Call{Time: when, API: "gemini", Provider: "gemini", Model: "gemini-3-pro", Purpose: api.PurposeTurn,
		Usage: api.Usage{Calls: 1, Input: 1000, Output: 100, CostUSD: 0.01, Priced: true}, Price: &api.Price{Input: 2, Output: 12}}
	second := api.Call{Time: when.Add(time.Minute), Model: "llama-local", Purpose: api.PurposeTask, Task: "task-1", Usage: api.Usage{Calls: 1, Unpriced: 1}}
	require.NoError(t, s.AppendCall(rec.ID, first))
	f, err := os.OpenFile(filepath.Join(dir, rec.ID+usageSuffix), os.O_APPEND|os.O_WRONLY, filePerm)
	require.NoError(t, err)
	_, err = f.WriteString(`{"time": "torn`)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, s.AppendCall(rec.ID, second))

	calls, err = s.Calls(rec.ID)
	require.NoError(t, err)
	require.Len(t, calls, 1, "the torn line, and the call written after it on the same line, are skipped")
	assert.Equal(t, first, calls[0])

	fork, err := s.Fork(rec.ID)
	require.NoError(t, err)
	calls, err = s.Calls(fork.ID)
	require.NoError(t, err)
	assert.Empty(t, calls, "a fork starts its own ledger")

	require.NoError(t, s.AppendCall("no-such-session", first), "a session this store doesn't have is left alone")
	_, err = os.Stat(filepath.Join(dir, "no-such-session"+usageSuffix))
	assert.ErrorIs(t, err, os.ErrNotExist)
	assert.Error(t, s.AppendCall("../bad", first))
	_, err = s.Calls("../bad")
	assert.Error(t, err)

	s.Release()
	require.NoError(t, s.Delete(rec.ID))
	_, err = os.Stat(filepath.Join(dir, rec.ID+usageSuffix))
	assert.ErrorIs(t, err, os.ErrNotExist, "deleting a session deletes its ledger")
}
