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
	"errors"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Commands get a copy of the ADC sign-in only when sandbox.share_adc says
// so, and only when a provider signs in with it.
func TestShareADC(t *testing.T) {
	tests := []struct {
		name       string
		adc, share bool
		wantCopies int
	}{
		{name: "ADC, not shared", adc: true},
		{name: "ADC, shared", adc: true, share: true, wantCopies: 1},
		{name: "shared, but no ADC", share: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testRegistryConfig(t)
			if tt.adc {
				cfg.LLM.Gemini.Auth = config.AuthADC
			}
			cfg.Sandbox.ShareADC = tt.share
			r, err := NewRegistry(cfg, nil, nil)
			require.NoError(t, err)
			t.Cleanup(func() { r.Close() })
			assert.Len(t, r.exec.Credentials, tt.wantCopies)
		})
	}
}

// The ant command's sign-in is hidden from the model's tools and commands
// unless Claude signs in with it.
func TestAntSignInHiddenUnlessUsed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTHROPIC_CONFIG_DIR", dir)
	creds := filepath.Join(dir, "credentials", "default.json")
	tests := []struct {
		name    string
		auth    string
		blocked bool
	}{
		{name: "an API key", auth: "", blocked: true},
		{name: "ADC", auth: config.AuthADC, blocked: true},
		{name: "the ant sign-in", auth: config.AuthOAuth, blocked: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testRegistryConfig(t)
			cfg.LLM.Anthropic.Auth = tt.auth
			r, err := NewRegistry(cfg, nil, nil)
			require.NoError(t, err)
			t.Cleanup(func() { r.Close() })
			_, blocked := r.Workspace().Blocked().Match(creds)
			assert.Equal(t, tt.blocked, blocked)
		})
	}
}

// A required sandbox that can't run is ErrSandboxUnavailable, saying how
// to get it or do without.
func TestRequiredSandboxUnavailable(t *testing.T) {
	orig := platformSandbox
	t.Cleanup(func() { platformSandbox = orig })
	platformSandbox = func(OSSandboxSpec) (sandboxWrapper, error) { return nil, errors.New("no bwrap") }
	_, err := NewOSSandbox(OSSandboxSpec{Mode: SandboxRequired})
	assert.ErrorIs(t, err, api.ErrSandboxUnavailable)
	assert.ErrorContains(t, err, "no bwrap")
	assert.ErrorContains(t, err, sandboxHint)
	box, err := NewOSSandbox(OSSandboxSpec{Mode: SandboxAuto})
	require.NoError(t, err, "auto runs without it")
	assert.False(t, box.Active())
}
