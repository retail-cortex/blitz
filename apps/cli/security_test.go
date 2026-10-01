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

package main

import (
	"bytes"
	"testing"

	"github.com/retail-cortex/blitz/pkg/sandboxsetup"
	"github.com/stretchr/testify/assert"
)

// Each sandbox state says what it is and what to do; only AppArmor's
// restriction points at fix-apparmor.
func TestPrintSandboxStatus(t *testing.T) {
	tests := []struct {
		state      sandboxsetup.State
		want       string
		suggestFix bool
	}{
		{sandboxsetup.Ready, "bubblewrap (/usr/bin/bwrap) runs the agent's commands", false},
		{sandboxsetup.NoBwrap, "sudo apt install bubblewrap", false},
		{sandboxsetup.Restricted, "AppArmor keeps bubblewrap (/usr/bin/bwrap)", true},
		{sandboxsetup.Broken, "sandbox.shell = \"auto\"", false},
		{sandboxsetup.Unsupported, "needs no set-up here", false},
	}
	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			var out bytes.Buffer
			printSandboxStatus(&out, sandboxsetup.Status{State: tt.state, Bwrap: "/usr/bin/bwrap", Detail: "why"})
			assert.Contains(t, out.String(), tt.want)
			assert.Equal(t, tt.suggestFix, bytes.Contains(out.Bytes(), []byte("blitz security fix-apparmor")))
		})
	}
}

// blitz security has the fix-apparmor subcommand and opens no workspace.
func TestSecurityCommand(t *testing.T) {
	cmd := newSecurityCommand()
	fix, _, err := cmd.Find([]string{"fix-apparmor"})
	assert.NoError(t, err)
	assert.Equal(t, "fix-apparmor", fix.Name())
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs(nil)
	assert.NoError(t, cmd.Execute())
	assert.NotEmpty(t, out.String())
}
