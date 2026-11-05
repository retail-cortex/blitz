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

package connectors

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/config"
)

// ErrOrgOwned is the refusal to change one of the organization's
// connections: its Admins change it, through a review.
var ErrOrgOwned = errors.New("this connection is your organization's: its Admins change it, through a review")

// IsOrg says whether scope is one of the organization's catalog's.
func IsOrg(scope string) bool { return scope == ScopeOrgLocked || scope == ScopeOrgDefault }

// orgRules is how the organization's catalog bears on one category's
// connections (spec_053 EN-30, EN-35): its locked ones take the place of
// the person's of their names, its defaults give way to the person's, and
// its policy may refuse the person's own, or kinds it doesn't list.
type orgRules struct {
	active bool
	policy config.OrgPolicy
	cat    Catalog
	c      Category
	locked map[string]bool   // the organization's locked connections' names
	own    map[string]string // the person's (global and workspace) that read: name → text
}

// rulesFor is the organization's rules for c's connections in d.
func rulesFor(d Dirs, cat Catalog, c Category) orgRules {
	o := config.OrgLayerFor(d.Prefix)
	r := orgRules{active: o.Active(), policy: o.Policy, cat: cat, c: c, locked: map[string]bool{}, own: map[string]string{}}
	if !r.active {
		return r
	}
	for _, f := range d.Files(ScopeOrgLocked, c.ID) {
		r.locked[idOf(f)] = true
	}
	for _, scope := range []string{ScopeGlobal, ScopeWorkspace} {
		for _, f := range d.Files(scope, c.ID) {
			// One that can't be read isn't served, so a default of its
			// name doesn't give way to it.
			if text, err := os.ReadFile(f); err == nil {
				r.own[idOf(f)] = string(text)
			}
		}
	}
	return r
}

// refusal is why a connection in scope, named name, whose file holds
// text, isn't used: "" when it is.
func (r orgRules) refusal(scope, name, text string) string {
	if !r.active {
		return ""
	}
	switch scope {
	case ScopeOrgLocked:
		return ""
	case ScopeOrgDefault:
		// It gives way only to one of the person's that's used.
		if t, ok := r.own[name]; ok && r.personal(name, t) == "" {
			return "your own connection of this name takes its place"
		}
		return ""
	}
	return r.personal(name, text)
}

// personal is why one of the person's own connections isn't used.
func (r orgRules) personal(name, text string) string {
	switch {
	case r.locked[name]:
		return "your organization's locked connection of this name takes its place"
	case !r.policy.Connectors.Own:
		return "your organization doesn't allow connections of your own"
	}
	return r.kindRefusal(text)
}

// kindRefusal is why a file isn't of a kind the organization allows:
// judged by what it connects to (its backend's source types), not by its
// "# kind:" comment, which anyone can write. A file whose sources can't
// be told isn't allowed when kinds are listed.
func (r orgRules) kindRefusal(text string) string {
	allowed := r.policy.Connectors.Kinds
	if len(allowed) == 0 {
		return ""
	}
	var types []string
	var err error
	b, ok := backendFor(r.cat, r.c, text)
	if l, can := b.(SourceLister); ok && can {
		types, err = l.SourceTypes(text)
	}
	if err != nil || len(types) == 0 {
		return fmt.Sprintf("your organization allows only %s connections, and what this file connects to can't be told", strings.Join(allowed, ", "))
	}
	kinds := r.cat.KindsOf(r.c.ID, "")
	for _, t := range types {
		if slices.ContainsFunc(kinds, func(k Kind) bool { return k.Source == t && slices.Contains(allowed, k.ID) }) {
			continue
		}
		name := t
		if i := slices.IndexFunc(kinds, func(k Kind) bool { return k.Source == t }); i >= 0 {
			name = kinds[i].ID
		}
		return fmt.Sprintf("your organization doesn't allow %s connections", name)
	}
	return ""
}
