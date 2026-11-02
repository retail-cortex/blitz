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
	locked map[string]bool   // the organization's locked connections' names
	own    map[string]string // the person's (global and workspace): name → kind
}

// rulesFor is the organization's rules for category's connections in d.
func rulesFor(d Dirs, category string) orgRules {
	o := config.OrgLayerFor(d.Prefix)
	r := orgRules{active: o.Active(), policy: o.Policy, locked: map[string]bool{}, own: map[string]string{}}
	if !r.active {
		return r
	}
	for _, f := range d.Files(ScopeOrgLocked, category) {
		r.locked[idOf(f)] = true
	}
	for _, scope := range []string{ScopeGlobal, ScopeWorkspace} {
		for _, f := range d.Files(scope, category) {
			text, _ := os.ReadFile(f)
			r.own[idOf(f)] = KindOf(string(text))
		}
	}
	return r
}

// refusal is why a connection in scope, named name, of kind (""
// unknown), isn't used: "" when it is.
func (r orgRules) refusal(scope, name, kind string) string {
	if !r.active {
		return ""
	}
	switch scope {
	case ScopeOrgLocked:
		return ""
	case ScopeOrgDefault:
		// It gives way only to one of the person's that's used.
		if k, ok := r.own[name]; ok && r.personal(name, k) == "" {
			return "your own connection of this name takes its place"
		}
		return ""
	}
	return r.personal(name, kind)
}

// personal is why one of the person's own connections isn't used.
func (r orgRules) personal(name, kind string) string {
	switch {
	case r.locked[name]:
		return "your organization's locked connection of this name takes its place"
	case !r.policy.Connectors.Own:
		return "your organization doesn't allow connections of your own"
	case len(r.policy.Connectors.Kinds) > 0 && kind != "" && !slices.Contains(r.policy.Connectors.Kinds, kind):
		return fmt.Sprintf("your organization doesn't allow %s connections", kind)
	}
	return ""
}
