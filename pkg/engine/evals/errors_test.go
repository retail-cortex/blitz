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

package evals

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Save fails with what went wrong; a table's columns it doesn't know are
// left out, and a case without a type or scope writes them empty.
func TestSaveErrorsAndTableEdges(t *testing.T) {
	dir := t.TempDir()
	e := Eval{Name: "shop", Cases: []Case{{ID: "c1", Prompt: "p", Output: "o"}}}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "shop.md"), []byte("---\n- a\n---\n"), 0o644))
	_, _, err := Save(dir, e, "shop")
	assert.ErrorContains(t, err, "the eval's")

	file := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(file, nil, 0o644))
	_, _, err = Save(filepath.Join(file, "sub"), e, "")
	assert.Error(t, err, "a folder that can't be made")

	ro := filepath.Join(dir, "ro")
	require.NoError(t, os.Mkdir(ro, 0o555))
	t.Cleanup(func() { os.Chmod(ro, 0o755) })
	if f, err := os.Create(filepath.Join(ro, "probe")); err == nil {
		f.Close()
	} else {
		_, _, err = Save(ro, e, "")
		assert.Error(t, err, "a folder that can't be written")
	}

	_, cases, _, err := ParseTable("| ID | Notes | Prompt | Output |\n| --- | --- | --- | --- |\n| c1 | ignored | p | o |\n")
	require.NoError(t, err)
	require.Len(t, cases, 1)
	assert.Equal(t, "p", cases[0].Prompt)
	assert.Contains(t, Table([]Case{{ID: "c1", Prompt: "p", Output: "o"}}), "| c1 |  | p | o |  |  |  |")
}
