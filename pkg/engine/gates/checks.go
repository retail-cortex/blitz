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
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/retail-cortex/blitz/pkg/redact"
)

// The checks a custom guardrail makes (spec_041 GT-41): the judge, and
// the deterministic ones, which need no model and run first.
const (
	CheckJudge      = "judge"       // the judge reads its text against the content (the default)
	CheckRegex      = "regex"       // patterns: a match is a catch
	CheckKeywords   = "keywords"    // words: one found is a catch
	CheckMaxLength  = "max_length"  // longer than max_length characters is a catch
	CheckJSONSchema = "json_schema" // content that isn't JSON valid against schema is a catch
	CheckPII        = "pii"         // personal data (emails, phone and card numbers…) is a catch
)

// Checks are every check, as a guardrail's check: may name them.
var Checks = []string{CheckJudge, CheckRegex, CheckKeywords, CheckMaxLength, CheckJSONSchema, CheckPII}

// The stages' names as the guardrail is placed around a model call or a
// tool (GT-40): before_model is input, after_model output, before_tool
// action, after_tool tool_result.
var stageAliases = map[string]string{
	"before_model": StageInput,
	"after_model":  StageOutput,
	"before_tool":  StageAction,
	"after_tool":   StageToolResult,
}

// checksOf is g's checks: those it names, else the judge.
func (g Guardrail) checksOf() []string {
	if len(g.Check) > 0 {
		return g.Check
	}
	return []string{CheckJudge}
}

// NeedsJudge reports whether g asks the judge (GT-41): a guardrail of
// deterministic checks alone runs without a model.
func (g Guardrail) NeedsJudge() bool { return slices.Contains(g.checksOf(), CheckJudge) }

// Streamable reports whether g can check an answer piece by piece as it
// streams (GT-45): only regex, keywords and pii, without the judge; a
// length or a schema needs the whole answer.
func (g Guardrail) Streamable() bool {
	for _, c := range g.checksOf() {
		if c != CheckRegex && c != CheckKeywords && c != CheckPII {
			return false
		}
	}
	return true
}

// checkChecks is the problem of g's checks and their settings ("" for
// none), compiling its patterns and reading its schema.
func checkChecks(g *Guardrail) string {
	for _, c := range g.Check {
		if !slices.Contains(Checks, c) {
			return fmt.Sprintf("check %q: %s", c, strings.Join(Checks, ", "))
		}
	}
	has := func(c string) bool { return slices.Contains(g.checksOf(), c) }
	g.compiled = nil
	switch {
	case has(CheckRegex) && len(g.Patterns) == 0:
		return "check regex: name the patterns (patterns: [...])"
	case has(CheckKeywords) && len(g.Words) == 0:
		return "check keywords: name the words (words: [...])"
	case has(CheckMaxLength) && g.MaxLength < 1:
		return "check max_length: max_length: the most characters, 1 or more"
	case has(CheckJSONSchema) && g.Schema == "":
		return "check json_schema: give the schema (schema: {...})"
	case has(CheckJudge) && strings.TrimSpace(g.Text) == "":
		return "the guardrail is empty: say what to catch"
	case g.Threshold < 0 || g.Threshold > 1:
		return fmt.Sprintf("threshold %v: from 0 to 1, the judge's score at which it acts", g.Threshold)
	case g.MinMatches < 0:
		return "min_matches: 1 or more"
	}
	if has(CheckRegex) {
		for _, p := range g.Patterns {
			re, err := regexp.Compile(p)
			if err != nil {
				return fmt.Sprintf("pattern %q: %v", p, err)
			}
			g.compiled = append(g.compiled, re)
		}
	}
	if has(CheckJSONSchema) {
		if _, err := resolveSchema(g.Schema); err != nil {
			return "schema: " + err.Error()
		}
	}
	return ""
}

func resolveSchema(text string) (*jsonschema.Resolved, error) {
	var s jsonschema.Schema
	if err := json.Unmarshal([]byte(text), &s); err != nil {
		return nil, err
	}
	return s.Resolve(nil)
}

// minMatches is how many matches make a catch (1 unless set).
func (g Guardrail) minMatches() int { return max(g.MinMatches, 1) }

// Deterministic runs g's deterministic checks on subject (GT-42): the
// first that catches it is a failed verdict, which check and why, with
// score 1; caught is false when none did (or it has none).
func Deterministic(g Guardrail, subject string) (v Verdict, caught bool) {
	fail := func(check, why string) (Verdict, bool) {
		return Verdict{Reason: why, Score: 1, Check: check}, true
	}
	for _, c := range g.checksOf() {
		switch c {
		case CheckRegex:
			n, first := 0, ""
			for _, re := range g.compiled {
				for _, m := range re.FindAllString(subject, -1) {
					n++
					if first == "" {
						first = m
					}
				}
			}
			if n >= g.minMatches() {
				return fail(c, fmt.Sprintf("it matches %d time(s), %q first", n, clip(first)))
			}
		case CheckKeywords:
			hay := subject
			if !g.CaseSensitive {
				hay = strings.ToLower(hay)
			}
			n, first := 0, ""
			for _, w := range g.Words {
				needle := w
				if !g.CaseSensitive {
					needle = strings.ToLower(w)
				}
				if k := strings.Count(hay, needle); k > 0 && needle != "" {
					n += k
					if first == "" {
						first = w
					}
				}
			}
			if n >= g.minMatches() {
				return fail(c, fmt.Sprintf("it says %q (%d time(s) in all)", first, n))
			}
		case CheckMaxLength:
			if n := utf8.RuneCountInString(subject); n > g.MaxLength {
				return fail(c, fmt.Sprintf("it's %d characters, more than %d", n, g.MaxLength))
			}
		case CheckJSONSchema:
			rs, err := resolveSchema(g.Schema)
			if err != nil {
				return fail(c, "its schema doesn't read: "+err.Error())
			}
			var doc any
			if err := json.Unmarshal([]byte(strings.TrimSpace(subject)), &doc); err != nil {
				return fail(c, "it isn't JSON")
			}
			if err := rs.Validate(doc); err != nil {
				return fail(c, "it doesn't hold to the schema: "+clip(err.Error()))
			}
		case CheckPII:
			if _, found := redact.PII(subject); found {
				return fail(c, "it holds personal data")
			}
		}
	}
	return Verdict{Pass: true}, false
}

// clip is s at most 120 characters, for a reason.
func clip(s string) string {
	if utf8.RuneCountInString(s) <= 120 {
		return s
	}
	return string([]rune(s)[:120]) + "…"
}

// ErrNoJudge is a guardrail that needs the judge where there's none.
var ErrNoJudge = errors.New("it can't be judged here")

// Evaluate checks subject against custom guardrail g (GT-42): its
// deterministic checks first, and a catch there is the verdict without a
// model; then, when it asks the judge, the judge's verdict, its score
// against g's threshold. A guardrail of deterministic checks alone that
// catches nothing passes.
func Evaluate(ctx context.Context, ask Asker, g Guardrail, subject string) (Verdict, error) {
	if v, caught := Deterministic(g, subject); caught {
		return v, nil
	}
	if !g.NeedsJudge() {
		return Verdict{Pass: true, Check: "deterministic"}, nil
	}
	if ask == nil {
		return Verdict{}, ErrNoJudge
	}
	return JudgeGuardrail(ctx, ask, g, subject)
}
