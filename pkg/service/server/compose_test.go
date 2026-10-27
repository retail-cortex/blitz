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
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/config/configtest"
	"github.com/retail-cortex/blitz/pkg/engine/compose"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The composer over the API: an unknown kind is refused, a model with
// nothing to say is the service's trouble, an ended session isn't found,
// and dropping one is fine.
func TestComposeOverTheAPI(t *testing.T) {
	c, _ := serve(t, configtest.RunTools)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	dir, _ := filepath.EvalSymlinks(t.TempDir())

	_, err := c.workspaces.StartCompose(ctx, connect.NewRequest(&pb.StartComposeRequest{Workspace: dir, Kind: "spaceship", Request: "x"}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	_, err = c.workspaces.StartCompose(ctx, connect.NewRequest(&pb.StartComposeRequest{Workspace: dir, Kind: "workflow", Request: "a brief"}))
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "the test model has nothing to say: not the service gone")
	_, err = c.workspaces.AnswerCompose(ctx, connect.NewRequest(&pb.AnswerComposeRequest{Workspace: dir, Id: "compose-gone", Answer: "x"}))
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	_, err = c.workspaces.DropCompose(ctx, connect.NewRequest(&pb.DropComposeRequest{Workspace: dir, Id: "compose-gone"}))
	assert.NoError(t, err)
	_, err = c.workspaces.StartCompose(ctx, connect.NewRequest(&pb.StartComposeRequest{Workspace: "relative", Kind: "workflow"}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

// A session's message: its turns, whether it asks, and a workflow draft
// with the workflow the canvas opens.
func TestComposeMsg(t *testing.T) {
	s := compose.Session{ID: "compose-1", Kind: "workflow", Turns: []compose.Turn{{Role: "user", Text: "a brief"}, {Role: "composer", Text: "Where?", Options: []string{"here"}}}}
	m := composeMsg(s)
	assert.True(t, m.Asking)
	assert.Equal(t, []string{"here"}, m.Turns[1].Options)
	assert.Nil(t, m.Draft)

	s.Draft = &compose.Draft{Name: "brief", Summary: "A brief.", File: "nodes:\n  - {id: a, type: accumulate}\n"}
	s.Turns = append(s.Turns, compose.Turn{Role: "composer", Text: "A brief."})
	m = composeMsg(s)
	assert.False(t, m.Asking)
	require.NotNil(t, m.Draft.Workflow)
	assert.Equal(t, "brief", m.Draft.Workflow.Name)
	require.Len(t, m.Draft.Workflow.Nodes, 1)
	assert.Equal(t, "accumulate", m.Draft.Workflow.Nodes[0].Type)
}
