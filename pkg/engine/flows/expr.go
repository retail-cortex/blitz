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

package flows

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// Expressions (spec_workflows_043 WF-13): conditions on links and
// {{ … }} in step settings are CEL, on these variables:
//
//	input   the run's input (text)
//	output  the step's own result, for a link's condition: {text, data}
//	steps   every finished step's result, by ID: steps.lookup.data.total
//	        (an ID with a hyphen: steps["look-up"])

// Result is what a step produced: its text, and its data when it has
// some (JSON an agent answered with, an extract step's fields, an API's
// response).
type Result struct {
	Text string `json:"text"`
	Data any    `json:"data,omitempty"`
}

// Value is r as CEL sees it.
func (r Result) Value() map[string]any {
	return map[string]any{"text": r.Text, "data": plain(r.Data)}
}

// plain is v as CEL's dynamic values take it (JSON's types).
func plain(v any) any {
	if v == nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out any
	_ = json.Unmarshal(data, &out)
	return out
}

var (
	envOnce sync.Once
	env     *cel.Env
	envErr  error
)

func celEnv() (*cel.Env, error) {
	envOnce.Do(func() {
		env, envErr = cel.NewEnv(
			cel.Variable("input", cel.StringType),
			cel.Variable("output", cel.DynType),
			cel.Variable("steps", cel.MapType(cel.StringType, cel.DynType)),
		)
	})
	return env, envErr
}

// CheckExpr reports whether expr is a CEL expression on the variables.
func CheckExpr(expr string) error {
	e, err := celEnv()
	if err != nil {
		return err
	}
	if _, iss := e.Compile(expr); iss != nil && iss.Err() != nil {
		return fmt.Errorf("%s", strings.TrimSpace(iss.Err().Error()))
	}
	return nil
}

// Vars are what an expression is evaluated against.
type Vars struct {
	Input  string
	Output *Result
	Steps  map[string]Result
}

func (v Vars) activation() map[string]any {
	steps := map[string]any{}
	for id, r := range v.Steps {
		steps[id] = r.Value()
	}
	act := map[string]any{"input": v.Input, "steps": steps, "output": nil}
	if v.Output != nil {
		act["output"] = v.Output.Value()
	}
	return act
}

// Eval evaluates expr against vars.
func Eval(expr string, vars Vars) (ref.Val, error) {
	e, err := celEnv()
	if err != nil {
		return nil, err
	}
	ast, iss := e.Compile(expr)
	if iss != nil && iss.Err() != nil {
		return nil, fmt.Errorf("%s", strings.TrimSpace(iss.Err().Error()))
	}
	prg, err := e.Program(ast, cel.CostLimit(1_000_000))
	if err != nil {
		return nil, err
	}
	out, _, err := prg.Eval(vars.activation())
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Holds evaluates a condition: it must be true or false.
func Holds(expr string, vars Vars) (bool, error) {
	v, err := Eval(expr, vars)
	if err != nil {
		return false, err
	}
	b, ok := v.(types.Bool)
	if !ok {
		return false, fmt.Errorf("%s is %s, not true or false", expr, v.Type().TypeName())
	}
	return bool(b), nil
}

// anyType is what values convert to for JSON.
var anyType = reflectAny()

// templateRE is a {{ expression }} in a setting.
var templateRE = regexp.MustCompile(`\{\{\s*(.+?)\s*\}\}`)

// CheckTemplate reports whether each {{ … }} in text is an expression.
func CheckTemplate(text string) error {
	for _, m := range templateRE.FindAllStringSubmatch(text, -1) {
		if err := CheckExpr(m[1]); err != nil {
			return fmt.Errorf("{{ %s }}: %w", m[1], err)
		}
	}
	return nil
}

// Fill replaces each {{ expression }} in text with its value: text as it
// is, anything else as JSON.
func Fill(text string, vars Vars) (string, error) {
	var firstErr error
	out := templateRE.ReplaceAllStringFunc(text, func(m string) string {
		expr := templateRE.FindStringSubmatch(m)[1]
		v, err := Eval(expr, vars)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("{{ %s }}: %w", expr, err)
			}
			return m
		}
		if s, ok := v.Value().(string); ok {
			return s
		}
		native, err := v.ConvertToNative(anyType)
		if err != nil {
			return fmt.Sprint(v.Value())
		}
		data, err := json.Marshal(native)
		if err != nil {
			return fmt.Sprint(v.Value())
		}
		return string(data)
	})
	return out, firstErr
}

// DataOf is the JSON in an answer, if it's JSON alone (a code fence
// around it allowed): an object or a list.
func DataOf(text string) any {
	t := strings.TrimSpace(text)
	if strings.HasPrefix(t, "```") {
		t = strings.TrimPrefix(t, "```json")
		t = strings.TrimPrefix(t, "```")
		t = strings.TrimSuffix(strings.TrimSpace(t), "```")
		t = strings.TrimSpace(t)
	}
	if !strings.HasPrefix(t, "{") && !strings.HasPrefix(t, "[") {
		return nil
	}
	var v any
	if json.Unmarshal([]byte(t), &v) != nil {
		return nil
	}
	return v
}

func reflectAny() reflect.Type { return reflect.TypeOf((*any)(nil)).Elem() }

// checkValue reports whether each string in v (a map, a list or a
// string) is a template that holds.
func checkValue(v any) error {
	switch x := v.(type) {
	case string:
		return CheckTemplate(x)
	case map[string]any:
		for _, e := range x {
			if err := checkValue(e); err != nil {
				return err
			}
		}
	case []any:
		for _, e := range x {
			if err := checkValue(e); err != nil {
				return err
			}
		}
	}
	return nil
}

// FillValue is v (a map, a list, a string, or anything else as it is)
// with each string's templates filled in; a string that is one template
// alone takes the expression's value, not its text ("{{ steps.a.data }}"
// stays an object).
func FillValue(v any, vars Vars) (any, error) {
	switch x := v.(type) {
	case string:
		if m := templateRE.FindStringSubmatch(x); m != nil && m[0] == strings.TrimSpace(x) {
			val, err := Eval(m[1], vars)
			if err != nil {
				return nil, fmt.Errorf("{{ %s }}: %w", m[1], err)
			}
			native, err := val.ConvertToNative(anyType)
			if err != nil {
				return val.Value(), nil
			}
			return plain(native), nil
		}
		return Fill(x, vars)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			f, err := FillValue(e, vars)
			if err != nil {
				return nil, err
			}
			out[k] = f
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			f, err := FillValue(e, vars)
			if err != nil {
				return nil, err
			}
			out[i] = f
		}
		return out, nil
	}
	return v, nil
}
