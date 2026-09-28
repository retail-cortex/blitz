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

package tools

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPermissionModesAtTheGate(t *testing.T) {
	write := api.ApprovalRequest{Tool: "create_file", Kind: api.ActionWrite, Detail: "Create a", Key: "write:/ws", Targets: []string{"a"}}
	del := api.ApprovalRequest{Tool: "delete_file", Kind: api.ActionDelete, Detail: "Delete a", Key: "delete:/ws", Targets: []string{"a"}}
	cmd := api.ApprovalRequest{Tool: "run_shell_command", Kind: api.ActionCommand, Detail: "ls", Key: "cmd:/ws\x00ls", Targets: []string{"ls"}}
	web := api.ApprovalRequest{Tool: "web_fetch", Kind: api.ActionNetwork, Detail: "GET x", Key: "web:x", Targets: []string{"x"}}
	allowed := func(h *Hooks, req api.ApprovalRequest) bool { return h.Approve(context.Background(), req) == nil }

	// No approver: whatever would ask is refused, so these show what each
	// mode lets through by itself.
	cases := map[api.PermissionMode][4]bool{ // write, delete, command, web
		api.ModeDefault:     {false, false, false, false},
		api.ModePlan:        {false, false, false, false},
		api.ModeAcceptEdits: {true, true, false, false},
		api.ModeDontAsk:     {false, false, false, false},
		api.ModeBypass:      {true, true, true, true},
	}
	for mode, want := range cases {
		h := NewHooks(Policy{Mode: mode})
		for i, req := range []api.ApprovalRequest{write, del, cmd, web} {
			got := allowed(h, req)
			assert.Equal(t, want[i], got, "%s: %s allowed=%v, want %v", mode, req.Tool, got, want[i])
		}
	}

	// dont-ask never reaches the approver, but saved rules still apply.
	asked := false
	h := NewHooks(Policy{Mode: api.ModeDontAsk})
	h.SetApprover(func(context.Context, api.ApprovalRequest) (api.Decision, error) {
		asked = true
		return api.DecisionOnce, nil
	})
	err := h.Approve(context.Background(), cmd)
	assert.Error(t, err, "dont-ask asked (%v) or allowed (%v)", asked, err)
	assert.False(t, asked, "dont-ask asked (%v) or allowed (%v)", asked, err)
	store, _ := OpenApprovalStore(filepath.Join(t.TempDir(), "a.json"))
	store.Add(cmd.Key, "")
	h.SetStore(store)
	assert.NoError(t, h.Approve(context.Background(), cmd), "dont-ask ignored a saved rule")

	// Unattended runs get only their permissions, whatever the mode.
	bypass := NewHooks(Policy{Mode: api.ModeBypass})
	ctx := Unattended(context.Background(), func(context.Context, api.ApprovalRequest) (api.Decision, error) { return api.DecisionDeny, nil })
	assert.Error(t, bypass.Approve(ctx, cmd), "bypass mode widened an unattended run")
}

func TestParsePermissionMode(t *testing.T) {
	for in, want := range map[string]api.PermissionMode{
		"": api.ModeDefault, "default": api.ModeDefault, "acceptEdits": api.ModeAcceptEdits, "accept_edits": api.ModeAcceptEdits,
		"PLAN": api.ModePlan, "dontAsk": api.ModeDontAsk, "bypassPermissions": api.ModeBypass, "bypass": api.ModeBypass,
	} {
		got, err := api.ParsePermissionMode(in)
		assert.NoError(t, err, "%q = %q", in, got)
		assert.Equal(t, want, got, "%q = %q %v", in, got, err)
	}
	_, err := api.ParsePermissionMode("yolo")
	assert.ErrorIs(t, err, api.ErrUnknownMode, "unknown mode: %v", err)
}

// Bypass (and auto_approve, its older spelling) needs the OS sandbox.
func TestBypassNeedsTheSandbox(t *testing.T) {
	for _, mutate := range []func(*config.Config){
		func(c *config.Config) { c.Blitz.AutoApprove = true },
		func(c *config.Config) { c.Blitz.PermissionMode = "bypass" },
	} {
		cfg := config.DefaultConfig()
		cfg.Tools.WorkspaceDir = t.TempDir()
		cfg.Tools.ApprovalsFile = ""
		cfg.Images.Enabled = false
		cfg.Sandbox.Shell = "off"
		mutate(cfg)
		r, err := NewRegistry(cfg, nil, nil)
		require.NoError(t, err)
		assert.Equal(t, api.ModeDefault, r.Hooks().Mode(), "without a sandbox: mode %s, note %v", r.Hooks().Mode(), r.ModeNote())
		assert.ErrorIs(t, r.ModeNote(), api.ErrBypassNeedsSandbox, "without a sandbox: mode %s, note %v", r.Hooks().Mode(), r.ModeNote())
		err = r.SetPermissionMode(api.ModeBypass)
		assert.ErrorIs(t, err, api.ErrBypassNeedsSandbox, "SetPermissionMode(bypass) without a sandbox: %v", err)
		err = r.SetPermissionMode(api.ModeAcceptEdits)
		assert.NoError(t, err, "accept-edits: %v %s", err, r.Hooks().Mode())
		assert.Equal(t, api.ModeAcceptEdits, r.Hooks().Mode(), "accept-edits: %v %s", err, r.Hooks().Mode())
		r.Close()
	}
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Blitz.PermissionMode = "yolo"
	_, err := NewRegistry(cfg, nil, nil)
	assert.Error(t, err, "an unknown permission_mode was accepted")
}

// With the OS sandbox active, bypass is allowed.
func TestBypassWithTheSandbox(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Tools.ApprovalsFile = ""
	cfg.Images.Enabled = false
	cfg.Blitz.AutoApprove = true
	r, err := NewRegistry(cfg, nil, nil)
	require.NoError(t, err)
	defer r.Close()
	if !r.ShellSandbox().Active() {
		t.Skip("no OS sandbox here: " + r.ShellSandbox().Status())
	}
	assert.Equal(t, api.ModeBypass, r.Hooks().Mode(), "auto_approve with a sandbox: %s %v", r.Hooks().Mode(), r.ModeNote())
	assert.NoError(t, r.ModeNote(), "auto_approve with a sandbox: %s %v", r.Hooks().Mode(), r.ModeNote())
}
