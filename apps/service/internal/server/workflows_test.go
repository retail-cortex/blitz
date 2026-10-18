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

func TestWorkflowsOverTheAPI(t *testing.T) {
	c, _ := serve(t, nil)
	w := c.workspaces
	ctx := context.Background()
	dir, _ := filepath.EvalSymlinks(t.TempDir())

	list, err := w.ListWorkflows(ctx, connect.NewRequest(&pb.ListWorkflowsRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.Empty(t, list.Msg.Workflows)
	assert.Equal(t, filepath.Join(dir, ".agents", "workflows"), list.Msg.Dir)

	flow := &pb.WorkflowFile{Name: "brief", Description: "A brief", Nodes: []*pb.FlowNode{
		{Id: "notes", Type: "read_file", Title: "Notes", X: 10, Y: 20, Path: "notes.md"},
		{Id: "draft", Type: "agent", Instructions: "Write a brief.", Rules: []string{"short"}, Skills: []string{"tone"}},
		{Id: "run", Type: "script", Skill: "pdf", Script: "render", Args: []string{"--a4"}},
	}, Edges: []*pb.FlowEdge{{From: "notes", To: "draft"}, {From: "draft", To: "run"}}}
	saved, err := w.SaveWorkflow(ctx, connect.NewRequest(&pb.SaveWorkflowRequest{Workspace: dir, Workflow: flow}))
	require.NoError(t, err)
	require.Empty(t, saved.Msg.Problems)
	got := saved.Msg.Workflow
	assert.Equal(t, filepath.Join(dir, ".agents", "workflows", "brief.flow.yaml"), got.Path)
	assert.Empty(t, got.Problem)
	assert.Equal(t, 10.0, got.Nodes[0].X)
	assert.Equal(t, "render", got.Nodes[2].Script)
	assert.Equal(t, []string{"--a4"}, got.Nodes[2].Args)
	assert.Len(t, got.Edges, 2)

	draft, err := w.SaveWorkflow(ctx, connect.NewRequest(&pb.SaveWorkflowRequest{Workspace: dir, Workflow: &pb.WorkflowFile{Name: "draft", Nodes: []*pb.FlowNode{{Id: "a", Type: "agent"}}}}))
	require.NoError(t, err)
	require.Empty(t, draft.Msg.Problems, "a step not filled in is saved")
	assert.Contains(t, draft.Msg.Workflow.Problem, "say what it's to do")
	bad, err := w.SaveWorkflow(ctx, connect.NewRequest(&pb.SaveWorkflowRequest{Workspace: dir, Workflow: &pb.WorkflowFile{Name: "loop", Nodes: []*pb.FlowNode{{Id: "a", Type: "accumulate"}, {Id: "b", Type: "accumulate"}}, Edges: []*pb.FlowEdge{{From: "a", To: "b"}, {From: "b", To: "a"}}}}))
	require.NoError(t, err)
	assert.Contains(t, bad.Msg.Problems[0], "loop")
	assert.Equal(t, "WORKFLOW_NOT_FOUND", reason(t, unary(w.SaveWorkflow, &pb.SaveWorkflowRequest{Workspace: dir, PreviousName: "gone", Workflow: flow})))
	_, err = w.SaveWorkflow(ctx, connect.NewRequest(&pb.SaveWorkflowRequest{Workspace: dir}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	list, err = w.ListWorkflows(ctx, connect.NewRequest(&pb.ListWorkflowsRequest{Workspace: dir}))
	require.NoError(t, err)
	require.Len(t, list.Msg.Workflows, 2)
	assert.Equal(t, "brief", list.Msg.Workflows[0].Name)

	require.NoError(t, unary(w.DeleteWorkflow, &pb.DeleteWorkflowRequest{Workspace: dir, Name: "brief"}))
	assert.Equal(t, "WORKFLOW_NOT_FOUND", reason(t, unary(w.DeleteWorkflow, &pb.DeleteWorkflowRequest{Workspace: dir, Name: "brief"})))
}
