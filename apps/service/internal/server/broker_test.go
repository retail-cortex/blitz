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
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/config"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func call(name string, args map[string]any) *genai.Content {
	return &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: name, Args: args}}}}
}

// runTurn runs a turn, answering each approval request with decide and each
// question with answer, and returns the tool results and the final event.
func runTurn(t *testing.T, c clients, dir, prompt string, decide pb.Decision, answer string) ([]*pb.ToolResult, *pb.TurnFinished) {
	t.Helper()
	ctx := context.Background()
	s, err := c.sessions.NewSession(ctx, connect.NewRequest(&pb.NewSessionRequest{Workspace: dir}))
	require.NoError(t, err)
	stream, err := c.sessions.RunTurn(ctx, connect.NewRequest(&pb.RunTurnRequest{Workspace: dir, SessionId: s.Msg.Session.Id, Turn: &pb.Turn{Text: prompt}}))
	require.NoError(t, err)
	var results []*pb.ToolResult
	var finished *pb.TurnFinished
	for stream.Receive() {
		ev := stream.Msg().Event
		switch {
		case ev.GetApprovalRequest() != nil:
			ar := ev.GetApprovalRequest()
			assert.Equal(t, pb.ActionKind_ACTION_KIND_WRITE, ar.Kind, "approval request %v", ar)
			assert.NotEqual(t, "", ar.Detail, "approval request %v", ar)
			assert.NotEqual(t, "", ar.RequestId, "approval request %v", ar)
			_, err := c.sessions.Approve(ctx, connect.NewRequest(&pb.ApproveRequest{Workspace: dir, RequestId: ar.RequestId, Decision: decide}))
			assert.NoError(t, err, "approve")
		case ev.GetQuestion() != nil:
			q := ev.GetQuestion()
			_, err := c.sessions.Answer(ctx, connect.NewRequest(&pb.AnswerRequest{Workspace: dir, RequestId: q.RequestId, Answer: answer}))
			assert.NoError(t, err, "answer")
		case ev.GetToolResult() != nil:
			results = append(results, ev.GetToolResult())
		case ev.GetFinished() != nil:
			finished = ev.GetFinished()
		}
	}
	require.NoError(t, stream.Err())
	return results, finished
}

func TestApprovalsTravelOverTheStream(t *testing.T) {
	create := call("create_file", map[string]any{"path": "made.txt", "content": "hi\n"})
	c, _ := serve(t, func(cfg *config.Config) { cfg.Blitz.AutoApprove = false }, create, text("done"), create, text("done"))
	dir := t.TempDir()

	// Approved once: the file is written.
	_, fin := runTurn(t, c, dir, "make a file", pb.Decision_DECISION_ONCE, "")
	require.NotNil(t, fin, "finished %v", fin)
	require.Nil(t, fin.Error, "finished %v", fin)
	_, err := os.Stat(filepath.Join(dir, "made.txt"))
	require.NoError(t, err, "approved write didn't happen")
	os.Remove(filepath.Join(dir, "made.txt"))

	// Denied: the tool reports the refusal and nothing is written.
	results, _ := runTurn(t, c, dir, "make it again", pb.Decision_DECISION_DENY, "")
	require.Len(t, results, 1, "results %v", results)
	_, err = os.Stat(filepath.Join(dir, "made.txt"))
	assert.ErrorIs(t, err, fs.ErrNotExist, "denied write happened")
}

func TestQuestionsTravelOverTheStream(t *testing.T) {
	ask := call("ask_user_question", map[string]any{"question": "Tabs or spaces?", "options": []any{"tabs", "spaces"}})
	c, _ := serve(t, nil, ask, text("ok"))
	results, _ := runTurn(t, c, t.TempDir(), "ask me", pb.Decision_DECISION_UNSPECIFIED, "tabs")
	assert.Len(t, results, 1, "results %v", results)
	assert.Equal(t, "tabs", results[0].Result.AsMap()["answer"], "results %v", results)
}

func TestAnsweringAnUnknownRequestFails(t *testing.T) {
	c, _ := serve(t, nil)
	_, err := c.sessions.Approve(context.Background(), connect.NewRequest(&pb.ApproveRequest{Workspace: t.TempDir(), RequestId: "nope", Decision: pb.Decision_DECISION_ONCE}))
	code, info := errorReason(t, err)
	assert.Equal(t, connect.CodeNotFound, code, "%v %v", code, info)
	assert.Equal(t, "UNKNOWN_REQUEST", info.Reason, "%v %v", code, info)
	_, err = c.sessions.Approve(context.Background(), connect.NewRequest(&pb.ApproveRequest{RequestId: "nope"}))
	code, info = errorReason(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, code, "%v %v", code, info)
	assert.Equal(t, "INVALID_DECISION", info.Reason, "%v %v", code, info)
}

// Outside a turn nobody can answer, so a request is refused at once.
func TestRequestsWithoutAClientAreRefused(t *testing.T) {
	b := newBroker()
	_, err := b.question(context.Background(), "q", nil)
	assert.ErrorIs(t, err, errNoClient, "err = %v", err)
}

// A client that goes away while an approval waits ends the turn; nothing
// stays pending.
func TestCancellingWhileAnApprovalWaits(t *testing.T) {
	create := call("create_file", map[string]any{"path": "made.txt", "content": "hi\n"})
	c, s := serve(t, func(cfg *config.Config) { cfg.Blitz.AutoApprove = false }, create, text("done"))
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sess, _ := c.sessions.NewSession(ctx, connect.NewRequest(&pb.NewSessionRequest{Workspace: dir}))
	stream, err := c.sessions.RunTurn(ctx, connect.NewRequest(&pb.RunTurnRequest{Workspace: dir, SessionId: sess.Msg.Session.Id, Turn: &pb.Turn{Text: "make a file"}}))
	require.NoError(t, err)
	for stream.Receive() {
		if stream.Msg().Event.GetApprovalRequest() != nil {
			cancel()
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.broker.mu.Lock()
		n := len(s.broker.pending)
		s.broker.mu.Unlock()
		if n == 0 {
			break
		}
		require.False(t, time.Now().After(deadline), "%d requests still pending", n)
		time.Sleep(10 * time.Millisecond)
	}
	_, err = os.Stat(filepath.Join(dir, "made.txt"))
	assert.ErrorIs(t, err, fs.ErrNotExist, "the write happened without approval")
}

// A workspace isn't closed while a turn runs in it: that would cut the
// turn off, maybe another client's.
func TestCloseWaitsForRunningTurns(t *testing.T) {
	c, s := serve(t, nil, call("create_file", map[string]any{"path": "a.txt", "content": "a"}), genai.NewContentFromText("ok", genai.RoleModel))
	dir := t.TempDir()
	ctx := context.Background()
	sess, err := c.sessions.NewSession(ctx, connect.NewRequest(&pb.NewSessionRequest{Workspace: dir}))
	require.NoError(t, err)
	stream, err := c.sessions.RunTurn(ctx, connect.NewRequest(&pb.RunTurnRequest{Workspace: dir, SessionId: sess.Msg.Session.Id, Turn: &pb.Turn{Text: "go"}}))
	require.NoError(t, err)
	for stream.Receive() {
		ev := stream.Msg().Event
		if ar := ev.GetApprovalRequest(); ar != nil { // the turn waits here
			_, cerr := c.workspaces.CloseWorkspace(ctx, connect.NewRequest(&pb.CloseWorkspaceRequest{Workspace: dir}))
			code, info := errorReason(t, cerr)
			assert.Equal(t, connect.CodeFailedPrecondition, code, "closing during a turn: %v %v", code, info)
			assert.Equal(t, "TURN_RUNNING", info.Reason, "closing during a turn: %v %v", code, info)
			c.sessions.Approve(ctx, connect.NewRequest(&pb.ApproveRequest{Workspace: dir, RequestId: ar.RequestId, Decision: pb.Decision_DECISION_DENY}))
		}
	}
	_, err = c.workspaces.CloseWorkspace(ctx, connect.NewRequest(&pb.CloseWorkspaceRequest{Workspace: dir}))
	require.NoError(t, err, "closing after the turn")
	got := s.openDirs()
	assert.Len(t, got, 0, "still open: %v", got)
}
