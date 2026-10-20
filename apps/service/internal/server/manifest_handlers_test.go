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
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The Studio handlers (skills, rules, guardrails, evals, workflows,
// human tasks and databases) refuse a workspace that isn't an absolute directory, as
// INVALID_WORKSPACE, before doing anything.
func TestStudioHandlersRefuseARelativeWorkspace(t *testing.T) {
	c, _ := serveEvery(t, nil)
	ctx := context.Background()
	const ws = "relative/dir"
	w := c.workspaces
	for name, call := range map[string]func() error{
		"GetSkillFile":        func() error { return unary(w.GetSkillFile, &pb.GetSkillFileRequest{Workspace: ws}) },
		"SaveSkill":           func() error { return unary(w.SaveSkill, &pb.SaveSkillRequest{Workspace: ws}) },
		"DeleteSkill":         func() error { return unary(w.DeleteSkill, &pb.DeleteSkillRequest{Workspace: ws}) },
		"ReadSkillFile":       func() error { return unary(w.ReadSkillFile, &pb.ReadSkillFileRequest{Workspace: ws}) },
		"WriteSkillFile":      func() error { return unary(w.WriteSkillFile, &pb.WriteSkillFileRequest{Workspace: ws}) },
		"DeleteSkillFile":     func() error { return unary(w.DeleteSkillFile, &pb.DeleteSkillFileRequest{Workspace: ws}) },
		"CopySkill":           func() error { return unary(w.CopySkill, &pb.CopySkillRequest{Workspace: ws}) },
		"RunSkillScript":      func() error { return unary(w.RunSkillScript, &pb.RunSkillScriptRequest{Workspace: ws}) },
		"ListRules":           func() error { return unary(w.ListRules, &pb.ListRulesRequest{Workspace: ws}) },
		"SaveRule":            func() error { return unary(w.SaveRule, &pb.SaveRuleRequest{Workspace: ws}) },
		"DeleteRule":          func() error { return unary(w.DeleteRule, &pb.DeleteRuleRequest{Workspace: ws}) },
		"ReorderRules":        func() error { return unary(w.ReorderRules, &pb.ReorderRulesRequest{Workspace: ws}) },
		"TryRule":             func() error { return unary(w.TryRule, &pb.TryRuleRequest{Workspace: ws}) },
		"ListGuardrails":      func() error { return unary(w.ListGuardrails, &pb.ListGuardrailsRequest{Workspace: ws}) },
		"SaveGuardrail":       func() error { return unary(w.SaveGuardrail, &pb.SaveGuardrailRequest{Workspace: ws}) },
		"DeleteGuardrail":     func() error { return unary(w.DeleteGuardrail, &pb.DeleteGuardrailRequest{Workspace: ws}) },
		"TryGuardrail":        func() error { return unary(w.TryGuardrail, &pb.TryGuardrailRequest{Workspace: ws}) },
		"ListEvals":           func() error { return unary(w.ListEvals, &pb.ListEvalsRequest{Workspace: ws}) },
		"SaveEval":            func() error { return unary(w.SaveEval, &pb.SaveEvalRequest{Workspace: ws}) },
		"DeleteEval":          func() error { return unary(w.DeleteEval, &pb.DeleteEvalRequest{Workspace: ws}) },
		"ListEvalRuns":        func() error { return unary(w.ListEvalRuns, &pb.ListEvalRunsRequest{Workspace: ws}) },
		"DraftEvalCases":      func() error { return unary(w.DraftEvalCases, &pb.DraftEvalCasesRequest{Workspace: ws}) },
		"ListWorkflows":       func() error { return unary(w.ListWorkflows, &pb.ListWorkflowsRequest{Workspace: ws}) },
		"SaveWorkflow":        func() error { return unary(w.SaveWorkflow, &pb.SaveWorkflowRequest{Workspace: ws}) },
		"DeleteWorkflow":      func() error { return unary(w.DeleteWorkflow, &pb.DeleteWorkflowRequest{Workspace: ws}) },
		"AnswerHumanTask":     func() error { return unary(w.AnswerHumanTask, &pb.AnswerHumanTaskRequest{Workspace: ws}) },
		"ListConnectors":      func() error { return unary(w.ListConnectors, &pb.ListConnectorsRequest{Workspace: ws}) },
		"SaveConnector":       func() error { return unary(w.SaveConnector, &pb.SaveConnectorRequest{Workspace: ws}) },
		"DeleteConnector":     func() error { return unary(w.DeleteConnector, &pb.DeleteConnectorRequest{Workspace: ws}) },
		"SetConnectorEnabled": func() error { return unary(w.SetConnectorEnabled, &pb.SetConnectorEnabledRequest{Workspace: ws}) },
		"MakeConnectorGlobal": func() error { return unary(w.MakeConnectorGlobal, &pb.MakeConnectorGlobalRequest{Workspace: ws}) },
		"TestConnector":       func() error { return unary(w.TestConnector, &pb.TestConnectorRequest{Workspace: ws}) },
		"TryConnectorTool":    func() error { return unary(w.TryConnectorTool, &pb.TryConnectorToolRequest{Workspace: ws}) },
		"ReadConnectorSchema": func() error { return unary(w.ReadConnectorSchema, &pb.ReadConnectorSchemaRequest{Workspace: ws}) },
		"DraftConnectorQuery": func() error { return unary(w.DraftConnectorQuery, &pb.DraftConnectorQueryRequest{Workspace: ws}) },
		"RunEval": func() error {
			return streamErr(w.RunEval(ctx, connect.NewRequest(&pb.RunEvalRequest{Workspace: ws, Name: "x"})))
		},
	} {
		t.Run(name, func(t *testing.T) {
			code, info := errorReason(t, call())
			assert.Equal(t, connect.CodeInvalidArgument, code)
			assert.Equal(t, "INVALID_WORKSPACE", info.Reason)
		})
	}
	t.Run("UninstallRegistrySkill", func(t *testing.T) {
		assert.Equal(t, "INVALID_WORKSPACE", reason(t, unary(c.config.UninstallRegistrySkill, &pb.UninstallRegistrySkillRequest{Workspace: ws, Name: "x"})))
	})
}

// Saving without a spec is an invalid argument; deleting or replacing
// what isn't there is not found; a rule that isn't a gate isn't judged.
func TestStudioHandlersRefuseMissingAndUnknown(t *testing.T) {
	c, _ := serveEvery(t, nil)
	dir := t.TempDir()
	w := c.workspaces
	invalidArg := map[string]func() error{
		"SaveSkill":     func() error { return unary(w.SaveSkill, &pb.SaveSkillRequest{Workspace: dir}) },
		"SaveRule":      func() error { return unary(w.SaveRule, &pb.SaveRuleRequest{Workspace: dir}) },
		"SaveGuardrail": func() error { return unary(w.SaveGuardrail, &pb.SaveGuardrailRequest{Workspace: dir}) },
		"SaveEval":      func() error { return unary(w.SaveEval, &pb.SaveEvalRequest{Workspace: dir}) },
		"SaveWorkflow":  func() error { return unary(w.SaveWorkflow, &pb.SaveWorkflowRequest{Workspace: dir}) },
		"TryRule": func() error {
			return unary(w.TryRule, &pb.TryRuleRequest{Workspace: dir, Rule: &pb.RuleSpec{Name: "plain", Text: "Be kind."}})
		},
	}
	for name, call := range invalidArg {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(call()))
		})
	}
	notFound := map[string]func() error{
		"DeleteGuardrail": func() error {
			return unary(w.DeleteGuardrail, &pb.DeleteGuardrailRequest{Workspace: dir, Name: "gone"})
		},
		"SaveGuardrail": func() error {
			return unary(w.SaveGuardrail, &pb.SaveGuardrailRequest{Workspace: dir, PreviousName: "gone", Guardrail: &pb.GuardrailSpec{Name: "new", Stage: "output", Action: "warn", Text: "No swearing."}})
		},
		"DeleteWorkflow": func() error { return unary(w.DeleteWorkflow, &pb.DeleteWorkflowRequest{Workspace: dir, Name: "gone"}) },
		"DeleteEval":     func() error { return unary(w.DeleteEval, &pb.DeleteEvalRequest{Workspace: dir, Name: "gone"}) },
		"DeleteRule":     func() error { return unary(w.DeleteRule, &pb.DeleteRuleRequest{Workspace: dir, Name: "gone"}) },
		"DeleteSkill":    func() error { return unary(w.DeleteSkill, &pb.DeleteSkillRequest{Workspace: dir, Name: "gone"}) },
		"GetSkillFile":   func() error { return unary(w.GetSkillFile, &pb.GetSkillFileRequest{Workspace: dir, Name: "gone"}) },
		"AnswerHumanTask": func() error {
			return unary(w.AnswerHumanTask, &pb.AnswerHumanTaskRequest{Workspace: dir, TaskId: "r/s"})
		},
		"GetSkillReview": func() error {
			return unary(c.config.GetSkillReview, &pb.GetSkillReviewRequest{Dir: filepath.Join(dir, "none")})
		},
	}
	for name, call := range notFound {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, connect.CodeNotFound, connect.CodeOf(call()))
		})
	}
}

// A workflow's form, arguments and JSON body travel as JSON text: what
// isn't JSON is a problem, and a saved form comes back as JSON.
func TestWorkflowJSONFields(t *testing.T) {
	c, _ := serveEvery(t, nil)
	ctx := context.Background()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	w := c.workspaces
	for _, n := range []*pb.FlowNode{
		{Id: "ask", Type: "human_task", FormJson: "{not json"},
		{Id: "call", Type: "mcp_call", ArgumentsJson: "[1"},
		{Id: "post", Type: "api_call", Url: "https://example.com", JsonBody: "{"},
	} {
		t.Run(n.Id, func(t *testing.T) {
			res, err := w.SaveWorkflow(ctx, connect.NewRequest(&pb.SaveWorkflowRequest{Workspace: dir, Workflow: &pb.WorkflowFile{Name: "bad-" + n.Id, Nodes: []*pb.FlowNode{n}}}))
			if err != nil {
				assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
				return
			}
			assert.NotEmpty(t, res.Msg.Problems)
		})
	}

	writeFlowFile(t, dir, "ask", `nodes:
  - id: ask
    type: human_task
    form:
      components:
        - {id: root, component: Text, text: Hello}
  - {id: wait, type: timer, wait: 1m, retry: {attempts: 2, delay: 1s}}
edges: [{from: ask, to: wait}]
`)
	list, err := w.ListWorkflows(ctx, connect.NewRequest(&pb.ListWorkflowsRequest{Workspace: dir}))
	require.NoError(t, err)
	var form, wait string
	for _, f := range list.Msg.Workflows {
		for _, n := range f.Nodes {
			if n.Id == "ask" {
				form = n.FormJson
			}
			if n.Id == "wait" {
				wait = n.Wait
			}
		}
	}
	assert.Contains(t, form, `"root"`)
	assert.Equal(t, "1m", wait)
}

// The settings handlers report a settings file that can't be read as an
// invalid argument.
func TestConfigHandlersWithABrokenSettingsFile(t *testing.T) {
	c, _ := serveEvery(t, nil)
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".blitz"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".blitz", ".env.toml"), []byte("[[[ not toml"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".blitz", "skill-registries.toml"), []byte("[[[ not toml"), 0o600))
	dir := t.TempDir()
	cf := c.config
	for name, call := range map[string]func() error{
		"DescribeConfig":        func() error { return unary(cf.DescribeConfig, &pb.DescribeConfigRequest{}) },
		"DescribePermissions":   func() error { return unary(cf.DescribePermissions, &pb.DescribePermissionsRequest{}) },
		"DescribePermissionsWs": func() error { return unary(cf.DescribePermissions, &pb.DescribePermissionsRequest{Workspace: dir}) },
		"RemovePermission":      func() error { return unary(cf.RemovePermission, &pb.RemovePermissionRequest{Rule: "shell:ls"}) },
		"GetInterfaceLanguage":  func() error { return unary(cf.GetInterfaceLanguage, &pb.GetInterfaceLanguageRequest{}) },
		"SetReadOnlyDefaults":   func() error { return unary(cf.SetReadOnlyDefaults, &pb.SetReadOnlyDefaultsRequest{Value: "sometimes"}) },
		"ListAllWorkers":        func() error { return unary(c.workers.ListAllWorkers, &pb.ListAllWorkersRequest{}) },
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(call()))
		})
	}
}

// The registries file that can't be read is reported, not hidden.
func TestSkillRegistriesWithABrokenFile(t *testing.T) {
	c, _ := serveEvery(t, nil)
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".blitz"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".blitz", "skill-registries.toml"), []byte("[[[ not toml"), 0o600))
	assert.Error(t, unary(c.config.ListSkillRegistries, &pb.ListSkillRegistriesRequest{}))
}

// A skill's inline script is reviewed as "inline".
func TestSkillReviewOfAnInlineScript(t *testing.T) {
	c, _ := serveEvery(t, nil)
	dir := filepath.Join(t.TempDir(), "tool")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: tool\ndescription: d\nscripts:\n  - name: hi\n    language: python\n    inline_code: \"print(1)\"\n---\n"), 0o644))
	got, err := c.config.GetSkillReview(context.Background(), connect.NewRequest(&pb.GetSkillReviewRequest{Dir: dir}))
	require.NoError(t, err)
	require.NotNil(t, got.Msg.Review)
	require.Len(t, got.Msg.Review.Scripts, 1)
	assert.Equal(t, "inline", got.Msg.Review.Scripts[0].Source)
}

// Forgetting a workspace's workers needs an absolute directory.
func TestForgetWorkspaceWorkersNeedsAnAbsoluteDirectory(t *testing.T) {
	c, _ := serveEvery(t, nil)
	assert.Equal(t, "INVALID_WORKSPACE", reason(t, unary(c.workers.ForgetWorkspaceWorkers, &pb.ForgetWorkspaceWorkersRequest{Workspace: "relative/dir"})))
	assert.NoError(t, unary(c.workers.ForgetWorkspaceWorkers, &pb.ForgetWorkspaceWorkersRequest{Workspace: filepath.Join(t.TempDir(), "gone")}), "a folder that's gone is forgotten")
}

// Without a scheduler, the running runs can't be listed or stopped.
func TestRunningWorkerRunsNeedAScheduler(t *testing.T) {
	_, s := serve(t, nil)
	srv := httptestServer(t, s)
	wk := pb.NewWorkerServiceClient(srv.Client(), srv.URL)
	for name, call := range map[string]func() error{
		"ListRunningWorkerRuns": func() error { return unary(wk.ListRunningWorkerRuns, &pb.ListRunningWorkerRunsRequest{}) },
		"StopWorkerRun":         func() error { return unary(wk.StopWorkerRun, &pb.StopWorkerRunRequest{RunId: "x"}) },
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, "NO_SCHEDULER", reason(t, call()))
		})
	}
}

func httptestServer(t *testing.T, s *Server) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return srv
}
