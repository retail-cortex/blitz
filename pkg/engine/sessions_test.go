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
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionOperations(t *testing.T) {
	w, _ := openTestWith(t, nil, text("noted"))
	_, err := w.SaveSnapshot("early", false)
	assert.ErrorIs(t, err, api.ErrNoActiveSession, "snapshot without a session: %v", err)
	_, err = w.RenameSession("x")
	assert.ErrorIs(t, err, api.ErrNoActiveSession, "rename without a session: %v", err)
	first, err := w.NewSession()
	require.NoError(t, err, "new: %+v", first)
	require.Equal(t, "blitz", first.Agent, "new: %+v %v", first, err)
	require.Equal(t, w.Dir(), first.Workspace, "new: %+v %v", first, err)
	_, runErr := w.Run(context.Background(), first.ID, api.Turn{Text: "remember pineapple"}, ignore)
	require.NoError(t, runErr)
	s, _ := w.ActiveSession()
	assert.Equal(t, "remember pineapple", s.Title, "active: %+v", s)
	assert.Len(t, s.Messages, 2, "active: %+v", s)
	assert.Equal(t, "remember pineapple", s.Messages[0].Text, "active: %+v", s)
	_, emptyErr := w.RenameSession(" ")
	assert.Error(t, emptyErr, "an empty title is refused")
	renamed, renameErr := w.RenameSession("fruit talk")
	assert.NoError(t, renameErr, "rename")
	assert.Equal(t, "fruit talk", renamed.Title)

	snap, err := w.SaveSnapshot("fruit", false)
	require.NoError(t, err, "snapshot: %+v", snap)
	require.Equal(t, "fruit", snap.Snapshot, "snapshot: %+v %v", snap, err)
	require.Equal(t, 2, snap.MessageCount, "snapshot: %+v %v", snap, err)
	require.Equal(t, first.ID, snap.From, "snapshot: %+v %v", snap, err)
	_, takenErr := w.SaveSnapshot("fruit", false)
	assert.ErrorIs(t, takenErr, api.ErrSnapshotNameTaken)
	s, _ = w.ActiveSession()
	assert.Equal(t, first.ID, s.ID, "saving a snapshot switched sessions")

	branch, branched, err := w.LoadSession("fruit")
	require.NoError(t, err, "load snapshot: %+v %v", branch, branched)
	require.True(t, branched, "load snapshot: %+v %v %v", branch, branched, err)
	require.NotEqual(t, first.ID, branch.ID, "load snapshot: %+v %v %v", branch, branched, err)
	require.NotEqual(t, snap.ID, branch.ID, "load snapshot: %+v %v %v", branch, branched, err)
	require.Len(t, branch.Messages, 2, "load snapshot: %+v %v %v", branch, branched, err)
	back, branched, err := w.LoadSession(first.ID)
	require.NoError(t, err, "load by id: %+v %v", back, branched)
	require.False(t, branched, "load by id: %+v %v %v", back, branched, err)
	require.Equal(t, first.ID, back.ID, "load by id: %+v %v %v", back, branched, err)
	_, _, unknownErr := w.LoadSession("nope")
	assert.Error(t, unknownErr, "an unknown session doesn't load")

	list, err := w.ListSessions(false)
	assert.NoError(t, err, "list: %d sessions,", len(list))
	assert.Len(t, list, 3, "list: %d sessions, %v", len(list), err)
	assert.Nil(t, list[0].Messages, "list: %d sessions, %v", len(list), err)
}
