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
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tool's description names every group and gives an example; help
// lists the commands, a group's arguments, a command's (DRV-03, DRV-60).
func TestBlitzHelp(t *testing.T) {
	d := blitzDescription()
	for _, g := range api.BlitzGroups() {
		assert.Contains(t, d, g[0].Group(), "the description names every group")
	}
	assert.Contains(t, d, `"command": "connector save"`)
	all := BlitzHelp("")
	assert.Contains(t, all, "connector save: ")
	assert.Contains(t, all, "help <command>")
	assert.Contains(t, BlitzHelp("connector"), "kind (string, required)")
	assert.True(t, strings.HasPrefix(BlitzHelp(" Settings  SET "), "settings set: "))
	assert.Equal(t, all, BlitzHelp("nothing-like-it"))
}

// Arguments are checked against the catalog: required, typed, among their
// values, none unknown; numbers come back whole.
func TestCheckBlitzArgs(t *testing.T) {
	c, _ := api.FindBlitzCommand("settings set")
	_, err := CheckBlitzArgs(c, map[string]any{"key": "colour", "extra": 1})
	assert.ErrorContains(t, err, "value is required")
	assert.ErrorContains(t, err, "key must be one of")
	assert.ErrorContains(t, err, `takes no argument "extra"`)
	got, err := CheckBlitzArgs(c, map[string]any{"key": "mode", "value": "plan"})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"key": "mode", "value": "plan"}, got)

	stop, _ := api.FindBlitzCommand("process stop")
	got, err = CheckBlitzArgs(stop, map[string]any{"id": float64(3)})
	require.NoError(t, err)
	assert.Equal(t, 3, got["id"])
	_, err = CheckBlitzArgs(stop, map[string]any{"id": 2.5})
	assert.ErrorContains(t, err, "whole number")
	save, _ := api.FindBlitzCommand("connector save")
	_, err = CheckBlitzArgs(save, map[string]any{"kind": "sqlite", "name": "x", "settings": "nope", "read_only": "yes", "queries": "q"})
	assert.ErrorContains(t, err, "settings must be a JSON object")
	assert.ErrorContains(t, err, "read_only must be true or false")
	assert.ErrorContains(t, err, "queries must be a list")
}

// The tool runs a command through the workspace's runner; an unknown one,
// or wrong arguments, come back with help; a job's run needs a blitz:
// permission for it, reading ones too (DRV-70).
func TestRunBlitz(t *testing.T) {
	hooks, _ := decisionHooks(api.DecisionOnce)
	r := &Registry{hooks: hooks}
	ctx := context.Background()
	out := r.runBlitz(ctx, BlitzInput{Command: "doctor"})
	assert.Contains(t, out.Error, "aren't available here")

	var ran []string
	r.SetBlitzRunner(func(_ context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
		ran = append(ran, c.Name)
		return map[string]any{"ok": true}, nil
	})
	out = r.runBlitz(ctx, BlitzInput{Command: " Doctor "})
	assert.Empty(t, out.Error)
	assert.Equal(t, map[string]any{"ok": true}, out.Result)
	out = r.runBlitz(ctx, BlitzInput{Command: "connector frobnicate"})
	assert.Contains(t, out.Error, `no command "connector frobnicate"`)
	assert.Contains(t, out.Help, "connector save")
	out = r.runBlitz(ctx, BlitzInput{Command: "connector enable"})
	assert.Contains(t, out.Error, "name is required")
	assert.True(t, strings.HasPrefix(out.Help, "connector enable:"))
	out = r.runBlitz(ctx, BlitzInput{Command: "help", Args: map[string]any{"command": "doctor"}})
	assert.True(t, strings.HasPrefix(out.Help, "doctor:"))

	// A job's run: only what its permissions name.
	permits := func(allowed string) context.Context {
		return Unattended(ctx, func(_ context.Context, req api.ApprovalRequest) (api.Decision, error) {
			if req.Kind == api.ActionBlitz && req.Targets[0] == allowed {
				return api.DecisionOnce, nil
			}
			return api.DecisionDeny, nil
		})
	}
	out = r.runBlitz(permits("connector list"), BlitzInput{Command: "doctor"})
	assert.NotEmpty(t, out.Error, "a job without blitz:doctor")
	out = r.runBlitz(permits("doctor"), BlitzInput{Command: "doctor"})
	assert.Empty(t, out.Error)
	assert.Equal(t, []string{"doctor", "doctor"}, ran)
}
