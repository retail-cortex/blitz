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
	"errors"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/engine/flows"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// Studio's workflows (spec_workflows_043).

func workflowMsg(f flows.Flow) *pb.WorkflowFile {
	m := &pb.WorkflowFile{Name: f.Name, Path: f.Path, Description: f.Description, Problem: f.Problem}
	for _, n := range f.Nodes {
		m.Nodes = append(m.Nodes, &pb.FlowNode{Id: n.ID, Type: n.Type, Title: n.Title, X: n.Position.X, Y: n.Position.Y, Agent: n.Agent, Skills: n.Skills,
			Instructions: n.Instructions, Rules: n.Rules, Skill: n.Skill, Script: n.ScriptName, Args: n.Args, Path: n.Path})
	}
	for _, e := range f.Edges {
		m.Edges = append(m.Edges, &pb.FlowEdge{From: e.From, To: e.To})
	}
	return m
}

func workflowOf(m *pb.WorkflowFile) flows.Flow {
	f := flows.Flow{Name: m.GetName(), Description: m.GetDescription()}
	for _, n := range m.GetNodes() {
		f.Nodes = append(f.Nodes, flows.Node{ID: n.GetId(), Type: n.GetType(), Title: n.GetTitle(), Position: flows.Position{X: n.GetX(), Y: n.GetY()}, Agent: n.GetAgent(), Skills: n.GetSkills(),
			Instructions: n.GetInstructions(), Rules: n.GetRules(), Skill: n.GetSkill(), ScriptName: n.GetScript(), Args: n.GetArgs(), Path: n.GetPath()})
	}
	for _, e := range m.GetEdges() {
		f.Edges = append(f.Edges, flows.Edge{From: e.GetFrom(), To: e.GetTo()})
	}
	return f
}

func (h workspaceService) ListWorkflows(ctx context.Context, r req[pb.ListWorkflowsRequest]) (*connect.Response[pb.ListWorkflowsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	out := &pb.ListWorkflowsResponse{Dir: w.ProjectWorkflowsDir()}
	for _, f := range w.Workflows() {
		out.Workflows = append(out.Workflows, workflowMsg(f))
	}
	return ok(out)
}

func (h workspaceService) SaveWorkflow(ctx context.Context, r req[pb.SaveWorkflowRequest]) (*connect.Response[pb.SaveWorkflowResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if r.Msg.Workflow == nil {
		return nil, invalid(errors.New("workflow is required"))
	}
	saved, problems, err := w.SaveWorkflow(workflowOf(r.Msg.Workflow), r.Msg.PreviousName)
	if errors.Is(err, flows.ErrUnknown) {
		return nil, apiError(connect.CodeNotFound, "WORKFLOW_NOT_FOUND", err)
	} else if err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		return ok(&pb.SaveWorkflowResponse{Problems: problems})
	}
	return ok(&pb.SaveWorkflowResponse{Workflow: workflowMsg(saved)})
}

func (h workspaceService) DeleteWorkflow(ctx context.Context, r req[pb.DeleteWorkflowRequest]) (*connect.Response[pb.DeleteWorkflowResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.DeleteWorkflow(r.Msg.Name); err != nil {
		return nil, apiError(connect.CodeNotFound, "WORKFLOW_NOT_FOUND", err)
	}
	return ok(&pb.DeleteWorkflowResponse{})
}
