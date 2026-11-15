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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func guard(t *testing.T, front, body string) Guardrail {
	t.Helper()
	return ParseGuardrail("g", "g.md", []byte("---\n"+front+"---\n"+body))
}

// GT-40, GT-41: the stages' before and after names, and the checks with
// their settings, read from the file and checked.
func TestGuardrailChecksParse(t *testing.T) {
	g := guard(t, "stage: before_model\naction: block\ncheck: regex\npatterns: ['ACME-\\d+']\n", "")
	require.Empty(t, g.Problem)
	assert.Equal(t, StageInput, g.Stage)
	assert.Equal(t, []string{CheckRegex}, g.Check)
	assert.False(t, g.NeedsJudge(), "no model needed")
	assert.True(t, g.Streamable())
	for alias, stage := range map[string]string{"after_model": StageOutput, "before_tool": StageAction, "after_tool": StageToolResult} {
		assert.Equal(t, stage, guard(t, "stage: "+alias+"\n", "Catch it.").Stage, alias)
	}
	g = guard(t, "stage: output\ncheck: [keywords, judge]\nwords: [Confidential]\ncase_sensitive: true\nmin_matches: 2\nthreshold: 0.8\n", "No leaks.")
	require.Empty(t, g.Problem)
	assert.Equal(t, []string{CheckKeywords, CheckJudge}, g.Check)
	assert.True(t, g.NeedsJudge())
	assert.False(t, g.Streamable())
	assert.Equal(t, 0.8, g.Threshold)
	g = guard(t, "stage: output\ncheck: json_schema\nschema:\n  type: object\n  required: [verdict]\n", "")
	require.Empty(t, g.Problem)
	assert.JSONEq(t, `{"type":"object","required":["verdict"]}`, g.Schema)

	for front, says := range map[string]string{
		"stage: output\ncheck: vibes\n":                                 `check "vibes"`,
		"stage: output\ncheck: regex\n":                                 "name the patterns",
		"stage: output\ncheck: regex\npatterns: ['(']\n":                "pattern",
		"stage: output\ncheck: keywords\n":                              "name the words",
		"stage: output\ncheck: max_length\n":                            "max_length",
		"stage: output\ncheck: json_schema\n":                           "give the schema",
		"stage: output\ncheck: json_schema\nschema: {type: 7}\n":        "schema:",
		"stage: output\nthreshold: 2\n":                                 "threshold",
		"stage: output\ncheck: regex\npatterns: [x]\nmin_matches: -1\n": "min_matches",
		"stage: somewhere\n":                                            "before_model",
		"stage: output\ncheck: {a: b}\n":                                "front matter",
	} {
		assert.Contains(t, guard(t, front, "Text.").Problem, says, front)
	}
	assert.Contains(t, guard(t, "stage: output\n", " ").Problem, "say what to catch", "the judge needs words")
	assert.Empty(t, guard(t, "stage: output\ncheck: pii\n", "").Problem, "a deterministic check needs none")
}

// GT-42: each deterministic check, its catch the verdict with score 1.
func TestDeterministic(t *testing.T) {
	tests := []struct {
		name, front, subject string
		caught               bool
		check, says          string
	}{
		{"regex", "check: regex\npatterns: ['ACME-\\d+']\n", "see ACME-12 and ACME-9", true, CheckRegex, `"ACME-12" first`},
		{"regex, not enough", "check: regex\npatterns: ['ACME-\\d+']\nmin_matches: 3\n", "ACME-1 ACME-2", false, "", ""},
		{"keywords", "check: keywords\nwords: [Confidential]\n", "this is CONFIDENTIAL", true, CheckKeywords, `"Confidential"`},
		{"keywords, case", "check: keywords\nwords: [Confidential]\ncase_sensitive: true\n", "this is CONFIDENTIAL", false, "", ""},
		{"max_length", "check: max_length\nmax_length: 5\n", "héllo!", true, CheckMaxLength, "6 characters"},
		{"max_length, under", "check: max_length\nmax_length: 5\n", "héllo", false, "", ""},
		{"json_schema, not JSON", "check: json_schema\nschema: {type: object, required: [verdict]}\n", "pass", true, CheckJSONSchema, "isn't JSON"},
		{"json_schema, missing", "check: json_schema\nschema: {type: object, required: [verdict]}\n", `{"x":1}`, true, CheckJSONSchema, "doesn't hold"},
		{"json_schema, holds", "check: json_schema\nschema: {type: object, required: [verdict]}\n", ` {"verdict":"pass"} `, false, "", ""},
		{"pii", "check: pii\n", "mail ana@example.com", true, CheckPII, "personal data"},
		{"pii, none", "check: pii\n", "nothing here", false, "", ""},
		{"judge alone", "", "anything", false, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := guard(t, "stage: output\n"+tt.front, "Catch it.")
			require.Empty(t, g.Problem)
			v, caught := Deterministic(g, tt.subject)
			assert.Equal(t, tt.caught, caught)
			if tt.caught {
				assert.False(t, v.Pass)
				assert.Equal(t, 1.0, v.Score)
				assert.Equal(t, tt.check, v.Check)
				assert.Contains(t, v.Reason, tt.says)
			}
		})
	}
	long := strings.Repeat("x", 200)
	v, _ := Deterministic(guard(t, "stage: output\ncheck: regex\npatterns: ['x+']\n", ""), long)
	assert.Contains(t, v.Reason, "…", "a long match clipped")
}

// GT-42, GT-43: deterministic checks first, the judge only when they
// catch nothing, its score against the threshold.
func TestEvaluate(t *testing.T) {
	ctx := context.Background()
	asked := 0
	reply := `{"verdict": "fail", "score": 0.6, "reason": "close to a leak"}`
	ask := func(_ context.Context, _, _ string) (string, error) { asked++; return reply, nil }

	both := guard(t, "stage: output\ncheck: [regex, judge]\npatterns: [SECRET]\n", "No leaks.")
	v, err := Evaluate(ctx, ask, both, "the SECRET is out")
	require.NoError(t, err)
	assert.False(t, v.Pass)
	assert.Equal(t, CheckRegex, v.Check)
	assert.Zero(t, asked, "caught without the judge")
	v, err = Evaluate(ctx, ask, both, "all fine")
	require.NoError(t, err)
	assert.False(t, v.Pass, "the judge's fail, no threshold")
	assert.Equal(t, CheckJudge, v.Check)
	assert.Equal(t, 0.6, v.Score)
	assert.Equal(t, 1, asked)

	sure := guard(t, "stage: output\nthreshold: 0.8\n", "No leaks.")
	v, err = Evaluate(ctx, ask, sure, "all fine")
	require.NoError(t, err)
	assert.True(t, v.Pass, "0.6 is under 0.8: it lets it through")
	reply = `{"verdict": "pass", "score": 0.9, "reason": "a leak, surely"}`
	v, _ = Evaluate(ctx, ask, sure, "x")
	assert.False(t, v.Pass, "0.9 is at least 0.8, whatever the word")
	reply = `{"verdict": "fail", "score": 7, "reason": "off the scale"}`
	v, _ = Evaluate(ctx, ask, guard(t, "stage: output\n", "No leaks."), "x")
	assert.Equal(t, 1.0, v.Score, "a score off the scale reads as the verdict's")

	regexOnly := guard(t, "stage: output\ncheck: regex\npatterns: [SECRET]\n", "")
	v, err = Evaluate(ctx, nil, regexOnly, "fine")
	require.NoError(t, err)
	assert.True(t, v.Pass, "no judge needed")
	_, err = Evaluate(ctx, nil, both, "fine")
	assert.ErrorIs(t, err, ErrNoJudge)
	fails := func(context.Context, string, string) (string, error) { return "", errors.New("down") }
	_, err = Evaluate(ctx, fails, both, "fine")
	assert.Error(t, err)
}
