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

package specs

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const orders = "# 002. Orders\nStatus: draft\n- **ORD-01** Checkout takes under 30 s.\n- **ORD-02** Card and cash.\nDone when: paid.\n"

// Approval records the requirements; changes are measured against it
// (SDD-10, SDD-11).
func TestLedgerApproveAndChanges(t *testing.T) {
	s := Parse("002-orders.md", orders)
	l, err := ReadLedger("002-orders.md", nil)
	require.NoError(t, err)
	assert.Nil(t, l.Changes(s), "never approved: nothing to compare")

	at := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	l.Approve(s, orders, at, "Ryan")
	assert.Equal(t, "Ryan", l.Approved.By)
	require.Len(t, l.Items, 2)
	assert.Empty(t, l.Changes(s))
	assert.Equal(t, Fingerprint("checkout takes   under 30 s."), l.Items["ORD-01"].Fingerprint, "case and spacing don't count")
	assert.NotEqual(t, Fingerprint("Checkout takes under 20 s."), l.Items["ORD-01"].Fingerprint)

	// Verified, then the spec changes.
	l.Items["ORD-02"].State = Traced
	edited := strings.Replace(strings.Replace(orders, "under 30 s", "under 20 s", 1), "- **ORD-02** Card and cash.\n", "- **ORD-03** Refunds.\n", 1)
	s2 := Parse("002-orders.md", edited)
	assert.Equal(t, []Change{{ID: "ORD-01", Kind: "changed"}, {ID: "ORD-03", Kind: "added"}, {ID: "ORD-02", Kind: "removed"}}, l.Changes(s2))

	// Approving again: unchanged requirements keep their verification.
	again := Parse("002-orders.md", strings.Replace(orders, "**ORD-02**", "ORD-02", 1))
	l.Approve(again, orders, at, "Ryan")
	assert.Equal(t, Traced, l.Items["ORD-02"].State, "only emphasis changed")
	l.Approve(s2, edited, at, "Ryan")
	assert.Empty(t, l.Items["ORD-01"].State, "changed: verified again")
	assert.NotContains(t, l.Items, "ORD-02")

	back, err := ReadLedger("002-orders.md", l.Bytes())
	require.NoError(t, err)
	assert.Equal(t, l.Approved.Hash, back.Approved.Hash)
	_, err = ReadLedger("002-orders.md", []byte("{"))
	assert.ErrorContains(t, err, ".agents/.ledger/specs/002-orders.json")
}

func TestLedgerPaths(t *testing.T) {
	assert.Equal(t, ".agents/.ledger/specs/002-orders.json", LedgerPath("002-orders.md"))
	assert.Equal(t, ".agents/.ledger/baselines/002-orders.md", BaselinePath("002-orders.md"))
	assert.Equal(t, ".agents/.ledger/diffs/20261107-120000-002-orders.diff", DiffPath("002-orders.md", time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)))
}

func TestSetStatus(t *testing.T) {
	assert.Contains(t, SetStatus(orders, "approved"), "\nStatus: approved\n")
	assert.Contains(t, SetStatus("# 1. A\n**Status:** draft\n", "approved"), "\nStatus: approved\n")
	assert.Equal(t, "# 1. A\n\nStatus: approved\nbody", SetStatus("# 1. A\nbody", "approved"), "after the title")
	assert.Equal(t, "Status: approved\n\nbody", SetStatus("body", "approved"))
}

func TestUnifiedDiff(t *testing.T) {
	a := "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12"
	b := "1\n2\nthree\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13"
	d := UnifiedDiff("002-orders.md", a, b)
	assert.True(t, strings.HasPrefix(d, "--- a/002-orders.md\n+++ b/002-orders.md\n@@ -1,6 +1,6 @@\n 1\n 2\n-3\n+three\n 4\n"), d)
	assert.Contains(t, d, "@@ -10,3 +10,4 @@\n 10\n 11\n 12\n+13\n")
	assert.Equal(t, "--- a/x\n+++ b/x\n", UnifiedDiff("x", a, a), "no changes: no hunks")
}

// The pass without a model: IDs found in code, tests, documents and
// commits (SDD-12).
func TestFinder(t *testing.T) {
	assert.Nil(t, NewFinder(nil))
	f := NewFinder([]string{"ORD-01", "ORD-02"})
	f.File("internal/cart/checkout.go", "package cart\n// ORD-01: under 30 s.\nfunc Pay() {} // see ORD-010, not ORD-01x\n")
	f.File("internal/cart/checkout_test.go", "// Checks ORD-01 and ORD-02.\n")
	f.File("docs/orders.md", "ORD-02 is planned.\n")
	f.File("plain.txt", "nothing here")
	f.Commit("abc1234", "Orders: refunds", "Orders: refunds\n\nImplements: ORD-02")
	assert.Equal(t, []Evidence{
		{Path: "internal/cart/checkout.go", Line: 2, Kind: "code", Text: "// ORD-01: under 30 s."},
		{Path: "internal/cart/checkout_test.go", Line: 1, Kind: "test", Text: "// Checks ORD-01 and ORD-02."},
	}, f.Found["ORD-01"], "not ORD-010 or ORD-01x")
	require.Len(t, f.Found["ORD-02"], 3)
	assert.Equal(t, Evidence{Path: "abc1234", Kind: "commit", Text: "Orders: refunds"}, f.Found["ORD-02"][2])
	assert.Equal(t, Traced, StateOf(f.Found["ORD-01"]))
	assert.Equal(t, Untraced, StateOf([]Evidence{{Kind: "doc"}}), "a document that mentions it doesn't implement it")
	assert.Equal(t, Untraced, StateOf(nil))

	for i := range 20 {
		f.File("many.go", strings.Repeat("\n", i)+"ORD-01")
	}
	assert.Len(t, f.Found["ORD-01"], MaxEvidence)

	for p, want := range map[string]string{"cart_test.go": "test", "tests/test_cart.py": "test", "src/cart.spec.ts": "test", "test_cart.py": "test", "README.md": "doc", "cart.py": "code", "notebooks/a.ipynb": "code"} {
		assert.Equal(t, want, EvidenceKind(p), p)
	}
	assert.Equal(t, "ab…", clip("abcdef", 3))
}
