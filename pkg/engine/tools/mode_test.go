package tools

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/config"
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
			if got := allowed(h, req); got != want[i] {
				t.Errorf("%s: %s allowed=%v, want %v", mode, req.Tool, got, want[i])
			}
		}
	}

	// dont-ask never reaches the approver, but saved rules still apply.
	asked := false
	h := NewHooks(Policy{Mode: api.ModeDontAsk})
	h.SetApprover(func(context.Context, api.ApprovalRequest) (api.Decision, error) {
		asked = true
		return api.DecisionOnce, nil
	})
	if err := h.Approve(context.Background(), cmd); err == nil || asked {
		t.Errorf("dont-ask asked (%v) or allowed (%v)", asked, err)
	}
	store, _ := OpenApprovalStore(filepath.Join(t.TempDir(), "a.json"))
	store.Add(cmd.Key, "")
	h.SetStore(store)
	if err := h.Approve(context.Background(), cmd); err != nil {
		t.Errorf("dont-ask ignored a saved rule: %v", err)
	}

	// Unattended runs get only their permissions, whatever the mode.
	bypass := NewHooks(Policy{Mode: api.ModeBypass})
	ctx := Unattended(context.Background(), func(context.Context, api.ApprovalRequest) (api.Decision, error) { return api.DecisionDeny, nil })
	if err := bypass.Approve(ctx, cmd); err == nil {
		t.Error("bypass mode widened an unattended run")
	}
}

func TestParsePermissionMode(t *testing.T) {
	for in, want := range map[string]api.PermissionMode{
		"": api.ModeDefault, "default": api.ModeDefault, "acceptEdits": api.ModeAcceptEdits, "accept_edits": api.ModeAcceptEdits,
		"PLAN": api.ModePlan, "dontAsk": api.ModeDontAsk, "bypassPermissions": api.ModeBypass, "bypass": api.ModeBypass,
	} {
		if got, err := api.ParsePermissionMode(in); err != nil || got != want {
			t.Errorf("%q = %q %v", in, got, err)
		}
	}
	if _, err := api.ParsePermissionMode("yolo"); !errors.Is(err, api.ErrUnknownMode) {
		t.Errorf("unknown mode: %v", err)
	}
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
		if err != nil {
			t.Fatal(err)
		}
		if r.Hooks().Mode() != api.ModeDefault || !errors.Is(r.ModeNote(), api.ErrBypassNeedsSandbox) {
			t.Errorf("without a sandbox: mode %s, note %v", r.Hooks().Mode(), r.ModeNote())
		}
		if err := r.SetPermissionMode(api.ModeBypass); !errors.Is(err, api.ErrBypassNeedsSandbox) {
			t.Errorf("SetPermissionMode(bypass) without a sandbox: %v", err)
		}
		if err := r.SetPermissionMode(api.ModeAcceptEdits); err != nil || r.Hooks().Mode() != api.ModeAcceptEdits {
			t.Errorf("accept-edits: %v %s", err, r.Hooks().Mode())
		}
		r.Close()
	}
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Blitz.PermissionMode = "yolo"
	if _, err := NewRegistry(cfg, nil, nil); err == nil {
		t.Error("an unknown permission_mode was accepted")
	}
}

// With the OS sandbox active, bypass is allowed.
func TestBypassWithTheSandbox(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Tools.ApprovalsFile = ""
	cfg.Images.Enabled = false
	cfg.Blitz.AutoApprove = true
	r, err := NewRegistry(cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if !r.ShellSandbox().Active() {
		t.Skip("no OS sandbox here: " + r.ShellSandbox().Status())
	}
	if r.Hooks().Mode() != api.ModeBypass || r.ModeNote() != nil {
		t.Errorf("auto_approve with a sandbox: %s %v", r.Hooks().Mode(), r.ModeNote())
	}
}
