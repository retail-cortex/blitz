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

package server

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// specPath is spec_021, whose SVC-30 lists the reasons a client can match.
const specPath = "../../../docs/content/about/specs/spec_021_service.md"

// isReason is what a reason looks like.
var isReason = regexp.MustCompile(`^[A-Z][A-Z0-9_]*[A-Z0-9]$`)

// sentReasons are the reasons the package's code sends, read from its
// source: apiError's reason and ErrorInfo's Reason, a literal or a
// constant. dynamic are the places a reason is made as the service runs
// (file:function), which SVC-30 describes in words.
func sentReasons(t *testing.T) (sent map[string]string, dynamic []string) {
	t.Helper()
	fset := token.NewFileSet()
	names, err := filepath.Glob("*.go")
	require.NoError(t, err)
	var files []*ast.File
	consts := map[string]string{}
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		require.NoError(t, err)
		files = append(files, f)
		for _, d := range f.Decls {
			if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.CONST {
				for _, spec := range g.Specs {
					vs := spec.(*ast.ValueSpec)
					for i, n := range vs.Names {
						if i < len(vs.Values) {
							if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
								consts[n.Name], _ = strconv.Unquote(lit.Value)
							}
						}
					}
				}
			}
		}
	}
	require.NotEmpty(t, files, "the package's source is in the test's data")
	sent = map[string]string{}
	for _, f := range files {
		for _, d := range f.Decls {
			// A function, or a package-level variable (an error made once).
			var fn ast.Node = d
			name := "var"
			if f, ok := d.(*ast.FuncDecl); ok {
				name = f.Name.Name
			}
			where := fset.Position(d.Pos()).Filename + ":" + name
			if name == "apiError" {
				continue // the reason it's given
			}
			add := func(e ast.Expr) {
				switch e := e.(type) {
				case *ast.BasicLit:
					v, _ := strconv.Unquote(e.Value)
					sent[v] = where
					return
				case *ast.Ident:
					if v, ok := consts[e.Name]; ok {
						sent[v] = where
						return
					}
				}
				// Made as it runs (from a table, say): every literal in the
				// function that could be a reason is taken as one.
				ast.Inspect(fn, func(n ast.Node) bool {
					if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if v, _ := strconv.Unquote(lit.Value); isReason.MatchString(v) {
							sent[v] = where
						}
					}
					return true
				})
				dynamic = append(dynamic, where)
			}
			ast.Inspect(fn, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.CallExpr:
					if id, ok := n.Fun.(*ast.Ident); ok && id.Name == "apiError" && len(n.Args) > 1 {
						add(n.Args[1])
					}
				case *ast.CompositeLit:
					if sel, ok := n.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "ErrorInfo" {
						for _, e := range n.Elts {
							if kv, ok := e.(*ast.KeyValueExpr); ok {
								if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Reason" {
									add(kv.Value)
								}
							}
						}
					}
				}
				return true
			})
		}
	}
	return sent, dynamic
}

// listedReasons are the reasons SVC-30 (and the items after it) list.
func listedReasons(t *testing.T) (string, map[string]bool) {
	t.Helper()
	data, err := os.ReadFile(specPath)
	require.NoError(t, err)
	var text strings.Builder
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "- **SVC-30") {
			text.WriteString(line + "\n")
		}
	}
	listed := map[string]bool{}
	for _, m := range regexp.MustCompile("`([A-Z][A-Z0-9_]*)`").FindAllStringSubmatch(text.String(), -1) {
		listed[m[1]] = true
	}
	require.NotEmpty(t, listed, "SVC-30 found in %s", specPath)
	return text.String(), listed
}

// Every reason the service sends is listed in spec_021 SVC-30, where a
// client's author looks for them, and every reason listed is still sent:
// a new reason fails here until it's listed (N26 service#1).
func TestReasonsAreListed(t *testing.T) {
	sent, dynamic := sentReasons(t)
	text, listed := listedReasons(t)
	for reason, where := range sent {
		assert.True(t, listed[reason], "%s sends %s, which spec_021 SVC-30 doesn't list", where, reason)
	}
	for reason := range listed {
		if strings.HasPrefix(reason, "RUN_") && reason != "RUN_IN_PROGRESS" && reason != "RUN_NOT_RUNNING" && reason != "RUN_NOT_FOUND" {
			continue // a run's end, below
		}
		switch reason {
		case "FAILED_PRECONDITION": // a code's name, made as it runs
			continue
		case "TERMS_REQUIRED", "FORBIDDEN": // the organization's, passed on
			continue
		}
		_, ok := sent[reason]
		assert.True(t, ok, "spec_021 SVC-30 lists %s, which nothing sends", reason)
	}
	// The reasons made as the service runs: a run's end, a refusal passed
	// on from the organization's service, and a Connect code's name.
	slices.Sort(dynamic)
	assert.Equal(t, []string{"enterprise.go:enterpriseError", "scheduler.go:start", "server.go:toAPI", "server.go:withReason", "worker.go:runMsg"}, slices.Compact(dynamic),
		"a reason made as the service runs: describe it in SVC-30, then add it here")
	for _, status := range []api.RunStatus{api.RunFailed, api.RunLimited, api.RunSkipped, api.RunStopped} {
		assert.Contains(t, text, "`RUN_"+strings.ToUpper(string(status))+"`", "a run's end")
	}
	assert.Contains(t, text, "upper-cased", "the organization's codes passed on")
	assert.Contains(t, text, "code's name", "a Connect error with no reason of its own")
}

// Every reason SVC-30 lists has words in the page's catalog
// (desktop.error.reason.<REASON>), shown when an error comes without a
// message (N26 desktop-4#25); the other languages' are checked with the
// rest of their keys.
func TestReasonsHaveWords(t *testing.T) {
	_, listed := listedReasons(t)
	b, errs := i18n.NewBundle()
	require.Empty(t, errs)
	en, ok := b.Catalog("en-US")
	require.True(t, ok)
	for reason := range listed {
		_, ok := en.Messages["desktop.error.reason."+reason]
		assert.True(t, ok, "spec_021 SVC-30 lists %s: add desktop.error.reason.%s to the catalogs", reason, reason)
	}
}

// Every error a handler returns has an ErrorInfo: one that isn't a Connect
// error goes through toAPI, and a Connect error without one gets its
// code's name (SVC-30a); one cancelled is left as it is (errorInfo reads
// it as INTERNAL here; a client, as its context's error).
func TestWithReason(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		code   connect.Code
		reason string
	}{
		{"a Go error", errors.New("boom"), connect.CodeInternal, "INTERNAL"},
		{"a typed error", fmt.Errorf("x: %w", api.ErrMaxTurns), connect.CodeResourceExhausted, "MAX_TURNS"},
		{"a bare Connect error", connect.NewError(connect.CodeFailedPrecondition, errors.New("no")), connect.CodeFailedPrecondition, "FAILED_PRECONDITION"},
		{"cancelled: as it is, for the client's context", context.Canceled, connect.CodeCanceled, "INTERNAL"},
		{"invalid", invalid(errors.New("bad")), connect.CodeInvalidArgument, "INVALID_ARGUMENT"},
		{"its own", apiError(connect.CodeNotFound, "UNKNOWN_RUN", errors.New("gone")), connect.CodeNotFound, "UNKNOWN_RUN"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := withReason(tc.err)
			assert.Equal(t, tc.code, connect.CodeOf(err))
			assert.Equal(t, tc.reason, errorInfo(err).GetReason())
		})
	}
	assert.NoError(t, withReason(nil))
}
