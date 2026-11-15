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

package agentpkg

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Dep is one asset another depends on, by name and version range
// (PK-03): an agent's skills. In YAML it's "name", "name@range", or
// {name: …, version: …}.
type Dep struct {
	Name    string `yaml:"name" json:"name"`
	Version string `yaml:"version,omitempty" json:"version,omitempty"`
}

// UnmarshalYAML reads any of a Dep's forms.
func (d *Dep) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		name, ver, _ := strings.Cut(n.Value, "@")
		*d = Dep{Name: strings.TrimSpace(name), Version: strings.TrimSpace(ver)}
		return nil
	}
	type plain Dep
	var p plain
	if err := n.Decode(&p); err != nil {
		return err
	}
	*d = Dep(p)
	return nil
}

// MarshalYAML writes d as "name" or "name@range".
func (d Dep) MarshalYAML() (any, error) {
	if d.Version == "" {
		return d.Name, nil
	}
	return d.Name + "@" + d.Version, nil
}

var depName = regexp.MustCompile(`^[a-z0-9]+([-_][a-z0-9]+)*$`)

// Check says what's wrong with d: a name Blitz can't have, a range that
// doesn't read.
func (d Dep) Check() error {
	if len(d.Name) > 64 || !depName.MatchString(d.Name) {
		return fmt.Errorf("%q: a name of lowercase letters, digits and single hyphens or underscores", d.Name)
	}
	if _, err := ParseRange(d.Version); err != nil {
		return fmt.Errorf("%s: %w", d.Name, err)
	}
	return nil
}

// Range is d's range (any when it doesn't read: Check says so).
func (d Dep) Range() Range {
	r, _ := ParseRange(d.Version)
	return r
}
