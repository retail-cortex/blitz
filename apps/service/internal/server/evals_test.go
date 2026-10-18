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
	"google.golang.org/genai"
)

func TestEvalsOverTheAPI(t *testing.T) {
	c, _ := serve(t, nil, genai.NewContentFromText("4", genai.RoleModel), genai.NewContentFromText("five", genai.RoleModel))
	w := c.workspaces
	ctx := context.Background()
	dir, _ := filepath.EvalSymlinks(t.TempDir())

	list, err := w.ListEvals(ctx, connect.NewRequest(&pb.ListEvalsRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.Empty(t, list.Msg.Evals)
	assert.Equal(t, filepath.Join(dir, ".agents", "evals"), list.Msg.Dir)

	spec := &pb.EvalSpec{Name: "sums", Description: "Arithmetic", Cases: []*pb.EvalCase{
		{Id: "a", Prompt: "2+2?", Output: "4", Type: "exact"},
		{Id: "b", Prompt: "2+3?", Output: "5", Type: "exact", Scope: "warn"},
	}}
	saved, err := w.SaveEval(ctx, connect.NewRequest(&pb.SaveEvalRequest{Workspace: dir, Eval: spec}))
	require.NoError(t, err)
	require.Empty(t, saved.Msg.Problems)
	assert.Equal(t, "fail", saved.Msg.Eval.Cases[0].Scope, "the default scope")
	bad, err := w.SaveEval(ctx, connect.NewRequest(&pb.SaveEvalRequest{Workspace: dir, Eval: &pb.EvalSpec{Name: "x", Cases: []*pb.EvalCase{{Id: "a"}}}}))
	require.NoError(t, err)
	assert.NotEmpty(t, bad.Msg.Problems)
	assert.Equal(t, "EVAL_NOT_FOUND", reason(t, unary(w.SaveEval, &pb.SaveEvalRequest{Workspace: dir, PreviousName: "gone", Eval: spec})))

	stream, err := w.RunEval(ctx, connect.NewRequest(&pb.RunEvalRequest{Workspace: dir, Name: "sums"}))
	require.NoError(t, err)
	var kinds []string
	var finished *pb.EvalRun
	for stream.Receive() {
		switch k := stream.Msg().Event.(type) {
		case *pb.RunEvalResponse_Started:
			kinds = append(kinds, "started")
		case *pb.RunEvalResponse_Result:
			kinds = append(kinds, "result:"+k.Result.Status)
		case *pb.RunEvalResponse_Finished:
			kinds = append(kinds, "finished")
			finished = k.Finished
		}
	}
	require.NoError(t, stream.Err())
	assert.Equal(t, []string{"started", "result:pass", "result:warn", "finished"}, kinds)
	require.NotNil(t, finished)
	assert.Equal(t, "passed", finished.Status)
	assert.Equal(t, int32(1), finished.Counts["warn"])
	assert.Equal(t, "five", finished.Results[1].Output)

	list, err = w.ListEvals(ctx, connect.NewRequest(&pb.ListEvalsRequest{Workspace: dir}))
	require.NoError(t, err)
	require.Len(t, list.Msg.Evals, 1)
	require.NotNil(t, list.Msg.Evals[0].LastRun)
	assert.Equal(t, "passed", list.Msg.Evals[0].LastRun.Status)
	assert.Empty(t, list.Msg.Evals[0].LastRun.Results, "a list carries no results")
	runs, err := w.ListEvalRuns(ctx, connect.NewRequest(&pb.ListEvalRunsRequest{Workspace: dir, Name: "sums"}))
	require.NoError(t, err)
	require.Len(t, runs.Msg.Runs, 1)
	assert.Len(t, runs.Msg.Runs[0].Results, 2)

	missing, err := w.RunEval(ctx, connect.NewRequest(&pb.RunEvalRequest{Workspace: dir, Name: "gone"}))
	require.NoError(t, err)
	for missing.Receive() {
	}
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(missing.Err()))
	unrunnable, err := w.RunEval(ctx, connect.NewRequest(&pb.RunEvalRequest{Workspace: dir, Name: "sums", Cases: []string{"zz"}}))
	require.NoError(t, err)
	for unrunnable.Receive() {
	}
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(unrunnable.Err()))

	require.NoError(t, unary(w.DeleteEval, &pb.DeleteEvalRequest{Workspace: dir, Name: "sums"}))
	assert.Equal(t, "EVAL_NOT_FOUND", reason(t, unary(w.DeleteEval, &pb.DeleteEvalRequest{Workspace: dir, Name: "sums"})))
}
