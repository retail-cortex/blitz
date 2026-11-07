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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blitz spec list|review (spec_057_spec_driven SDD-06).
func TestSpecCommand(t *testing.T) {
	isolate(t)
	ws := t.TempDir()
	out, err := runCLI(t, "spec", "list", "--dir", ws)
	require.NoError(t, err)
	assert.Contains(t, out, "No specs")

	dir := filepath.Join(ws, ".agents", "specs")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	for name, text := range map[string]string{
		"README.md":       "# Specification\n1. [Overview](001-overview.md)\n2. [Orders](002-orders.md)\n",
		"001-overview.md": "# 001. Overview\nStatus: approved\n- **OV-01** One store with 3 tills.\nDone when: it takes orders.\n",
		"002-orders.md":   "# 002. Orders\nStatus: draft\nDepends on: 007\n- **ORD-01** Checkout is fast.\n- **ORD-02** Card and cash.\nOpen questions:\n- Refunds?\n",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644))
	}
	out, err = runCLI(t, "spec", "list", "--dir", ws)
	require.NoError(t, err)
	assert.Contains(t, out, "001-overview.md\tapproved\tOverview\t1 requirement\n")
	assert.Contains(t, out, "002-orders.md\tdraft\tOrders\t2 requirements, 1 error, 2 warnings, 1 open question\n")
	assert.Contains(t, out, "README.md\t-\tSpecification\t0 requirements\n")

	out, err = runCLI(t, "spec", "review", "001-overview.md", "--dir", ws)
	require.NoError(t, err, "a sound spec")
	assert.Empty(t, out)

	out, err = runCLI(t, "spec", "review", "--dir", ws)
	assert.ErrorIs(t, err, errSpecErrors, "every spec, and an error fails it")
	assert.Contains(t, out, ".agents/specs/002-orders.md:3: error: it depends on 007, which isn't a spec\n")
	assert.Contains(t, out, `.agents/specs/002-orders.md:4: warning: ORD-01: "fast" is a judgement`)

	_, err = runCLI(t, "spec", "review", "nope.md", "--dir", ws)
	assert.Error(t, err)

	out, _ = runCLI(t, "spec", "review", "001-overview.md", "--judge", "--dir", ws)
	assert.Contains(t, out, "The model's review cost $0.0000.", "no model here: its problem, and nothing spent")
}
