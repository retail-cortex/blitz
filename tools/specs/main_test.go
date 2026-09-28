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

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A spec naming a missing path, or missing from the index, is reported;
// examples, planned files and home paths aren't.
func TestCheck(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	write := func(p, text string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(text), 0o644))
	}
	write("pkg/real/real.go", "package real")
	write("specs/README.md", "[a](spec_a_001.md)")
	write("specs/spec_a_001.md", "Source: `pkg/real/real.go`, `pkg/real/*.go`, `pkg/{real,gone}`, `internal/app`; "+
		"for example `internal/cart/x.go`; `docs/X.md` (planned); `~/.blitz/x`; `//pkg/real`; `pkg/engine/…`.")
	write("specs/spec_b_002.md", "Nothing.")
	problems, n, err := check("specs")
	require.NoError(t, err)
	want := []string{
		"spec_a_001.md: `internal/app` doesn't exist",
		"spec_a_001.md: `pkg/{real,gone}` doesn't exist",
		"spec_b_002.md: not in the specs index (README.md)",
	}
	assert.Equal(t, 2, n, "%d specs, problems:\n%s", n, strings.Join(problems, "\n"))
	assert.Equal(t, strings.Join(want, "\n"), strings.Join(problems, "\n"), "%d specs, problems:\n%s", n, strings.Join(problems, "\n"))
}
