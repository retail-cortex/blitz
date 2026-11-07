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

	// Approve and verify (SDD-11, SDD-12).
	_, err = runCLI(t, "spec", "approve", "002-orders.md", "--dir", ws)
	assert.ErrorContains(t, err, "isn't ready to approve")
	out, err = runCLI(t, "spec", "approve", "001-overview.md", "--dir", ws)
	require.NoError(t, err)
	assert.Contains(t, out, "Approved .agents/specs/001-overview.md.")
	require.NoError(t, os.MkdirAll(filepath.Join(ws, "internal"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ws, "internal", "tills.go"), []byte("package internal\n\n// OV-01: three tills.\n"), 0o644))
	out, err = runCLI(t, "spec", "verify", "--dir", ws)
	require.NoError(t, err)
	assert.Contains(t, out, "001-overview.md\tOV-01\ttraced\tinternal/tills.go:3\n")
	assert.Contains(t, out, "002-orders.md\tORD-02\tuntraced\t\n")
	out, err = runCLI(t, "spec", "verify", "001-overview.md", "--judge", "--dir", ws)
	require.NoError(t, err)
	assert.Contains(t, out, "--yes", "only the estimate, without --yes")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "001-overview.md"), []byte("# 001. Overview\nStatus: approved\n- **OV-01** One store with 4 tills.\nDone when: it takes orders.\n"), 0o644))
	out, err = runCLI(t, "spec", "verify", "001-overview.md", "--dir", ws)
	require.NoError(t, err)
	assert.Contains(t, out, "OV-01\ttraced (changed)\t")
	out, err = runCLI(t, "spec", "approve", "001-overview.md", "--dir", ws)
	require.NoError(t, err)
	assert.Contains(t, out, ".agents/.ledger/diffs/")

	// The change's impact (SDD-20, SDD-21): recorded, then scanned.
	require.NoError(t, os.WriteFile(filepath.Join(ws, "internal", "tills.go"), []byte("package internal\n\n// Open opens a till (OV-01).\nfunc Open() {}\n\nfunc use() { Open() }\n"), 0o644))
	out, err = runCLI(t, "spec", "impact", "001-overview.md", "--dir", ws)
	require.NoError(t, err)
	assert.Contains(t, out, "Nothing changed since it was approved.")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "001-overview.md"), []byte("# 001. Overview\nStatus: approved\n- **OV-01** One store with 5 tills.\nDone when: it takes orders.\n"), 0o644))
	out, err = runCLI(t, "spec", "impact", "001-overview.md", "--dir", ws)
	require.NoError(t, err)
	assert.Contains(t, out, ".agents/.ledger/diffs/")
	assert.Contains(t, out, "OV-01\tchanged\tOne store with 5 tills.\n")
	assert.Contains(t, out, "internal/tills.go:4\tOpen\t2 uses\n")
	assert.Contains(t, out, "internal/tills.go:3\tcode\tOV-01\n")
	assert.Contains(t, out, "internal/tills.go:6\tcode\tOpen\n")
	out, err = runCLI(t, "spec", "impact", "001-overview.md", "--judge", "--dir", ws)
	require.NoError(t, err)
	assert.Contains(t, out, "001-overview.md: ", "no model here: its problem")
	text, err := os.ReadFile(filepath.Join(dir, "001-overview.md"))
	require.NoError(t, err)
	assert.Contains(t, string(text), "Status: changed")

	// Drift in CI, and the plan (SDD-30, SDD-31, SDD-40).
	out, err = runCLI(t, "spec", "verify", "001-overview.md", "--ci", "--dir", ws)
	assert.ErrorIs(t, err, errSpecDrift)
	assert.Contains(t, out, "OV-01 changed since it was approved")
	out, err = runCLI(t, "spec", "implement", "002-orders.md", "--dir", ws)
	require.NoError(t, err)
	assert.Contains(t, out, "Implement Orders (.agents/specs/002-orders.md).")
	assert.Contains(t, out, "- ORD-01: Checkout is fast.")
	assert.Contains(t, out, "It waits for 007", "on stderr, mixed in here")
}
