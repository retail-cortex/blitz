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
	"testing"

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
	info, err := w.OpenDocument("w", "main.go", "package main\n", 1)
	require.NoError(t, err)
	assert.Equal(t, LanguageUntrusted, info.State)
	assert.Empty(t, info.ID)
	assert.Contains(t, info.Detail, "aren't trusted")

	require.NoError(t, w.TrustProject(w.ProjectSettings().Hash, true))
	assert.True(t, w.LanguageTrusted())
	info, err = w.OpenDocument("w", "main.go", "package main\n", 1)
	require.NoError(t, err)
	assert.NotEqual(t, LanguageUntrusted, info.State)
	assert.NotEmpty(t, info.ID)
	require.NoError(t, w.CloseDocument(info.ID))

	_, err = w.OpenDocument("w", "../out.go", "", 1)
	assert.ErrorIs(t, err, ErrBadPath)
}

// Locations outside the workspace are marked.
func TestSourceLocations(t *testing.T) {
	got := sourceLocations([]lsp.Location{{Path: "main.go"}, {Path: "/usr/lib/go/src/fmt/print.go"}, {Path: "../other/x.go"}})
	assert.Equal(t, []bool{false, true, true}, []bool{got[0].Outside, got[1].Outside, got[2].Outside})
}
