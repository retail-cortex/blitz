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
	"os/exec"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The sandbox's status: off without one, and what an active one limits.
func TestOSSandboxStatus(t *testing.T) {
	orig := platformSandbox
	t.Cleanup(func() { platformSandbox = orig })
	platformSandbox = func(OSSandboxSpec) (sandboxWrapper, error) { return prefixWrapper("sbx"), nil }

	var none *OSSandbox
	assert.Equal(t, "off", none.Status())

	tests := []struct {
		name string
		spec OSSandboxSpec
		want string
	}{
		{"network blocked, by default mode", OSSandboxSpec{WritableDirs: []string{"/a", "/b"}}, "on (writes limited to 2 dirs, network blocked)"},
		{"network allowed", OSSandboxSpec{Mode: SandboxRequired, AllowNetwork: true}, "on (writes limited to 0 dirs, network allowed)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := NewOSSandbox(tt.spec)
			require.NoError(t, err)
			assert.True(t, s.Active())
			assert.Equal(t, tt.want, s.Status())
		})
	}
}

// Without bwrap on the PATH, Linux has no sandbox, and says what to install.
func TestNativeSandboxWithoutBwrap(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("bubblewrap is Linux's sandbox")
	}
	t.Setenv("PATH", t.TempDir())
	_, err := nativeSandbox(OSSandboxSpec{})
	assert.ErrorContains(t, err, "bubblewrap (bwrap) not found")
}

// Killing the group of a command that never started does nothing.
func TestKillProcessGroupNotStarted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process groups are Unix's")
	}
	assert.NoError(t, killProcessGroup(exec.Command("true")))
}
