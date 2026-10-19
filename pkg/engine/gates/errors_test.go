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

package gates

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// notMapping is a file whose front matter is a list: it can't be edited.
const notMapping = "---\n- a\n- b\n---\nText"

// readOnly is a folder nothing can be written in, or "" when this user
// writes anywhere (root).
func readOnly(t *testing.T) string {
	dir := filepath.Join(t.TempDir(), "ro")
	require.NoError(t, os.Mkdir(dir, 0o555))
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	if f, err := os.Create(filepath.Join(dir, "probe")); err == nil {
		f.Close()
		return ""
	}
	return dir
}

// A rule's save, reorder and judge fail with what went wrong.
func TestRuleErrors(t *testing.T) {
	dir := t.TempDir()
	spec := Spec{Name: "tested", When: AfterAgent, Action: Warn, Text: "Say how."}
	_, _, err := Save(dir, spec, "../bad")
	assert.ErrorIs(t, err, ErrUnknown, "a previous name that isn't one")

	write(t, dir, "tested.md", notMapping)
	_, _, err = Save(dir, spec, "tested")
	assert.ErrorContains(t, err, "the rule's")
	assert.ErrorContains(t, Reorder(dir, []string{"tested"}), "tested")

	file := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(file, nil, 0o644))
	_, _, err = Save(filepath.Join(file, "sub"), spec, "")
	assert.Error(t, err, "a folder that can't be made")

	_, err = Judge(context.Background(), func(context.Context, string, string) (string, error) { return "{not json}", nil }, Rule{Name: "x", When: AfterAgent}, "y")
	assert.ErrorContains(t, err, "isn't JSON")

	if ro := readOnly(t); ro != "" {
		_, _, err = Save(ro, spec, "")
		assert.Error(t, err, "a folder that can't be written")
	}
}

// A guardrail's save and judges fail with what went wrong; the limits
// guardrail writes each limit set.
func TestGuardrailErrors(t *testing.T) {
	down := func(context.Context, string, string) (string, error) { return "", errors.New("down") }
	_, err := JudgeGuardrail(context.Background(), down, Guardrail{Name: "x", Stage: StageOutput}, "y")
	assert.EqualError(t, err, "down")
	_, err = ConfirmInjection(context.Background(), down, "y")
	assert.EqualError(t, err, "down")

	dir := t.TempDir()
	spec := GuardrailSpec{Name: "privacy", Stage: StageOutput, Action: Block, Text: "No names."}
	_, _, err = SaveGuardrail(dir, spec, "../bad")
	assert.Error(t, err, "a previous name that isn't one")
	_, _, err = SaveGuardrail(dir, spec, "missing")
	assert.ErrorIs(t, err, ErrUnknown)

	write(t, dir, "privacy.md", notMapping)
	_, _, err = SaveGuardrail(dir, spec, "privacy")
	assert.ErrorContains(t, err, "the guardrail's")

	file := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(file, nil, 0o644))
	_, _, err = SaveGuardrail(filepath.Join(file, "sub"), spec, "")
	assert.Error(t, err, "a folder that can't be made")
	if ro := readOnly(t); ro != "" {
		_, _, err = SaveGuardrail(ro, spec, "")
		assert.Error(t, err, "a folder that can't be written")
	}

	p, probs, err := SaveGuardrail(dir, GuardrailSpec{Builtin: BuiltinLimits, Action: Block, Limits: Limits{MaxCostUSD: 2.5, MaxFilesChanged: 12, AllowedDomains: []string{"go.dev"}}}, "")
	require.NoError(t, err)
	require.Empty(t, probs)
	data, _ := os.ReadFile(p)
	g := ParseGuardrail("limits", p, data)
	assert.Equal(t, Limits{MaxCostUSD: 2.5, MaxFilesChanged: 12, AllowedDomains: []string{"go.dev"}}, g.Limits)
	assert.Nil(t, limitsFields(GuardrailSpec{Builtin: BuiltinSecrets}), "only the limits guardrail has limits")
}
