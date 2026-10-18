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
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/engine/flows"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// Studio's workflows (spec_workflows_043).

func workflowMsg(f flows.Flow) *pb.WorkflowFile {
	m := &pb.WorkflowFile{Name: f.Name, Path: f.Path, Description: f.Description, Problem: f.Problem}
	for _, n := range f.Nodes {
		pn := &pb.FlowNode{Id: n.ID, Type: n.Type, Title: n.Title, X: n.Position.X, Y: n.Position.Y, Agent: n.Agent, Skills: n.Skills,
			Instructions: n.Instructions, Rules: n.Rules, Skill: n.Skill, Script: n.ScriptName, Args: n.Args, Path: n.Path, Timeout: n.Timeout}
		for _, fl := range n.Fields {
			pn.Fields = append(pn.Fields, &pb.FlowField{Name: fl.Name, Type: fl.Type, Description: fl.Description})
		}
		if n.Retry != nil {
			pn.RetryAttempts, pn.RetryDelay = int32(n.Retry.Attempts), n.Retry.Delay
		}
		pn.Server, pn.Tool, pn.Url, pn.Method, pn.Body = n.Server, n.Tool, n.URL, n.Method, n.Body
		pn.ArgumentsJson, pn.JsonBody = jsonText(n.Arguments), jsonText(n.JSON)
		pn.Workflow, pn.ForEach, pn.Concurrency = n.Workflow, n.ForEach, int32(n.Concurrency)
		for _, p := range n.Query {
			pn.Query = append(pn.Query, &pb.FlowPair{Name: p.Name, Value: p.Value})
		}
		for _, p := range n.Headers {
			pn.Headers = append(pn.Headers, &pb.FlowPair{Name: p.Name, Value: p.Value})
		}
		if a := n.Auth; a != nil {
			pn.Auth = &pb.FlowAuth{Type: a.Type, Token: a.Token, Username: a.Username, Password: a.Password, Name: a.Name, Value: a.Value, In: a.In,
				TokenUrl: a.TokenURL, ClientId: a.ClientID, ClientSecret: a.ClientSecret, Scopes: a.Scopes, Audience: a.Audience}
		}
		m.Nodes = append(m.Nodes, pn)
	}
	for _, e := range f.Edges {
		m.Edges = append(m.Edges, &pb.FlowEdge{From: e.From, To: e.To, When: e.When, If: e.If, Default: e.Default, Error: e.Error, Label: e.Label})
	}
	return m
}

// jsonText is v as JSON text ("" for nothing).
func jsonText(v any) string {
	if v == nil {
		return ""
	}
	if m, ok := v.(map[string]any); ok && len(m) == 0 {
		return ""
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return ""
	}
	return string(data)
}

// workflowOf is a workflow from the canvas, or what's wrong with its JSON.
func workflowOf(m *pb.WorkflowFile) (flows.Flow, []string) {
	f := flows.Flow{Name: m.GetName(), Description: m.GetDescription()}
	var probs []string
	for _, n := range m.GetNodes() {
		fn := flows.Node{ID: n.GetId(), Type: n.GetType(), Title: n.GetTitle(), Position: flows.Position{X: n.GetX(), Y: n.GetY()}, Agent: n.GetAgent(), Skills: n.GetSkills(),
			Instructions: n.GetInstructions(), Rules: n.GetRules(), Skill: n.GetSkill(), ScriptName: n.GetScript(), Args: n.GetArgs(), Path: n.GetPath(), Timeout: n.GetTimeout()}
		for _, fl := range n.GetFields() {
			fn.Fields = append(fn.Fields, flows.Field{Name: fl.GetName(), Type: fl.GetType(), Description: fl.GetDescription()})
		}
		if n.GetRetryAttempts() > 1 {
			fn.Retry = &flows.Retry{Attempts: int(n.GetRetryAttempts()), Delay: n.GetRetryDelay()}
		}
		fn.Server, fn.Tool, fn.URL, fn.Method, fn.Body = n.GetServer(), n.GetTool(), n.GetUrl(), n.GetMethod(), n.GetBody()
		fn.Workflow, fn.ForEach, fn.Concurrency = n.GetWorkflow(), n.GetForEach(), int(n.GetConcurrency())
		if s := strings.TrimSpace(n.GetArgumentsJson()); s != "" {
			if err := json.Unmarshal([]byte(s), &fn.Arguments); err != nil {
				probs = append(probs, fmt.Sprintf("step %s: the arguments aren't a JSON object: %v", n.GetId(), err))
			}
		}
		if s := strings.TrimSpace(n.GetJsonBody()); s != "" {
			if err := json.Unmarshal([]byte(s), &fn.JSON); err != nil {
				probs = append(probs, fmt.Sprintf("step %s: the JSON body isn't JSON: %v", n.GetId(), err))
			}
		}
		for _, p := range n.GetQuery() {
			fn.Query = append(fn.Query, flows.Pair{Name: p.GetName(), Value: p.GetValue()})
		}
		for _, p := range n.GetHeaders() {
			fn.Headers = append(fn.Headers, flows.Pair{Name: p.GetName(), Value: p.GetValue()})
		}
		if a := n.GetAuth(); a != nil && a.GetType() != "" {
			fn.Auth = &flows.Auth{Type: a.GetType(), Token: a.GetToken(), Username: a.GetUsername(), Password: a.GetPassword(), Name: a.GetName(), Value: a.GetValue(), In: a.GetIn(),
				TokenURL: a.GetTokenUrl(), ClientID: a.GetClientId(), ClientSecret: a.GetClientSecret(), Scopes: a.GetScopes(), Audience: a.GetAudience()}
		}
		f.Nodes = append(f.Nodes, fn)
	}
	for _, e := range m.GetEdges() {
		f.Edges = append(f.Edges, flows.Edge{From: e.GetFrom(), To: e.GetTo(), When: e.GetWhen(), If: e.GetIf(), Default: e.GetDefault(), Error: e.GetError(), Label: e.GetLabel()})
	}
	return f, probs
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
	f, bad := workflowOf(r.Msg.Workflow)
	if len(bad) > 0 {
		return ok(&pb.SaveWorkflowResponse{Problems: bad})
	}
	saved, problems, err := w.SaveWorkflow(f, r.Msg.PreviousName)
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
