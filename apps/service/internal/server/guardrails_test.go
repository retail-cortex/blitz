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

// Studio's guardrails over the API: the built-ins on by default, one tuned
// and switched off, a custom one, try samples, reset one (spec_gates_041).
func TestGuardrailsOverTheAPI(t *testing.T) {
	c, _ := serve(t, nil)
	w := c.workspaces
	ctx := context.Background()
	dir, _ := filepath.EvalSymlinks(t.TempDir())

	list, err := w.ListGuardrails(ctx, connect.NewRequest(&pb.ListGuardrailsRequest{Workspace: dir}))
	require.NoError(t, err)
	require.Len(t, list.Msg.Builtins, 3)
	assert.Equal(t, "prompt_injection", list.Msg.Builtins[0].Id)
	injection := list.Msg.Builtins[0].File
	require.NotNil(t, injection, "on by default")
	assert.Empty(t, injection.Path, "its defaults, no file")
	assert.Equal(t, "warn", injection.Action)
	assert.True(t, injection.Editable)
	assert.False(t, injection.Off)
	assert.Equal(t, filepath.Join(dir, ".agents", "guardrails"), list.Msg.Dir)

	for _, s := range []*pb.GuardrailSpec{
		{Builtin: "limits", Action: "block", Limits: &pb.GuardrailLimits{MaxTurns: 20, Commands: []string{"git push --force*"}}, Off: true},
		{Builtin: "secrets", Action: "redact", Pii: true},
		{Name: "careful", Stage: "action", Action: "warn", Tools: []string{"run_shell_command"}, Text: "No deleting branches."},
	} {
		got, err := w.SaveGuardrail(ctx, connect.NewRequest(&pb.SaveGuardrailRequest{Workspace: dir, Guardrail: s}))
		require.NoError(t, err)
		require.Empty(t, got.Msg.Problems, "%v", s)
		assert.True(t, got.Msg.Guardrail.Editable)
	}
	bad, err := w.SaveGuardrail(ctx, connect.NewRequest(&pb.SaveGuardrailRequest{Workspace: dir, Guardrail: &pb.GuardrailSpec{Name: "x", Stage: "soon"}}))
	require.NoError(t, err)
	assert.NotEmpty(t, bad.Msg.Problems)

	list, err = w.ListGuardrails(ctx, connect.NewRequest(&pb.ListGuardrailsRequest{Workspace: dir}))
	require.NoError(t, err)
	limits := list.Msg.Builtins[2]
	require.NotNil(t, limits.File)
	assert.Equal(t, int32(20), limits.File.Limits.MaxTurns)
	assert.True(t, limits.File.Off)
	assert.True(t, list.Msg.Builtins[1].File.Pii)
	require.Len(t, list.Msg.Guardrails, 1)
	assert.Equal(t, "careful", list.Msg.Guardrails[0].Name)

	tried, err := w.TryGuardrail(ctx, connect.NewRequest(&pb.TryGuardrailRequest{Workspace: dir, Guardrail: &pb.GuardrailSpec{Builtin: "secrets", Pii: true}, Sample: "mail ada@example.com"}))
	require.NoError(t, err)
	assert.False(t, tried.Msg.Pass)
	assert.Contains(t, tried.Msg.Reason, "[EMAIL REDACTED]")
	tried, err = w.TryGuardrail(ctx, connect.NewRequest(&pb.TryGuardrailRequest{Workspace: dir, Guardrail: &pb.GuardrailSpec{Name: "careful", Stage: "action", Text: "x"}, Sample: "rm"}))
	require.NoError(t, err)
	assert.NotEmpty(t, tried.Msg.Problem, "the mock judge has nothing to say")

	require.NoError(t, unary(w.DeleteGuardrail, &pb.DeleteGuardrailRequest{Workspace: dir, Name: "limits"}))
	list, err = w.ListGuardrails(ctx, connect.NewRequest(&pb.ListGuardrailsRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.Empty(t, list.Msg.Builtins[2].File.Path, "back to its defaults")
	assert.False(t, list.Msg.Builtins[2].File.Off)
	assert.Equal(t, "GUARDRAIL_NOT_FOUND", reason(t, unary(w.DeleteGuardrail, &pb.DeleteGuardrailRequest{Workspace: dir, Name: "limits"})))
}
