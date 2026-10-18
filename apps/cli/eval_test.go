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
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blitz eval with no model: each case can't run, so the eval fails
// (exit 1); an unknown eval is a usage error.
func TestEvalCommand(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".agents", "evals"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".agents", "evals", "sums.md"), []byte("| ID | Name | Prompt | Output | Type |\n|-|-|-|-|-|\n| a | Sum | 2+2? | 4 | exact |\n| b | | 2+3? | 5 | exact |\n"), 0o644))

	out, err := runCLI(t, "-d", dir, "eval", "sums", "--case", "a")
	assert.Equal(t, exitFailure, exitCodeFor(err), "%v\n%s", err, out)
	assert.Contains(t, out, "! a Sum · ")
	assert.ErrorContains(t, err, "sums failed: 1 couldn't run")

	out, err = runCLI(t, "-d", dir, "eval", "--json", "sums")
	assert.Equal(t, exitFailure, exitCodeFor(err), "%v", err)
	var run api.EvalRun
	require.NoError(t, json.Unmarshal([]byte(out), &run), out)
	assert.Equal(t, api.EvalFailed, run.Status)
	assert.Len(t, run.Results, 2)

	_, err = runCLI(t, "-d", dir, "eval", "gone")
	assert.Equal(t, exitUsage, exitCodeFor(err), "%v", err)
}

// An eval with no cases can't run (a usage error); a broken settings file
// stops blitz eval before it opens the workspace.
func TestEvalCommandEdges(t *testing.T) {
	home := isolate(t)
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".agents", "evals"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".agents", "evals", "empty.md"), []byte("| ID | Name | Prompt | Output | Type |\n|-|-|-|-|-|\n"), 0o644))
	_, err := runCLI(t, "-d", dir, "eval", "empty")
	assert.Equal(t, exitUsage, exitCodeFor(err), "%v", err)
	assert.ErrorContains(t, err, "no case to run")

	require.NoError(t, os.MkdirAll(filepath.Join(home, ".blitz"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".blitz", ".env.toml"), []byte("[[[ not toml"), 0o600))
	_, err = runCLI(t, "-d", dir, "eval", "empty")
	assert.Error(t, err, "a broken settings file")
}

func TestPrintEvalResult(t *testing.T) {
	tests := []struct {
		name string
		r    api.EvalResult
		want string
	}{
		{"near pass", api.EvalResult{Case: "a", Name: "Greets", Agent: "blitz", Type: "near", Status: api.EvalPass, Score: 0.91, Threshold: 0.8}, "✓ a Greets · blitz · 0.91/0.80\n"},
		{"exact miss", api.EvalResult{Case: "b", Agent: "qa", Type: "exact", Status: api.EvalWarn, Output: "five\n  apples"}, "⚠ b · qa\n    got: five apples\n"},
		{"near fail", api.EvalResult{Case: "c", Agent: "qa", Type: "near", Status: api.EvalFail, Score: 0.2, Threshold: 0.8, Reason: "Off topic.", Refusals: []string{"create_file: notes.md"}},
			"✗ c · qa · 0.20/0.80\n    Off topic.\n    refused: create_file: notes.md\n"},
		{"error", api.EvalResult{Case: "d", Agent: "qa", Type: "near", Status: api.EvalError, Reason: "no model"}, "! d · qa\n    no model\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b bytes.Buffer
			printEvalResult(&b, tt.r)
			assert.Equal(t, tt.want, b.String())
		})
	}
	assert.Equal(t, "1 passed, 2 warnings", evalSummary(api.EvalRun{Results: []api.EvalResult{{Status: api.EvalWarn}, {Status: api.EvalPass}, {Status: api.EvalWarn}}}))
	assert.Len(t, []rune(oneLine(string(bytes.Repeat([]byte("x"), 300)))), 160)
}
