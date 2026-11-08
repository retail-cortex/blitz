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
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/secrets"

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
		{name: "a word after the arguments", words: []string{"connector", "save", "kind=sqlite", "oops"}, err: "arguments are name=value"},
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

// doCLI runs `blitz do …` with input as what the person types.
func doCLI(t *testing.T, input string, args ...string) (string, error) {
	t.Helper()
	cmd := newRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(input))
	cmd.SetArgs(append([]string{"do"}, args...))
	err := cmd.ExecuteContext(context.Background())
	stopPrivate()
	return out.String(), err
}

// blitz do runs a command in the workspace: help, a list as JSON or
// indented, and a change shown and confirmed with y, or left undone
// (DRV-02, DRV-74).
func TestDoCommand(t *testing.T) {
	isolate(t)
	ws := t.TempDir()
	cases := []struct {
		name  string
		input string
		args  []string
		want  string
		err   string
	}{
		{name: "help", args: []string{"help", "rule", "save"}, want: "rule save"},
		{name: "a list as JSON", args: []string{"command", "list", "--json"}, want: `{"Name":"blitz"`},
		{name: "a list", args: []string{"settings", "show"}, want: `"mode": `},
		{name: "a change declined", input: "n\n", args: []string{"command", "save", "name=hello", "prompt=Say hello."}, want: "Save the command /hello", err: "nothing was changed"},
		{name: "a change confirmed", input: "y\n", args: []string{"command", "save", "name=hello", "prompt=Say hello."}, want: `"use": "/hello"`},
		{name: "--yes", args: []string{"command", "save", "name=bye", "prompt=Say bye.", "--yes"}, want: `"saved"`},
		{name: "bad arguments", args: []string{"doctor", "--args", "{"}, err: "--args"},
		{name: "no such command", args: []string{"frobnicate"}, err: "no command"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := doCLI(t, c.input, append(c.args, "--dir", ws)...)
			if c.err != "" {
				assert.ErrorContains(t, err, c.err)
			} else {
				require.NoError(t, err, out)
			}
			assert.Contains(t, out, c.want)
		})
	}
	text, err := os.ReadFile(filepath.Join(ws, ".blitz", "commands", "hello.md"))
	require.NoError(t, err)
	assert.Contains(t, string(text), "Say hello.")
}

// A secret that can't be read (the input ended) is an error.
func TestDoSecretUnread(t *testing.T) {
	var out bytes.Buffer
	_, err := readHidden(strings.NewReader(""), &out, "API_TOKEN")
	assert.ErrorContains(t, err, "API_TOKEN")
}

// A secret a command needs is typed after the change is confirmed, and
// kept as a Blitz secret (DRV-05).
func TestDoSecret(t *testing.T) {
	isolate(t)
	secrets.SetDefault(&secrets.Memory{}) // never the machine's keychain
	ws := t.TempDir()
	out, err := doCLI(t, "y\ns3cret\n", "secret", "set", "name=API_TOKEN", "--dir", ws)
	require.NoError(t, err, out)
	assert.Contains(t, out, "${API_TOKEN}")
	assert.NotContains(t, out, "s3cret")
	out, err = doCLI(t, "y\n", "secret", "set", "name=OTHER", "--dir", ws)
	assert.Error(t, err, "no value typed: %s", out)
}
