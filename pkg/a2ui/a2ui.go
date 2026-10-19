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

// Package a2ui is the part of A2UI v0.9 (https://a2ui.org) a workflow's
// human task uses, on the service's side (spec_workflows_043 WF-18): a
// form as the standard server-to-client messages any A2UI renderer can
// draw, and an answer (the client-to-server action message) checked
// against the form before a run goes on, so no client is trusted to have
// checked it. Nothing in a form runs as code: its functions are this
// package's catalog, by name.
package a2ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// Version is the A2UI version the messages carry.
const Version = "v0.9"

// CatalogID is the basic catalog's, whose components and functions forms use.
const CatalogID = "https://a2ui.org/specification/v0_9/catalogs/basic/catalog.json"

// Form is a surface's components (a flat list, one with the id root) and
// its data model.
type Form struct {
	Components []map[string]any `json:"components" yaml:"components"`
	Data       map[string]any   `json:"data,omitempty" yaml:"data,omitempty"`
}

// Messages is the form as A2UI's server-to-client messages for a surface:
// createSurface, updateComponents and updateDataModel (the whole model).
func (f Form) Messages(surfaceID string) []map[string]any {
	data := f.Data
	if data == nil {
		data = map[string]any{}
	}
	return []map[string]any{
		{"version": Version, "createSurface": map[string]any{"surfaceId": surfaceID, "catalogId": CatalogID, "sendDataModel": true}},
		{"version": Version, "updateComponents": map[string]any{"surfaceId": surfaceID, "components": f.Components}},
		{"version": Version, "updateDataModel": map[string]any{"surfaceId": surfaceID, "path": "/", "value": data}},
	}
}

// FormOf reads a surface's form back from its messages (the last
// components and the whole model they set; a path's update is applied).
func FormOf(messages []map[string]any) (Form, string, error) {
	var f Form
	surface := ""
	for _, m := range messages {
		switch {
		case m["createSurface"] != nil:
			cs, _ := m["createSurface"].(map[string]any)
			surface, _ = cs["surfaceId"].(string)
		case m["updateComponents"] != nil:
			uc, _ := m["updateComponents"].(map[string]any)
			list, _ := uc["components"].([]any)
			f.Components = f.Components[:0]
			for _, c := range list {
				if cm, ok := c.(map[string]any); ok {
					f.Components = append(f.Components, cm)
				}
			}
		case m["updateDataModel"] != nil:
			ud, _ := m["updateDataModel"].(map[string]any)
			path, _ := ud["path"].(string)
			if path == "" || path == "/" {
				f.Data, _ = ud["value"].(map[string]any)
			} else {
				f.Data = SetPath(f.Data, path, ud["value"])
			}
		}
	}
	if surface == "" {
		return f, "", errors.New("no createSurface message")
	}
	if !f.HasRoot() {
		return f, surface, errors.New("no component with the id root")
	}
	return f, surface, nil
}

// HasRoot reports whether a component has the id root.
func (f Form) HasRoot() bool {
	for _, c := range f.Components {
		if c["id"] == "root" {
			return true
		}
	}
	return false
}

// Action is a client's answer: A2UI's action message, from a button.
type Action struct {
	Name              string         `json:"name"`
	SurfaceID         string         `json:"surfaceId"`
	SourceComponentID string         `json:"sourceComponentId"`
	Timestamp         string         `json:"timestamp"`
	Context           map[string]any `json:"context"`
}

// ParseAction reads a client-to-server message ({"version": "v0.9",
// "action": {...}}); an error message, or anything else, is an error.
func ParseAction(raw []byte) (Action, error) {
	var m struct {
		Version string          `json:"version"`
		Action  *Action         `json:"action"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return Action{}, fmt.Errorf("not an A2UI message: %w", err)
	}
	if m.Version != Version {
		return Action{}, fmt.Errorf("A2UI version %q, not %s", m.Version, Version)
	}
	if m.Action == nil {
		return Action{}, errors.New("not an action message")
	}
	if m.Action.Name == "" || m.Action.SourceComponentID == "" {
		return Action{}, errors.New("an action needs its name and the component it came from")
	}
	if m.Action.Context == nil {
		m.Action.Context = map[string]any{}
	}
	return *m.Action, nil
}

// Problem is why an answer isn't accepted: a component's failing check,
// or what's wrong with the action itself (Component empty).
type Problem struct {
	Component string `json:"component,omitempty"`
	Message   string `json:"message"`
}

// String is the problem as a line: the component, then the message.
func (p Problem) String() string {
	if p.Component == "" {
		return p.Message
	}
	return p.Component + ": " + p.Message
}

// Answer checks an action against the form it answers and returns the
// answer a run gets: the action's name as "action", with its context's
// values. data is the surface's data model as the client had it (nil:
// the form's own, with the context's bound values put back): every
// component's checks must pass on it, the button's context must be what
// it resolves to, and the button must be one of the form's, with that
// action, and its own checks passing.
func (f Form) Answer(a Action, data map[string]any) (map[string]any, []Problem) {
	byID := map[string]map[string]any{}
	for _, c := range f.Components {
		if id, ok := c["id"].(string); ok {
			byID[id] = c
		}
	}
	btn, ok := byID[a.SourceComponentID]
	if !ok || btn["component"] != "Button" {
		return nil, []Problem{{Message: fmt.Sprintf("%q isn't one of the form's buttons", a.SourceComponentID)}}
	}
	event := eventOf(btn)
	if event == nil || event["name"] != a.Name {
		return nil, []Problem{{Component: a.SourceComponentID, Message: fmt.Sprintf("its action isn't %q", a.Name)}}
	}
	bindings, _ := event["context"].(map[string]any)
	for k := range a.Context {
		if _, ok := bindings[k]; !ok {
			return nil, []Problem{{Component: a.SourceComponentID, Message: fmt.Sprintf("its action sends no %q", k)}}
		}
	}
	if data == nil {
		data = clone(f.Data)
		for k, b := range bindings {
			if p, ok := pathOf(b); ok {
				if v, sent := a.Context[k]; sent {
					data = SetPath(data, p, v)
				}
			}
		}
	}
	var probs []Problem
	for k, b := range bindings {
		if want := Resolve(b, data, ""); !same(want, a.Context[k]) {
			probs = append(probs, Problem{Component: a.SourceComponentID, Message: fmt.Sprintf("%s isn't what the form holds", k)})
		}
	}
	probs = append(probs, f.Problems(data)...)
	if len(probs) > 0 {
		return nil, probs
	}
	answer := map[string]any{"action": a.Name}
	for k, v := range a.Context {
		answer[k] = v
	}
	return answer, nil
}

// Problems is every failing check of the components shown (from root,
// templates over the data's lists included), in the order they're drawn.
func (f Form) Problems(data map[string]any) []Problem {
	byID := map[string]map[string]any{}
	for _, c := range f.Components {
		if id, ok := c["id"].(string); ok {
			byID[id] = c
		}
	}
	var out []Problem
	var walk func(id, scope string, depth int)
	walk = func(id, scope string, depth int) {
		c, ok := byID[id]
		if !ok || depth > 24 {
			return
		}
		for _, m := range failing(c, data, scope) {
			out = append(out, Problem{Component: id, Message: m})
		}
		for _, k := range childrenOf(c, data, scope) {
			walk(k.id, k.scope, depth+1)
		}
	}
	walk("root", "", 0)
	return out
}

type child struct{ id, scope string }

// childrenOf is a component's children: its child, its list, or a template
// over a list in the data ({path, componentId}), each with its scope.
func childrenOf(c map[string]any, data map[string]any, scope string) []child {
	var out []child
	for _, key := range []string{"child", "content", "entryPointChild", "contentChild"} {
		if id, ok := c[key].(string); ok {
			out = append(out, child{id, scope})
		}
	}
	if tabs, ok := c["tabs"].([]any); ok {
		for _, t := range tabs {
			if tm, ok := t.(map[string]any); ok {
				if id, ok := tm["child"].(string); ok {
					out = append(out, child{id, scope})
				}
			}
		}
	}
	switch ch := c["children"].(type) {
	case []any:
		for _, x := range ch {
			if id, ok := x.(string); ok {
				out = append(out, child{id, scope})
			}
		}
	case map[string]any:
		p, _ := ch["path"].(string)
		tmpl, _ := ch["componentId"].(string)
		base := Scoped(p, scope)
		if items, ok := GetPath(data, base).([]any); ok && tmpl != "" {
			for i := range items {
				out = append(out, child{tmpl, base + "/" + strconv.Itoa(i)})
			}
		}
	}
	return out
}

// failing is a component's failing checks' messages.
func failing(c map[string]any, data map[string]any, scope string) []string {
	var out []string
	checks, _ := c["checks"].([]any)
	for _, ch := range checks {
		if Resolve(ch, data, scope) == false {
			msg := "doesn't pass its check"
			if m, ok := ch.(map[string]any); ok {
				if s, ok := m["message"].(string); ok && s != "" {
					msg = s
				}
			}
			out = append(out, msg)
		}
	}
	if re, ok := c["validationRegexp"].(string); ok {
		if v, ok := Resolve(c["value"], data, scope).(string); ok && v != "" {
			if r := safeRegexp(re); r != nil && !r.MatchString(v) {
				out = append(out, "doesn't pass its check")
			}
		}
	}
	return out
}

func eventOf(c map[string]any) map[string]any {
	a, _ := c["action"].(map[string]any)
	e, _ := a["event"].(map[string]any)
	return e
}

func pathOf(v any) (string, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return "", false
	}
	p, ok := m["path"].(string)
	return p, ok
}

// Resolve is a dynamic value: a literal, {path} in the data (relative to
// a template's item), or {call} of one of the catalog's functions.
func Resolve(v any, data map[string]any, scope string) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	if p, ok := m["path"].(string); ok {
		return GetPath(data, Scoped(p, scope))
	}
	if name, ok := m["call"].(string); ok {
		fn, ok := functions[name]
		if !ok {
			return nil
		}
		args := map[string]any{}
		raw, _ := m["args"].(map[string]any)
		for k, a := range raw {
			args[k] = Resolve(a, data, scope)
		}
		return fn(args)
	}
	return v
}

// Scoped is a path relative to a template's item (name in the item at
// /items/2 is /items/2/name); an absolute path stays as it is.
func Scoped(path, scope string) string {
	if strings.HasPrefix(path, "/") || scope == "" {
		return path
	}
	return scope + "/" + path
}

// GetPath is the value at a JSON Pointer path, or nil.
func GetPath(data any, path string) any {
	if path == "" || path == "/" {
		return data
	}
	cur := data
	for _, raw := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		key := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		switch x := cur.(type) {
		case map[string]any:
			cur = x[key]
		case []any:
			i, err := strconv.Atoi(key)
			if err != nil || i < 0 || i >= len(x) {
				return nil
			}
			cur = x[i]
		default:
			return nil
		}
	}
	return cur
}

// SetPath is data with value at path (copied along the way).
func SetPath(data map[string]any, path string, value any) map[string]any {
	keys := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, k := range keys {
		keys[i] = strings.ReplaceAll(strings.ReplaceAll(k, "~1", "/"), "~0", "~")
	}
	var put func(obj any, i int) map[string]any
	put = func(obj any, i int) map[string]any {
		base := map[string]any{}
		if m, ok := obj.(map[string]any); ok {
			for k, v := range m {
				base[k] = v
			}
		}
		if i == len(keys)-1 {
			base[keys[i]] = value
		} else {
			base[keys[i]] = put(base[keys[i]], i+1)
		}
		return base
	}
	return put(data, 0)
}

// functions are the basic catalog's validation, logic and formatting
// functions: the only ones a form can call.
var functions = map[string]func(map[string]any) any{
	"required": func(a map[string]any) any {
		switch v := a["value"].(type) {
		case nil:
			return false
		case string:
			return v != ""
		case []any:
			return len(v) > 0
		}
		return true
	},
	"regex": func(a map[string]any) any {
		v, ok := a["value"].(string)
		p, _ := a["pattern"].(string)
		r := safeRegexp(p)
		return ok && r != nil && r.MatchString(v)
	},
	"length": func(a map[string]any) any {
		n := 0
		switch v := a["value"].(type) {
		case string:
			n = len([]rune(v))
		case []any:
			n = len(v)
		}
		return within(float64(n), a)
	},
	"numeric": func(a map[string]any) any {
		n, ok := number(a["value"])
		return ok && within(n, a)
	},
	"email": func(a map[string]any) any {
		v, ok := a["value"].(string)
		return ok && emailRE.MatchString(v)
	},
	"and": func(a map[string]any) any {
		vs, ok := a["values"].([]any)
		if !ok {
			return false
		}
		for _, v := range vs {
			if !truthy(v) {
				return false
			}
		}
		return true
	},
	"or": func(a map[string]any) any {
		vs, _ := a["values"].([]any)
		for _, v := range vs {
			if truthy(v) {
				return true
			}
		}
		return false
	},
	"not":          func(a map[string]any) any { return !truthy(a["value"]) },
	"formatString": func(a map[string]any) any { return formatString(a) },
	"formatNumber": func(a map[string]any) any {
		n, ok := number(a["value"])
		if !ok {
			return ""
		}
		d, _ := number(a["decimals"])
		return strconv.FormatFloat(n, 'f', int(d), 64)
	},
	"pluralize": func(a map[string]any) any {
		if n, _ := number(a["count"]); n == 1 {
			s, _ := a["singular"].(string)
			return s
		}
		s, _ := a["plural"].(string)
		return s
	},
}

var (
	emailRE    = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	templateRE = regexp.MustCompile(`\$\{([^}]*)\}`)
)

func formatString(a map[string]any) string {
	t, ok := a["template"].(string)
	if !ok {
		t = fmt.Sprint(a["value"])
	}
	vals, _ := a["value"].(map[string]any)
	return templateRE.ReplaceAllStringFunc(t, func(m string) string {
		v := vals[templateRE.FindStringSubmatch(m)[1]]
		if v == nil {
			return ""
		}
		return fmt.Sprint(v)
	})
}

// safeRegexp is a form's pattern, or nil (not one, or too long to trust).
func safeRegexp(p string) *regexp.Regexp {
	if len(p) > 200 {
		return nil
	}
	r, err := regexp.Compile(p)
	if err != nil {
		return nil
	}
	return r
}

func within(n float64, a map[string]any) bool {
	if lo, ok := number(a["min"]); ok && n < lo {
		return false
	}
	if hi, ok := number(a["max"]); ok && n > hi {
		return false
	}
	return true
}

func number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, !math.IsNaN(x) && !math.IsInf(x, 0)
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil && x != "" && !math.IsNaN(f) && !math.IsInf(f, 0)
	}
	return 0, false
}

func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0
	case int:
		return x != 0
	}
	return true
}

// same compares two JSON values (numbers by value, whatever their Go type).
func same(a, b any) bool {
	return reflect.DeepEqual(normal(a), normal(b))
}

func normal(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	_ = json.Unmarshal(raw, &out)
	return out
}

// clone is a deep copy of a data model (through JSON: it holds only JSON).
func clone(m map[string]any) map[string]any {
	out, _ := normal(m).(map[string]any)
	if out == nil {
		out = map[string]any{}
	}
	return out
}
