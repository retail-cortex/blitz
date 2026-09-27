package tools

import (
	"github.com/retail-cortex/blitz/pkg/api"
)

// Mode returns the permission mode.
func (h *Hooks) Mode() api.PermissionMode {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.policy.Mode == "" {
		return api.ModeDefault
	}
	return h.policy.Mode
}

// setMode changes the mode; Registry.SetPermissionMode checks bypass's
// requirement first.
func (h *Hooks) setMode(m api.PermissionMode) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.policy.Mode = m
}

// SetPermissionMode changes the permission mode, refusing bypass unless
// the OS sandbox is active.
func (r *Registry) SetPermissionMode(m api.PermissionMode) error {
	if _, err := api.ParsePermissionMode(string(m)); err != nil {
		return err
	}
	if m == api.ModeBypass && !r.ShellSandbox().Active() {
		return api.ErrBypassNeedsSandbox
	}
	r.hooks.setMode(m)
	return nil
}
