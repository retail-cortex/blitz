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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blitz do's words are the command, then name=value arguments, a value
// read as JSON when it is; help takes what to help with; --args is one
// JSON object (spec_060_blitz_tools DRV-02, DRV-74).
func TestDoArgs(t *testing.T) {
	cases := []struct {
		name     string
		words    []string
		argsJSON string
		cmd      string
		args     map[string]any
		err      string
	}{
		{name: "a command", words: []string{"connector", "list"}, cmd: "connector list", args: map[string]any{}},
		{name: "arguments", words: []string{"connector", "save", "kind=sqlite", "read_only=true", `settings={"database":"r.db"}`, "description=a = b"},
			cmd: "connector save", args: map[string]any{"kind": "sqlite", "read_only": true, "settings": map[string]any{"database": "r.db"}, "description": "a = b"}},
		{name: "a number", words: []string{"process", "stop", "id=3"}, cmd: "process stop", args: map[string]any{"id": float64(3)}},
		{name: "help with a command", words: []string{"help", "connector", "save"}, cmd: "help", args: map[string]any{"command": "connector save"}},
		{name: "--args", words: []string{"settings", "set"}, argsJSON: `{"key":"mode","value":"plan"}`, cmd: "settings set", args: map[string]any{"key": "mode", "value": "plan"}},
		{name: "bad --args", words: []string{"doctor"}, argsJSON: "{", err: "--args"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd, args, err := doArgs(c.words, c.argsJSON)
			if c.err != "" {
				assert.ErrorContains(t, err, c.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.cmd, cmd)
			assert.Equal(t, c.args, args)
		})
	}
}

// A change is confirmed with y; a secret is read as a line from a pipe.
func TestDoConfirmAndSecret(t *testing.T) {
	var out bytes.Buffer
	assert.True(t, confirmed(strings.NewReader("y\n"), &out))
	assert.False(t, confirmed(strings.NewReader("\n"), &out))
	v, err := readHidden(strings.NewReader("s3cret\n"), &out, "API_TOKEN")
	require.NoError(t, err)
	assert.Equal(t, "s3cret", v)
	assert.Contains(t, out.String(), "API_TOKEN (kept as a Blitz secret, not shown)")
}
