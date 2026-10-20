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

package databases

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/retail-cortex/blitz/pkg/engine/connectors"
)

// A connection file, as the toolbox backend writes it: one Toolbox source
// named after the connection, of its kind's source type, with its kind's
// fields; the ready-made tools switched on (<name>_<id>); and the user's
// queries (<name>_<query>, the kind's query type). A file that's more
// (other sources or tools, toolsets, unknown settings) is still served,
// and listed as Custom, edited as YAML.

// aboutRE is the comment that keeps a connection's description:
// Toolbox's sources take no description field.
var aboutRE = regexp.MustCompile(`(?m)^# about: (.*)$`)

// kindRE is the comment that names its kind.
var kindRE = regexp.MustCompile(`(?m)^# kind: ([a-z][a-z0-9-]*)\s*$`)

// doc is one YAML document of a connection file.
type doc = map[string]any

// Parse reads a connection file's text into the form: its kind is the
// one its comment names, else the first of kinds with its source's type.
func (Backend) Parse(cat connectors.Category, kinds []connectors.Kind, name, text string) connectors.Connection {
	c := connectors.Connection{Name: name, Category: cat.ID, Text: text, Settings: map[string]string{}}
	docs, err := decode(text)
	if err != nil {
		c.Custom, c.Problem = true, err.Error()
		return c
	}
	var source doc
	var tools []doc
	for _, d := range docs {
		switch d["kind"] {
		case "source", "sources":
			if source != nil {
				c.Custom = true
				return c
			}
			source = d
		case "tool", "tools":
			tools = append(tools, d)
		default:
			c.Custom = true
			return c
		}
	}
	if source == nil || source["name"] != name {
		c.Custom = true
		return c
	}
	typ, _ := source["type"].(string)
	var kind *connectors.Kind
	if m := kindRE.FindStringSubmatch(text); m != nil {
		if i := slices.IndexFunc(kinds, func(k connectors.Kind) bool { return k.ID == m[1] && k.Source == typ }); i >= 0 {
			kind = &kinds[i]
		}
	}
	if kind == nil {
		if i := slices.IndexFunc(kinds, func(k connectors.Kind) bool { return k.Source == typ }); i >= 0 {
			kind = &kinds[i]
		}
	}
	if kind == nil {
		c.Custom = true
		return c
	}
	c.Kind = kind.ID
	if m := aboutRE.FindStringSubmatch(text); m != nil {
		c.Description = strings.TrimSpace(m[1])
	}
	known := map[string]bool{"kind": true, "name": true, "type": true}
	if ro := kind.ReadOnly; ro != nil {
		known[ro.Key] = true
		c.ReadOnly = fmt.Sprint(source[ro.Key]) == fmt.Sprint(ro.Value)
	}
	// Toolbox's own readOnly makes any source read-only.
	if source["readOnly"] == true {
		known["readOnly"] = true
		c.ReadOnly = true
	}
	for _, f := range kind.Fields {
		known[f.Key] = true
		if v, ok := source[f.Key]; ok && v != nil {
			c.Settings[f.Key] = scalar(v)
		}
	}
	for k := range source {
		if !known[k] {
			c.Custom = true
		}
	}
	for _, t := range tools {
		tname, _ := t["name"].(string)
		ttype, _ := t["type"].(string)
		id, ok := strings.CutPrefix(tname, name+"_")
		if !ok || t["source"] != name {
			c.Custom = true
			continue
		}
		if r, ok := kind.Tool(id); ok && r.Type == ttype {
			c.Tools = append(c.Tools, id)
			continue
		}
		if kind.Queries == nil || ttype != kind.Queries.Type {
			c.Custom = true
			continue
		}
		q := connectors.Query{Name: id}
		q.Description, _ = t["description"].(string)
		q.Statement, _ = t["statement"].(string)
		q.Statement = strings.TrimRight(q.Statement, "\n")
		if ps, ok := t["parameters"].([]any); ok {
			for _, p := range ps {
				pm, _ := p.(doc)
				var pp connectors.Param
				pp.Name, _ = pm["name"].(string)
				pp.Type, _ = pm["type"].(string)
				pp.Description, _ = pm["description"].(string)
				q.Params = append(q.Params, pp)
			}
		}
		c.Queries = append(c.Queries, q)
	}
	return c
}

// scalar is a YAML scalar as text; a list, its items with commas.
func scalar(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		parts := make([]string, len(x))
		for i, p := range x {
			parts[i] = scalar(p)
		}
		return strings.Join(parts, ", ")
	}
	return fmt.Sprint(v)
}

// decode reads a file's YAML documents.
func decode(text string) ([]doc, error) {
	dec := yaml.NewDecoder(strings.NewReader(text))
	var out []doc
	for {
		var d doc
		err := dec.Decode(&d)
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		if d != nil {
			out = append(out, d)
		}
	}
}

// Check lists what Toolbox would refuse: a custom file that isn't YAML.
func (Backend) Check(k *connectors.Kind, c connectors.Connection) []string {
	if c.Custom {
		if _, err := decode(c.Text); err != nil {
			return []string{"the YAML: " + err.Error()}
		}
	}
	return nil
}

// Render writes c as Toolbox YAML: its source, then its ready-made tools
// and queries. A secret field always names its secret.
func (Backend) Render(cat connectors.Category, kind *connectors.Kind, c connectors.Connection) (string, error) {
	if c.Custom || kind == nil {
		return c.Text, nil
	}
	label := c.Name
	if c.Description != "" {
		label = c.Name + " (" + strings.TrimRight(c.Description, ".") + ")"
	}
	var docs []*yaml.Node
	src := mapping("kind", "source", "name", c.Name, "type", kind.Source)
	for _, f := range kind.Fields {
		v := strings.TrimSpace(c.Settings[f.Key])
		switch {
		case f.Secret():
			if v == "" {
				continue
			}
			v = "${" + connectors.SecretKey(cat, c.Name, f.Key) + "}"
		case v == "":
			if v = f.Default; v == "" {
				continue
			}
		}
		switch f.Type {
		case connectors.FieldList:
			var list []string
			for _, d := range strings.Split(v, ",") {
				if d = strings.TrimSpace(d); d != "" {
					list = append(list, d)
				}
			}
			addList(src, f.Key, list)
		case connectors.FieldNumber:
			src.Content = append(src.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: f.Key}, &yaml.Node{Kind: yaml.ScalarNode, Value: v, Tag: "!!int"})
		default:
			add(src, f.Key, v)
		}
	}
	if ro := kind.ReadOnly; ro != nil && c.ReadOnly {
		switch v := ro.Value.(type) {
		case bool:
			addTyped(src, ro.Key, fmt.Sprint(v), "!!bool")
		default:
			add(src, ro.Key, fmt.Sprint(v))
		}
	}
	docs = append(docs, src)
	for _, r := range kind.Tools {
		if !slices.Contains(c.Tools, r.ID) {
			continue
		}
		about := strings.ReplaceAll(r.Agent, "{connection}", label)
		if about == "" {
			about = r.ID + " on " + label + "."
		}
		t := mapping("kind", "tool", "name", c.Name+"_"+r.ID, "type", r.Type, "source", c.Name, "description", about)
		if r.Statement != "" {
			add(t, "statement", r.Statement)
		}
		docs = append(docs, t)
	}
	if kind.Queries != nil {
		for _, q := range c.Queries {
			docs = append(docs, queryDoc(c.Name, kind.Queries.Type, c.Name+"_"+q.Name, q))
		}
	}
	var b bytes.Buffer
	b.WriteString(connectors.KindComment(kind.ID))
	b.WriteString("# A connection for Blitz's agents (spec_connectors_047), written by Studio.\n# ${NAME} is the Blitz secret NAME.\n")
	if d := strings.Join(strings.Fields(c.Description), " "); d != "" {
		b.WriteString("# about: " + d + "\n")
	}
	if err := encode(&b, docs); err != nil {
		return "", err
	}
	return b.String(), nil
}

// queryDoc is a query as a Toolbox tool named name, of type typ, on source.
func queryDoc(source, typ, name string, q connectors.Query) *yaml.Node {
	t := mapping("kind", "tool", "name", name, "type", typ, "source", source, "description", q.Description)
	stmt := &yaml.Node{Kind: yaml.ScalarNode, Value: strings.TrimSpace(q.Statement) + "\n", Style: yaml.LiteralStyle}
	t.Content = append(t.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "statement"}, stmt)
	if len(q.Params) > 0 {
		seq := &yaml.Node{Kind: yaml.SequenceNode}
		for _, p := range q.Params {
			seq.Content = append(seq.Content, mapping("name", p.Name, "type", p.Type, "description", p.Description))
		}
		t.Content = append(t.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "parameters"}, seq)
	}
	return t
}

// encode writes docs to b as YAML documents.
func encode(b *bytes.Buffer, docs []*yaml.Node) error {
	enc := yaml.NewEncoder(b)
	enc.SetIndent(2)
	for _, d := range docs {
		if err := enc.Encode(d); err != nil {
			return err
		}
	}
	return enc.Close()
}

func mapping(kv ...string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode}
	for i := 0; i+1 < len(kv); i += 2 {
		add(n, kv[i], kv[i+1])
	}
	return n
}

func add(n *yaml.Node, k, v string) { addTyped(n, k, v, "!!str") }

func addTyped(n *yaml.Node, k, v, tag string) {
	n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, &yaml.Node{Kind: yaml.ScalarNode, Value: v, Tag: tag})
}

func addList(n *yaml.Node, k string, vs []string) {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	for _, v := range vs {
		seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: v, Tag: "!!str"})
	}
	n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, seq)
}
