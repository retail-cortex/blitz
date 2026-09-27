package tools

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/internal/config"
)

func TestPermissionModesAtTheGate(t *testing.T) {
	write := ApprovalRequest{Tool: "create_file", Kind: ActionWrite, Detail: "Create a", Key: "write:/ws", Targets: []string{"a"}}
	del := ApprovalRequest{Tool: "delete_file", Kind: ActionDelete, Detail: "Delete a", Key: "delete:/ws", Targets: []string{"a"}}
	cmd := ApprovalRequest{Tool: "run_shell_command", Kind: ActionCommand, Detail: "ls", Key: "cmd:/ws\x00ls", Targets: []string{"ls"}}
	web := ApprovalRequest{Tool: "web_fetch", Kind: ActionNetwork, Detail: "GET x", Key: "web:x", Targets: []string{"x"}}
	allowed := func(h *Hooks, req ApprovalRequest) bool { return h.Approve(context.Background(), req) == nil }

	// No approver: whatever would ask is refused, so these show what each
	// mode lets through by itself.
	cases := map[PermissionMode][4]bool{ // write, delete, command, web
		ModeDefault:     {false, false, false, false},
		ModePlan:        {false, false, false, false},
		ModeAcceptEdits: {true, true, false, false},
		ModeDontAsk:     {false, false, false, false},
		ModeBypass:      {true, true, true, true},
	}
	for mode, want := range cases {
		h := NewHooks(Policy{Mode: mode})
		for i, req := range []ApprovalRequest{write, del, cmd, web} {
			if got := allowed(h, req); got != want[i] {
				t.Errorf("%s: %s allowed=%v, want %v", mode, req.Tool, got, want[i])
			}
		}
	}

	// dont-ask never reaches the approver, but saved rules still apply.
	asked := false
	h := NewHooks(Policy{Mode: ModeDontAsk})
	h.SetApprover(func(context.Context, ApprovalRequest) (Decision, error) { asked = true; return DecisionOnce, nil })
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
	bypass := NewHooks(Policy{Mode: ModeBypass})
	ctx := Unattended(context.Background(), func(context.Context, ApprovalRequest) (Decision, error) { return DecisionDeny, nil })
	if err := bypass.Approve(ctx, cmd); err == nil {
		t.Error("bypass mode widened an unattended run")
	}
}

func TestParsePermissionMode(t *testing.T) {
	for in, want := range map[string]PermissionMode{
		"": ModeDefault, "default": ModeDefault, "acceptEdits": ModeAcceptEdits, "accept_edits": ModeAcceptEdits,
		"PLAN": ModePlan, "dontAsk": ModeDontAsk, "bypassPermissions": ModeBypass, "bypass": ModeBypass,
	} {
		if got, err := ParsePermissionMode(in); err != nil || got != want {
			t.Errorf("%q = %q %v", in, got, err)
		}
	}
	if _, err := ParsePermissionMode("yolo"); !errors.Is(err, ErrUnknownMode) {
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
		if r.Hooks().Mode() != ModeDefault || !errors.Is(r.ModeNote(), ErrBypassNeedsSandbox) {
			t.Errorf("without a sandbox: mode %s, note %v", r.Hooks().Mode(), r.ModeNote())
		}
		if err := r.SetPermissionMode(ModeBypass); !errors.Is(err, ErrBypassNeedsSandbox) {
			t.Errorf("SetPermissionMode(bypass) without a sandbox: %v", err)
		}
		if err := r.SetPermissionMode(ModeAcceptEdits); err != nil || r.Hooks().Mode() != ModeAcceptEdits {
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
	if r.Hooks().Mode() != ModeBypass || r.ModeNote() != nil {
		t.Errorf("auto_approve with a sandbox: %s %v", r.Hooks().Mode(), r.ModeNote())
	}
}
