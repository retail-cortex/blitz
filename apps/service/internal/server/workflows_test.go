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
		{Id: "run", Type: "script", Skill: "pdf", Script: "render", Args: []string{"--a4"}, RetryAttempts: 3, RetryDelay: "2s", Timeout: "1m"},
		{Id: "pull", Type: "extract", Fields: []*pb.FlowField{{Name: "total", Type: "number", Description: "The sum"}}},
		{Id: "ask", Type: "exclusive"},
		{Id: "fix", Type: "accumulate"},
	}, Edges: []*pb.FlowEdge{{From: "notes", To: "draft"}, {From: "draft", To: "run"}, {From: "run", To: "fix", Error: true}, {From: "run", To: "pull"}, {From: "pull", To: "ask"},
		{From: "ask", To: "fix", When: "output.data.total > 1", Label: "big"}, {From: "ask", To: "draft2", If: "it's small"}}}
	flow.Nodes = append(flow.Nodes, &pb.FlowNode{Id: "draft2", Type: "accumulate"})
	saved, err := w.SaveWorkflow(ctx, connect.NewRequest(&pb.SaveWorkflowRequest{Workspace: dir, Workflow: flow}))
	require.NoError(t, err)
	require.Empty(t, saved.Msg.Problems)
	got := saved.Msg.Workflow
	assert.Equal(t, filepath.Join(dir, ".agents", "workflows", "brief.flow.yaml"), got.Path)
	assert.Empty(t, got.Problem)
	assert.Equal(t, 10.0, got.Nodes[0].X)
	assert.Equal(t, "render", got.Nodes[2].Script)
	assert.Equal(t, []string{"--a4"}, got.Nodes[2].Args)
	assert.Len(t, got.Edges, 7)
	assert.Equal(t, int32(3), got.Nodes[2].RetryAttempts)
	assert.Equal(t, "1m", got.Nodes[2].Timeout)
	assert.Equal(t, "number", got.Nodes[3].Fields[0].Type)
	assert.True(t, got.Edges[2].Error)
	assert.Equal(t, "big", got.Edges[5].Label)
	assert.Equal(t, "it's small", got.Edges[6].If)

	draft, err := w.SaveWorkflow(ctx, connect.NewRequest(&pb.SaveWorkflowRequest{Workspace: dir, Workflow: &pb.WorkflowFile{Name: "draft", Nodes: []*pb.FlowNode{{Id: "a", Type: "agent"}}}}))
	require.NoError(t, err)
	require.Empty(t, draft.Msg.Problems, "a step not filled in is saved")
	assert.Contains(t, draft.Msg.Workflow.Problem, "say what it's to do")
	bad, err := w.SaveWorkflow(ctx, connect.NewRequest(&pb.SaveWorkflowRequest{Workspace: dir, Workflow: &pb.WorkflowFile{Name: "loop", Nodes: []*pb.FlowNode{{Id: "a", Type: "accumulate"}, {Id: "b", Type: "accumulate"}}, Edges: []*pb.FlowEdge{{From: "a", To: "b"}, {From: "b", To: "a"}}}}))
	require.NoError(t, err)
	assert.Contains(t, bad.Msg.Problems[0], "loop")
	assert.Equal(t, "WORKFLOW_NOT_FOUND", reason(t, unary(w.SaveWorkflow, &pb.SaveWorkflowRequest{Workspace: dir, PreviousName: "gone", Workflow: flow})))
	calls := &pb.WorkflowFile{Name: "calls", Nodes: []*pb.FlowNode{
		{Id: "m", Type: "mcp_call", Server: "shop", Tool: "lookup", ArgumentsJson: `{"order": "{{ input }}"}`},
		{Id: "a", Type: "api_call", Method: "POST", Url: "https://api.example.com/x", Query: []*pb.FlowPair{{Name: "q", Value: "1"}}, Headers: []*pb.FlowPair{{Name: "X-A", Value: "b"}},
			JsonBody: `{"n": 1}`, Auth: &pb.FlowAuth{Type: "oauth2", TokenUrl: "https://auth.example.com/token", ClientId: "id", ClientSecret: "{secrets:s}", Scopes: []string{"read"}}},
		{Id: "r", Type: "a2a_call", Url: "https://agents.example.com"},
	}}
	got2, err := w.SaveWorkflow(ctx, connect.NewRequest(&pb.SaveWorkflowRequest{Workspace: dir, Workflow: calls}))
	require.NoError(t, err)
	require.Empty(t, got2.Msg.Problems)
	assert.Empty(t, got2.Msg.Workflow.Problem)
	assert.JSONEq(t, `{"order": "{{ input }}"}`, got2.Msg.Workflow.Nodes[0].ArgumentsJson)
	assert.Equal(t, "oauth2", got2.Msg.Workflow.Nodes[1].Auth.Type)
	assert.Equal(t, []string{"read"}, got2.Msg.Workflow.Nodes[1].Auth.Scopes)
	assert.JSONEq(t, `{"n": 1}`, got2.Msg.Workflow.Nodes[1].JsonBody)
	badJSON, err := w.SaveWorkflow(ctx, connect.NewRequest(&pb.SaveWorkflowRequest{Workspace: dir, Workflow: &pb.WorkflowFile{Name: "bad", Nodes: []*pb.FlowNode{{Id: "m", Type: "mcp_call", ArgumentsJson: "{nope"}}}}))
	require.NoError(t, err)
	assert.Contains(t, badJSON.Msg.Problems[0], "aren't a JSON object")
	require.NoError(t, unary(w.DeleteWorkflow, &pb.DeleteWorkflowRequest{Workspace: dir, Name: "calls"}))
	_, err = w.SaveWorkflow(ctx, connect.NewRequest(&pb.SaveWorkflowRequest{Workspace: dir}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	list, err = w.ListWorkflows(ctx, connect.NewRequest(&pb.ListWorkflowsRequest{Workspace: dir}))
	require.NoError(t, err)
	require.Len(t, list.Msg.Workflows, 2)
	assert.Equal(t, "brief", list.Msg.Workflows[0].Name)

	require.NoError(t, unary(w.DeleteWorkflow, &pb.DeleteWorkflowRequest{Workspace: dir, Name: "brief"}))
	assert.Equal(t, "WORKFLOW_NOT_FOUND", reason(t, unary(w.DeleteWorkflow, &pb.DeleteWorkflowRequest{Workspace: dir, Name: "brief"})))
}
