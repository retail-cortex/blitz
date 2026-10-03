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

package client

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"

	"github.com/retail-cortex/blitz/pkg/api"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A notice from the service reaches the front end as api.Event.Notice.
func TestNoticeFromTheService(t *testing.T) {
	got, ok := event(&pb.TurnEvent{Kind: &pb.TurnEvent_Notice{Notice: &pb.Notice{Text: "Couldn't save this message", Error: true}}})
	require.True(t, ok)
	assert.Equal(t, &api.Notice{Text: "Couldn't save this message", Error: true}, got.Notice)
}

// Each typed error the service reports comes back as api's error, with
// its data; an unknown reason keeps the service's message.
func TestErrorsFromTheService(t *testing.T) {
	for reason, check := range map[string]func(*testing.T, error){
		"UNKNOWN_AGENT": func(t *testing.T, err error) {
			var e *api.UnknownAgentError
			require.ErrorAs(t, err, &e)
			assert.Equal(t, "v", e.Name)
		},
		"RESUME_FAILED": func(t *testing.T, err error) {
			var e *api.ResumeError
			assert.ErrorAs(t, err, &e)
		},
		"UNKNOWN_SETTING": func(t *testing.T, err error) {
			var e *api.UnknownSettingError
			require.ErrorAs(t, err, &e)
			assert.Equal(t, "v", e.Key)
		},
		"PROMPT_BLOCKED": func(t *testing.T, err error) {
			var e *api.BlockedError
			require.ErrorAs(t, err, &e)
			assert.Equal(t, "v", e.Reason)
		},
		"UNKNOWN_RUN": func(t *testing.T, err error) {
			assert.ErrorIs(t, err, api.ErrUnknownRun)
			assert.EqualError(t, err, "the message", "the service's message")
		},
		"WORKER_EXISTS": func(t *testing.T, err error) {
			assert.ErrorIs(t, err, api.ErrWorkerExists)
		},
		"OUTPUT_LIMIT": func(t *testing.T, err error) {
			assert.ErrorIs(t, err, api.ErrOutputLimit)
		},
		"NO_EMBEDDINGS": func(t *testing.T, err error) {
			assert.ErrorIs(t, err, api.ErrNoEmbeddings)
		},
		"APPROVALS_STILL_SAVED": func(t *testing.T, err error) {
			assert.ErrorIs(t, err, api.ErrApprovalsStillSaved)
			assert.EqualError(t, err, "the message")
		},
		"SANDBOX_UNAVAILABLE": func(t *testing.T, err error) {
			assert.ErrorIs(t, err, api.ErrSandboxUnavailable)
		},
		"WORKER_INVALID": func(t *testing.T, err error) {
			assert.ErrorIs(t, err, api.ErrWorkerInvalid)
			assert.EqualError(t, err, "the message", "the service's message, with the problems")
		},
		"SOMETHING_NEW": func(t *testing.T, err error) {
			assert.EqualError(t, err, "the message")
		},
	} {
		t.Run(reason, func(t *testing.T) {
			err := errorFromInfo(&pb.ErrorInfo{Reason: reason, Message: "the message", Metadata: map[string]string{"name": "v", "key": "v", "reason": "v"}})
			check(t, err)
		})
	}
	assert.NoError(t, errorFromInfo(nil))
}

// A cancelled or timed-out call keeps its context error, so the CLI
// exits as interrupted; other codes keep the service's message.
func TestErrorsByCode(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{"cancelled here", connect.NewError(connect.CodeCanceled, context.Canceled), context.Canceled},
		{"cancelled by the service", connect.NewError(connect.CodeCanceled, errors.New("stopped")), context.Canceled},
		{"out of time", connect.NewError(connect.CodeDeadlineExceeded, context.DeadlineExceeded), context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.ErrorIs(t, fromAPI(tc.err), tc.want)
		})
	}
	assert.EqualError(t, fromAPI(connect.NewError(connect.CodeInternal, errors.New("broke"))), "broke")
	assert.NoError(t, fromAPI(nil))
	plain := errors.New("not connect")
	assert.Same(t, plain, fromAPI(plain))
}

// Messages the service leaves out convert to zero values, and every
// field it sends arrives.
func TestConversions(t *testing.T) {
	assert.Equal(t, api.SessionInfo{}, session(nil))
	assert.Equal(t, api.Usage{}, usage(nil))
	assert.Equal(t, api.ModelSettingsInfo{}, modelSettingsInfo(nil))
	assert.Equal(t, api.TaskInfo{}, taskInfo(nil))
	assert.Equal(t, api.ProjectSettings{State: api.TrustNone}, projectSettings(nil))
	assert.EqualError(t, saved(&pb.Saved{Path: "p", Error: "read-only"}).Err, "read-only")

	sk := skill(&pb.SkillInfo{Name: "s", Tools: []*pb.SkillTool{{Name: "t"}}, Scripts: []*pb.SkillScript{{Name: "run.py", Language: "python"}}})
	require.Len(t, sk.Tools, 1)
	require.Len(t, sk.Scripts, 1)
	assert.Equal(t, "python", sk.Scripts[0].Language)

	_, ok := event(&pb.TurnEvent{Kind: &pb.TurnEvent_Accepted{Accepted: &pb.Accepted{}}})
	assert.False(t, ok, "not a model event")

	for k, want := range map[pb.ActionKind]api.ActionKind{
		pb.ActionKind_ACTION_KIND_COMMAND:     api.ActionCommand,
		pb.ActionKind_ACTION_KIND_WRITE:       api.ActionWrite,
		pb.ActionKind_ACTION_KIND_DELETE:      api.ActionDelete,
		pb.ActionKind_ACTION_KIND_NETWORK:     api.ActionNetwork,
		pb.ActionKind_ACTION_KIND_MCP:         api.ActionMCP,
		pb.ActionKind_ACTION_KIND_UNSPECIFIED: "",
	} {
		t.Run(k.String(), func(t *testing.T) {
			assert.Equal(t, want, actionKind(k))
		})
	}
	for d, want := range map[api.Decision]pb.Decision{
		api.DecisionOnce:    pb.Decision_DECISION_ONCE,
		api.DecisionSession: pb.Decision_DECISION_SESSION,
		api.DecisionAlways:  pb.Decision_DECISION_ALWAYS,
		api.DecisionDeny:    pb.Decision_DECISION_DENY,
	} {
		t.Run(d.String(), func(t *testing.T) {
			assert.Equal(t, want, decisionMsg(d))
		})
	}
}
