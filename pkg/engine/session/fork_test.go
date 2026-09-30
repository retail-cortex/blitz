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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForkGetAndReadEvents(t *testing.T) {
	st, _, rec, dir := conversation(t)

	fork, err := st.Fork(rec.ID)
	require.NoError(t, err)
	assert.Equal(t, rec.ID, fork.From)
	assert.Equal(t, fork.ID, st.Active().ID, "the copy is active")

	got, err := st.Get(rec.ID)
	require.NoError(t, err)
	assert.Len(t, got.Messages, 2)

	events, err := ReadEvents(dir, fork.ID)
	require.NoError(t, err)
	require.Len(t, events, 2, "the copy has the event log")
	assert.Equal(t, "remember pineapple", events[0].Content.Parts[0].Text)

	none, err := ReadEvents(dir, NewSessionID())
	require.NoError(t, err)
	assert.Nil(t, none)
	_, err = ReadEvents(dir, "../bad")
	assert.Error(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, fork.ID+eventsSuffix), []byte("{not json\n"), 0o600))
	_, err = ReadEvents(dir, fork.ID)
	assert.ErrorContains(t, err, "event 1")
	_, err = st.Get("missing-session")
	assert.Error(t, err)
}
