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

package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/session"
)

// The personality is the root agent's, after every other instruction, in
// the runs someone attends (spec_064_personality PER-02): a sub-agent the
// root calls and an unattended run (a worker, an eval) never get it;
// SetPersonality changes it from the next turn, "" takes it away.
func TestPersonalityRootOnly(t *testing.T) {
	const mentor = "\n\nPERSONALITY: be a patient mentor"
	f := newEngineWith(t, fixtureOpts{opts: []Option{WithInstructions("\nPROJECT RULE"), WithPersonality(mentor)}},
		textContent("root"), textContent("qa"), textContent("worker"), textContent("direct"), textContent("plain"))
	ctx := context.Background()

	_, err := collect(t, f.eng, "s", "hi")
	require.NoError(t, err)
	sys := systemText(f.llm)
	require.Contains(t, sys, mentor, "the root agent has it")
	assert.Greater(t, strings.Index(sys, mentor), strings.Index(sys, "PROJECT RULE"), "after the other instructions")

	_, err = f.eng.InvokeSubagent(ctx, "qa", "run the tests")
	require.NoError(t, err)
	assert.NotContains(t, systemText(f.llm), "PERSONALITY", "a sub-agent hasn't")

	deny := func(context.Context, api.ApprovalRequest) (api.Decision, error) { return 0, nil }
	err = f.eng.Execute(tools.Unattended(ctx, deny), "w", "nightly", func(*session.Event) error { return nil })
	require.NoError(t, err)
	assert.NotContains(t, systemText(f.llm), "PERSONALITY", "nor an unattended run")

	require.NoError(t, f.eng.SetPersonality(ctx, "\n\nPERSONALITY: be direct"))
	_, err = collect(t, f.eng, "s", "again")
	require.NoError(t, err)
	assert.Contains(t, systemText(f.llm), "be direct")
	assert.NotContains(t, systemText(f.llm), "mentor")

	require.NoError(t, f.eng.SetPersonality(ctx, ""))
	_, err = collect(t, f.eng, "s", "plain")
	require.NoError(t, err)
	assert.NotContains(t, systemText(f.llm), "PERSONALITY")

	var instructions int64
	require.NoError(t, f.eng.SetPersonality(ctx, mentor))
	for _, p := range f.eng.ContextParts(ctx, "s", 0) {
		if p.Name == "instructions" {
			instructions = p.Tokens
		}
	}
	assert.Equal(t, int64(len("\nPROJECT RULE"+mentor)/4), instructions, "the context breakdown counts it")
}
