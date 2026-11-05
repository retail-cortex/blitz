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

package engine

import (
	"context"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/lsp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// In a workspace whose project settings wait for trust, no language server
// starts for the editor; once trusted, documents open (VE-43).
func TestOpenDocumentUntrusted(t *testing.T) {
	ws := projectWorkspace(t)
	w := openProject(t, ws, Options{})
	defer w.Close()
	assert.False(t, w.LanguageTrusted())
	info, err := w.OpenDocument(context.Background(), "w", "main.go", "package main\n", 1, nil)
	require.NoError(t, err)
	assert.Equal(t, LanguageUntrusted, info.State)
	assert.Empty(t, info.ID)
	assert.Contains(t, info.Detail, "aren't trusted")

	require.NoError(t, w.TrustProject(w.ProjectSettings().Hash, true))
	assert.True(t, w.LanguageTrusted())
	info, err = w.OpenDocument(context.Background(), "w", "main.go", "package main\n", 1, nil)
	require.NoError(t, err)
	assert.NotEqual(t, LanguageUntrusted, info.State)
	assert.NotEmpty(t, info.ID)
	require.NoError(t, w.CloseDocument(info.ID))

	_, err = w.OpenDocument(context.Background(), "w", "../out.go", "", 1, nil)
	assert.ErrorIs(t, err, ErrBadPath)
}

// Whether the project's files may act (language servers, a browser
// profile) follows trust as it is now, and a run that trusts them
// (--trust-project) counts (N26 engine-top-2#20).
func TestProjectTrusted(t *testing.T) {
	for _, tc := range []struct {
		name   string
		o      Options
		trust  bool // trusted after it opened
		forget bool // trusted, then forgotten
		want   bool
	}{
		{name: "waiting for trust", want: false},
		{name: "this run trusts it", o: Options{TrustProject: true}, want: true},
		{name: "trusted since", trust: true, want: true},
		{name: "trusted, then forgotten", trust: true, forget: true, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := projectWorkspace(t)
			w := openProject(t, ws, tc.o)
			defer w.Close()
			if tc.trust {
				require.NoError(t, w.TrustProject(w.ProjectSettings().Hash, true))
			}
			if tc.forget {
				w.project.State = api.TrustTrusted // as if it opened trusted
				require.NoError(t, w.TrustProject(w.ProjectSettings().Hash, false))
			}
			assert.Equal(t, tc.want, w.LanguageTrusted())
			assert.Equal(t, tc.want, w.projectTrusted())
		})
	}
}

// A Markdown file's code block opens as a document of its own; one
// without a language or an ID is refused.
func TestOpenCodeBlock(t *testing.T) {
	ws := projectWorkspace(t)
	w := openProject(t, ws, Options{})
	defer w.Close()
	require.NoError(t, w.TrustProject(w.ProjectSettings().Hash, true))
	info, err := w.OpenDocument(context.Background(), "w", "notes.md", "x := 1\n", 1, &CodeBlock{Extension: ".GO", ID: "b1"})
	require.NoError(t, err)
	assert.NotEmpty(t, info.ID)
	assert.Equal(t, "go", info.Language)
	require.NoError(t, w.CloseDocument(info.ID))

	for _, block := range []*CodeBlock{{Extension: "go", ID: "b1"}, {Extension: ".go"}, {Extension: "../x", ID: "b1"}} {
		_, err = w.OpenDocument(context.Background(), "w", "notes.md", "", 1, block)
		assert.ErrorIs(t, err, ErrBadPath)
	}
}

// Locations outside the workspace are marked.
func TestSourceLocations(t *testing.T) {
	got := sourceLocations([]lsp.Location{{Path: "main.go"}, {Path: "/usr/lib/go/src/fmt/print.go"}, {Path: "../other/x.go"}})
	assert.Equal(t, []bool{false, true, true}, []bool{got[0].Outside, got[1].Outside, got[2].Outside})
}
