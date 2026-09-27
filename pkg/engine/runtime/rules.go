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
	"maps"
	"path/filepath"
	"strings"
	"sync"

	"github.com/retail-cortex/blitz/pkg/engine/memory"
)

// RulesKey is the tool-result field that carries path-scoped rules the
// first time a tool touches a file they apply to.
const RulesKey = "project_rules"

// WithScopedRules hands rules scoped to paths to agents as they touch
// matching files (see memory.Rule).
func WithScopedRules(rules []memory.Rule) Option {
	return func(e *Engine) { e.scoped.set(rules) }
}

// SetScopedRules replaces the path-scoped rules (after /memory reload).
// Rules already handed out in a session aren't handed out again.
func (e *Engine) SetScopedRules(rules []memory.Rule) { e.scoped.set(rules) }

// scopedRules are the rules and, per session, which were handed out.
type scopedRules struct {
	mu    sync.Mutex
	rules []memory.Rule
	given map[string]map[string]bool // session ID -> rule path
}

func (s *scopedRules) set(rules []memory.Rule) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rules = rules
}

// take returns the rules matching any of paths not yet handed out in
// session, and marks them handed out.
func (s *scopedRules) take(session string, paths []string) []*memory.Rule {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.rules) == 0 || len(paths) == 0 {
		return nil
	}
	if s.given == nil {
		s.given = map[string]map[string]bool{}
	}
	given := s.given[session]
	if given == nil {
		given = map[string]bool{}
		s.given[session] = given
	}
	var out []*memory.Rule
	for i := range s.rules {
		r := &s.rules[i]
		if given[r.Path] {
			continue
		}
		for _, p := range paths {
			if r.Matches(p) {
				given[r.Path] = true
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// toolPaths returns the files a tool call reads or edits, absolute. Tools
// that only list or search (list_files, glob, grep) don't count: a rule is
// for working on a file, not for seeing its name.
func toolPaths(workspace, tool string, args map[string]any) []string {
	var raw []string
	switch tool {
	case "read_file", "create_file", "replace_in_file", "edit", "delete_snippet", "delete_file", "view_image", "notebook_edit":
		if p, ok := args["path"].(string); ok && p != "" {
			raw = append(raw, p)
		}
	case "apply_patch":
		patch, _ := args["patch"].(string)
		raw = patchPaths(patch)
	}
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		if !filepath.IsAbs(p) {
			p = filepath.Join(workspace, p)
		}
		out = append(out, canonical(filepath.Clean(p)))
	}
	return out
}

// canonical resolves symlinks in p, or in its directory when p doesn't
// exist (a file about to be created), so it compares with rule roots.
func canonical(p string) string {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	if dir, err := filepath.EvalSymlinks(filepath.Dir(p)); err == nil {
		return filepath.Join(dir, filepath.Base(p))
	}
	return p
}

// patchPaths finds the files a patch names, in either format apply_patch
// takes.
func patchPaths(patch string) []string {
	var out []string
	for line := range strings.Lines(patch) {
		line = strings.TrimRight(line, "\r\n")
		for _, prefix := range []string{"+++ ", "--- ", "*** Update File: ", "*** Add File: ", "*** Delete File: ", "*** Move to: "} {
			if p, ok := strings.CutPrefix(line, prefix); ok {
				p = strings.TrimSpace(p)
				if i := strings.IndexByte(p, '\t'); i >= 0 {
					p = p[:i]
				}
				p = strings.TrimPrefix(strings.TrimPrefix(p, "a/"), "b/")
				if p != "" && p != "/dev/null" {
					out = append(out, p)
				}
			}
		}
	}
	return out
}

// attachRules returns result with the rules for the files the tool
// touched, or nil when there are none. Failed calls don't take rules: the
// agent hasn't worked on the file.
func (e *Engine) attachRules(ctx interface {
	context.Context
	SessionID() string
}, tool string, args, result map[string]any, toolErr error) map[string]any {
	if toolErr != nil {
		return nil
	}
	if msg, _ := result["error"].(string); msg != "" {
		return nil
	}
	rules := e.scoped.take(ctx.SessionID(), toolPaths(e.toolReg.Workspace().Dir(), tool, args))
	if len(rules) == 0 {
		return nil
	}
	out := maps.Clone(result)
	if out == nil {
		out = map[string]any{}
	}
	out[RulesKey] = memory.RenderRules(rules)
	return out
}
