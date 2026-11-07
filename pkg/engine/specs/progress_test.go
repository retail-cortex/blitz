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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func approved(s Spec, states map[string]string) Ledger {
	l, _ := ReadLedger(s.File, nil)
	l.Approve(s, "", time.Now(), "")
	for id, st := range states {
		l.Items[id].State = st
		l.Items[id].VerifiedFingerprint = l.Items[id].Fingerprint
		l.Items[id].Evidence = []Evidence{{Path: "internal/" + id + ".go", Line: 1, Kind: "code"}, {Path: "docs/x.md", Kind: "doc"}}
	}
	return l
}

// Done, next up and blocked (SDD-32).
func TestProgress(t *testing.T) {
	over := Parse("001-overview.md", "# 001. Overview\n- **OV-01** a\n")
	orders := Parse("002-orders.md", "# 002. Orders\nDepends on: 001\n- **ORD-01** a\n- **ORD-02** b\n")
	pay := Parse("003-pay.md", "# 003. Pay\nDepends on: 002\n- **PAY-01** a\n")
	notes := Parse("notes.md", "# Notes\n")
	assert.True(t, Done(over, approved(over, map[string]string{"OV-01": Traced})))
	assert.False(t, Done(over, approved(over, map[string]string{"OV-01": Untraced})))
	l, _ := ReadLedger(over.File, nil)
	assert.False(t, Done(over, l), "never approved")
	assert.False(t, Done(Parse("004-x.md", "# 004. X\n"), approved(Parse("004-x.md", "# 004. X\n"), nil)), "nothing to do isn't done")

	all := []Spec{over, orders, pay, notes}
	o := Progress(all, map[int]bool{1: true})
	assert.Equal(t, 2, o.Next)
	assert.Equal(t, map[int][]int{3: {2}}, o.Blocked)
	o = Progress(all, map[int]bool{})
	assert.Equal(t, 1, o.Next)
	assert.Equal(t, map[int][]int{2: {1}, 3: {2}}, o.Blocked)
	assert.Zero(t, Progress(all, map[int]bool{1: true, 2: true, 3: true}).Next, "all done")
}

// A turn's changed files, and why a requirement drifted (SDD-31).
func TestTouchedAndDrift(t *testing.T) {
	orders := Parse("002-orders.md", "# 002. Orders\n- **ORD-01** a\n- **ORD-02** b\n")
	l := approved(orders, map[string]string{"ORD-01": Traced, "ORD-02": Traced})
	assert.Equal(t, []string{"ORD-02"}, Touched(l, map[string]bool{"internal/ORD-02.go": true, "docs/x.md": true}), "not a document")
	assert.Empty(t, Touched(Ledger{Items: l.Items}, map[string]bool{"internal/ORD-02.go": true}), "never approved")

	assert.Equal(t, "changed", Drift("changed", Traced, Traced, false))
	assert.Equal(t, "lost", Drift("", Implemented, Untraced, false))
	assert.Equal(t, "judged", Drift("", Traced, Contradicted, true))
	assert.Empty(t, Drift("", Untraced, Untraced, false), "never found: not drift")
	assert.Empty(t, Drift("", "", Missing, false), "missing by the check alone isn't a verdict")
}

func TestPendingAndDoneWhen(t *testing.T) {
	orders := Parse("002-orders.md", "# 002. Orders\n- **ORD-01** a\n- **ORD-02** b\n")
	l := approved(orders, map[string]string{"ORD-01": Implemented, "ORD-02": Traced})
	got := Pending(orders, l)
	assert.Len(t, got, 1)
	assert.Equal(t, "ORD-02", got[0].ID)
	assert.Equal(t, "Orders are paid.\nRefunds work.", DoneWhen("# X\n## Done when\nOrders are paid.\nRefunds work.\n## Open questions\n- a\n"))
	assert.Equal(t, "it opens.", DoneWhen("**Done when:** it opens.\n**Open questions:**\n- x"))
	assert.Empty(t, DoneWhen("# X"))
}
