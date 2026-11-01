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

// A spec naming a missing path, missing from the index, or named in the
// old form is reported;
// examples, planned files and home paths aren't.
func TestCheck(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	write := func(p, text string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(text), 0o644))
	}
	write("pkg/real/real.go", "package real")
	write("specs/_index.md", "[a](spec_001_a.md) [c](spec_c_003.md)")
	write("specs/spec_001_a.md", "Source: `pkg/real/real.go`, `pkg/real/*.go`, `pkg/{real,gone}`, `internal/app`; "+
		"for example `internal/cart/x.go`; `docs/X.md` (planned); `~/.blitz/x`; `//pkg/real`; `pkg/engine/…`.")
	write("specs/spec_002_b.md", "Nothing.")
	write("specs/spec_c_003.md", "Nothing.")
	problems, n, err := check("specs")
	require.NoError(t, err)
	want := []string{
		"spec_001_a.md: `internal/app` doesn't exist",
		"spec_001_a.md: `pkg/{real,gone}` doesn't exist",
		"spec_002_b.md: not in the specs index (_index.md)",
		"spec_c_003.md: not named spec_<NNN>_<name>.md",
	}
	assert.Equal(t, 3, n, "%d specs, problems:\n%s", n, strings.Join(problems, "\n"))
	assert.Equal(t, strings.Join(want, "\n"), strings.Join(problems, "\n"), "%d specs, problems:\n%s", n, strings.Join(problems, "\n"))
}
