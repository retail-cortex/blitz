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

package a2ui

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// refund is a form like a workflow's: an amount (numeric, 1–500), a
// required note, a list of items drawn by a template (each needs a name),
// and approve or reject.
func refund(t *testing.T) Form {
	t.Helper()
	var f Form
	require.NoError(t, json.Unmarshal([]byte(`{
	  "components": [
	    {"id": "root", "component": "Column", "children": ["amount", "note", "items", "buttons"]},
	    {"id": "amount", "component": "TextField", "label": "Refund", "value": {"path": "/amount"},
	     "checks": [{"call": "numeric", "args": {"value": {"path": "/amount"}, "min": 1, "max": 500}, "message": "1 to 500"}]},
	    {"id": "note", "component": "TextField", "label": "Note", "value": {"path": "/note"},
	     "checks": [{"call": "required", "args": {"value": {"path": "/note"}}, "message": "say why"}]},
	    {"id": "items", "component": "List", "children": {"path": "/items", "componentId": "item"}},
	    {"id": "item", "component": "TextField", "value": {"path": "name"},
	     "checks": [{"call": "required", "args": {"value": {"path": "name"}}, "message": "name it"}]},
	    {"id": "buttons", "component": "Row", "children": ["reject", "approve"]},
	    {"id": "reject", "component": "Button", "child": "reject_label", "action": {"event": {"name": "reject", "context": {}}}},
	    {"id": "approve", "component": "Button", "child": "approve_label",
	     "action": {"event": {"name": "approve", "context": {"amount": {"path": "/amount"}, "note": {"path": "/note"}}}}},
	    {"id": "hidden", "component": "TextField", "checks": [{"call": "required", "args": {"value": ""}, "message": "never shown"}]}
	  ],
	  "data": {"amount": 240, "note": "", "items": [{"name": "kettle"}]}
	}`), &f))
	return f
}

func TestMessagesRoundTrip(t *testing.T) {
	f := refund(t)
	msgs := f.Messages("run-7/approve")
	require.Len(t, msgs, 3)
	assert.Equal(t, Version, msgs[0]["version"])
	assert.Equal(t, CatalogID, msgs[0]["createSurface"].(map[string]any)["catalogId"])

	// Through JSON, as a client gets them.
	raw, err := json.Marshal(msgs)
	require.NoError(t, err)
	var back []map[string]any
	require.NoError(t, json.Unmarshal(raw, &back))
	got, surface, err := FormOf(back)
	require.NoError(t, err)
	assert.Equal(t, "run-7/approve", surface)
	assert.Len(t, got.Components, len(f.Components))
	assert.Equal(t, 240.0, got.Data["amount"])

	// A path's update is applied.
	back = append(back, map[string]any{"version": Version, "updateDataModel": map[string]any{"surfaceId": surface, "path": "/note", "value": "late"}})
	got, _, err = FormOf(back)
	require.NoError(t, err)
	assert.Equal(t, "late", got.Data["note"])

	_, _, err = FormOf(back[1:])
	assert.ErrorContains(t, err, "createSurface")
	_, _, err = FormOf(back[:1])
	assert.ErrorContains(t, err, "root")
	assert.Equal(t, map[string]any{}, Form{}.Messages("s")[2]["updateDataModel"].(map[string]any)["value"])
}

func TestParseAction(t *testing.T) {
	cases := []struct {
		name, raw, err string
	}{
		{name: "an action", raw: `{"version": "v0.9", "action": {"name": "approve", "surfaceId": "s", "sourceComponentId": "approve", "timestamp": "2026-10-18T09:00:00Z", "context": {"note": "ok"}}}`},
		{name: "no context", raw: `{"version": "v0.9", "action": {"name": "reject", "sourceComponentId": "reject"}}`},
		{name: "not JSON", raw: `{`, err: "not an A2UI message"},
		{name: "another version", raw: `{"version": "v0.8", "action": {"name": "a", "sourceComponentId": "b"}}`, err: "version"},
		{name: "an error message", raw: `{"version": "v0.9", "error": {"code": "X", "message": "m", "surfaceId": "s"}}`, err: "not an action"},
		{name: "no name", raw: `{"version": "v0.9", "action": {"sourceComponentId": "b"}}`, err: "its name"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, err := ParseAction([]byte(c.raw))
			if c.err != "" {
				assert.ErrorContains(t, err, c.err)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, a.Context)
		})
	}
}

func TestAnswer(t *testing.T) {
	approve := func(ctx map[string]any) Action {
		return Action{Name: "approve", SurfaceID: "s", SourceComponentID: "approve", Context: ctx}
	}
	good := map[string]any{"amount": 240.0, "note": "over the limit, but a regular", "items": []any{map[string]any{"name": "kettle"}}}
	cases := []struct {
		name   string
		action Action
		data   map[string]any
		want   map[string]any
		probs  []string
	}{
		{name: "approved, with the data model", action: approve(map[string]any{"amount": 240, "note": "over the limit, but a regular"}), data: good,
			want: map[string]any{"action": "approve", "amount": 240, "note": "over the limit, but a regular"}},
		{name: "approved, the context alone", action: approve(map[string]any{"amount": 99, "note": "fine"}),
			want: map[string]any{"action": "approve", "amount": 99, "note": "fine"}},
		{name: "rejected: nothing to check but the form", action: Action{Name: "reject", SourceComponentID: "reject", Context: map[string]any{}}, data: good,
			want: map[string]any{"action": "reject"}},
		{name: "a required note left empty", action: approve(map[string]any{"amount": 240, "note": ""}), probs: []string{"note: say why"}},
		{name: "an amount out of range", action: approve(map[string]any{"amount": 900, "note": "x"}), probs: []string{"amount: 1 to 500"}},
		{name: "a template's item", action: approve(map[string]any{"amount": 240, "note": "x"}),
			data: map[string]any{"amount": 240, "note": "x", "items": []any{map[string]any{"name": ""}}}, probs: []string{"item: name it"}},
		{name: "context not what the data holds", action: approve(map[string]any{"amount": 1, "note": "x"}), data: good,
			probs: []string{"approve: amount isn't what the form holds", "approve: note isn't what the form holds"}},
		{name: "a button that isn't there", action: Action{Name: "approve", SourceComponentID: "nope"}, probs: []string{`"nope" isn't one of the form's buttons`}},
		{name: "not a button", action: Action{Name: "approve", SourceComponentID: "note"}, probs: []string{`"note" isn't one of the form's buttons`}},
		{name: "another action", action: Action{Name: "refund-all", SourceComponentID: "approve"}, probs: []string{`approve: its action isn't "refund-all"`}},
		{name: "a value it doesn't send", action: approve(map[string]any{"amount": 240, "note": "x", "admin": true}), probs: []string{`approve: its action sends no "admin"`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, probs := refund(t).Answer(c.action, c.data)
			if c.probs != nil {
				var msgs []string
				for _, p := range probs {
					msgs = append(msgs, p.String())
				}
				assert.ElementsMatch(t, c.probs, msgs)
				assert.Nil(t, got)
				return
			}
			assert.Empty(t, probs)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestFunctions(t *testing.T) {
	data := map[string]any{"n": "12", "s": "abc", "list": []any{1.0, 2.0}, "who": map[string]any{"name": "Ana"}}
	call := func(name string, args map[string]any) any {
		return Resolve(map[string]any{"call": name, "args": args}, data, "")
	}
	p := func(path string) map[string]any { return map[string]any{"path": path} }
	cases := []struct {
		name string
		got  any
		want any
	}{
		{"required string", call("required", map[string]any{"value": p("/s")}), true},
		{"required missing", call("required", map[string]any{"value": p("/none")}), false},
		{"required empty list", call("required", map[string]any{"value": []any{}}), false},
		{"required number", call("required", map[string]any{"value": 0.0}), true},
		{"regex", call("regex", map[string]any{"value": p("/s"), "pattern": "^a"}), true},
		{"regex bad pattern", call("regex", map[string]any{"value": p("/s"), "pattern": "("}), false},
		{"length", call("length", map[string]any{"value": p("/s"), "min": 2, "max": 3}), true},
		{"length list", call("length", map[string]any{"value": p("/list"), "min": 3}), false},
		{"numeric string", call("numeric", map[string]any{"value": p("/n"), "max": 20}), true},
		{"numeric not", call("numeric", map[string]any{"value": p("/s")}), false},
		{"numeric empty", call("numeric", map[string]any{"value": ""}), false},
		{"email", call("email", map[string]any{"value": "a@b.co"}), true},
		{"email not", call("email", map[string]any{"value": "a@b"}), false},
		{"and", call("and", map[string]any{"values": []any{true, "x", 1.0}}), true},
		{"and not", call("and", map[string]any{"values": []any{true, ""}}), false},
		{"and no list", call("and", map[string]any{}), false},
		{"or", call("or", map[string]any{"values": []any{false, nil, true}}), true},
		{"or none", call("or", map[string]any{"values": []any{false, 0.0}}), false},
		{"not", call("not", map[string]any{"value": false}), true},
		{"formatString", call("formatString", map[string]any{"template": "Hi ${name}!", "value": p("/who")}), "Hi Ana!"},
		{"formatString missing", call("formatString", map[string]any{"template": "${x}.", "value": p("/who")}), "."},
		{"formatNumber", call("formatNumber", map[string]any{"value": p("/n"), "decimals": 2}), "12.00"},
		{"formatNumber not", call("formatNumber", map[string]any{"value": "x"}), ""},
		{"pluralize one", call("pluralize", map[string]any{"count": 1, "singular": "item", "plural": "items"}), "item"},
		{"pluralize many", call("pluralize", map[string]any{"count": 3, "singular": "item", "plural": "items"}), "items"},
		{"unknown function", call("exec", map[string]any{}), nil},
		{"a literal", Resolve("plain", data, ""), "plain"},
		{"a map literal", Resolve(map[string]any{"x": 1}, data, ""), map[string]any{"x": 1}},
		{"scoped path", Resolve(p("name"), data, "/who"), "Ana"},
		{"list index", GetPath(data, "/list/1"), 2.0},
		{"bad index", GetPath(data, "/list/9"), nil},
		{"through a string", GetPath(data, "/s/x"), nil},
		{"escaped key", GetPath(map[string]any{"a/b": 1}, "/a~1b"), 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { assert.Equal(t, c.want, c.got) })
	}
	assert.Nil(t, safeRegexp(string(make([]byte, 300))), "too long to trust")
	assert.Equal(t, map[string]any{"a": map[string]any{"b": 1}}, SetPath(nil, "/a/b", 1))
	assert.Equal(t, "x", Problem{Message: "x"}.String())
}

func TestValidationRegexp(t *testing.T) {
	f := Form{Components: []map[string]any{
		{"id": "root", "component": "Card", "child": "zip"},
		{"id": "zip", "component": "TextField", "value": map[string]any{"path": "/zip"}, "validationRegexp": `^\d{5}$`},
	}}
	assert.Empty(t, f.Problems(map[string]any{"zip": "12345"}))
	assert.Empty(t, f.Problems(map[string]any{"zip": ""}), "empty is for required to say")
	assert.Len(t, f.Problems(map[string]any{"zip": "12"}), 1)
	tabs := Form{Components: []map[string]any{
		{"id": "root", "component": "Tabs", "tabs": []any{map[string]any{"title": "A", "child": "a"}}},
		{"id": "a", "component": "Text", "checks": []any{map[string]any{"call": "required", "args": map[string]any{"value": ""}}}},
	}}
	assert.Equal(t, []Problem{{Component: "a", Message: "doesn't pass its check"}}, tabs.Problems(nil))
}
