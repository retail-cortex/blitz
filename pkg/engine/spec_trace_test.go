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
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	specdoc "github.com/retail-cortex/blitz/pkg/engine/specs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
)

const ordersSpec = "# 002. Orders\nStatus: draft\nDepends on: 001-overview\n## Requirements\n- **ORD-01** Checkout takes under 30 s at a till.\n- **ORD-02** Online orders wait in a queue.\n## Done when\nOrders are paid.\n"

// writeTree puts files in the workspace.
func writeTree(t *testing.T, w *Workspace, files map[string]string) {
	t.Helper()
	for name, text := range files {
		p := filepath.Join(w.Dir(), filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(text), 0o644))
	}
}

func readWS(t *testing.T, w *Workspace, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(w.Dir(), filepath.FromSlash(rel)))
	require.NoError(t, err)
	return string(data)
}

// Approve writes the status, the baseline and the ledger; a change
// approved again keeps a diff (SDD-10, SDD-11).
func TestApproveSpec(t *testing.T) {
	w := openTest(t)
	at := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	specNow = func() time.Time { return at }
	t.Cleanup(func() { specNow = time.Now })
	ctx := context.Background()
	writeSpecs(t, w, map[string]string{"001-overview.md": shopSpecs["001-overview.md"], "002-orders.md": ordersSpec + "## Open questions\n- Cash?\n"})

	_, err := w.ApproveSpec(ctx, ".agents/specs/README.md", false)
	assert.ErrorIs(t, err, ErrNotASpec)
	_, err = w.ApproveSpec(ctx, ".agents/specs/002-orders.md", false)
	assert.ErrorIs(t, err, ErrSpecNotReady)
	assert.ErrorContains(t, err, "1 open question(s)")

	a, err := w.ApproveSpec(ctx, ".agents/specs/002-orders.md", true)
	require.NoError(t, err)
	assert.Empty(t, a.Diff, "the first approval")
	assert.Contains(t, a.Text, "\nStatus: approved\n")
	assert.Equal(t, a.Text, readWS(t, w, ".agents/specs/002-orders.md"))
	assert.Equal(t, a.Text, readWS(t, w, ".agents/.ledger/baselines/002-orders.md"))
	var l specdoc.Ledger
	require.NoError(t, json.Unmarshal([]byte(readWS(t, w, ".agents/.ledger/specs/002-orders.json")), &l))
	assert.Equal(t, at, l.Approved.At)
	assert.Len(t, l.Items, 2)

	reqs, err := w.TraceSpecs(".agents/specs/002-orders.md")
	require.NoError(t, err)
	require.Len(t, reqs, 2)
	assert.Empty(t, reqs[0].Change)

	// The spec changes: the trace says how, and approving again keeps a diff.
	changed := strings.Replace(a.Text, "under 30 s", "under 20 s", 1)
	changed = strings.Replace(changed, "- **ORD-02** Online orders wait in a queue.\n", "- **ORD-03** Refunds within 30 days.\n", 1)
	writeSpecs(t, w, map[string]string{"002-orders.md": changed})
	reqs, err = w.TraceSpecs("")
	require.NoError(t, err)
	var got []string
	for _, r := range reqs {
		if r.Spec == "002-orders.md" {
			got = append(got, r.ID+" "+r.Change)
		}
	}
	assert.Equal(t, []string{"ORD-01 changed", "ORD-03 added", "ORD-02 removed"}, got)
	specs, err := w.ListSpecs()
	require.NoError(t, err)
	assert.True(t, specs[1].Approved)
	assert.Equal(t, 3, specs[1].Changed)

	at = at.Add(time.Hour)
	a, err = w.ApproveSpec(ctx, ".agents/specs/002-orders.md", true)
	require.NoError(t, err)
	assert.Equal(t, ".agents/.ledger/diffs/20261107-130000-002-orders.diff", a.Diff)
	diff := readWS(t, w, a.Diff)
	assert.Contains(t, diff, "--- a/.agents/specs/002-orders.md")
	assert.Contains(t, diff, "\n-- **ORD-01** Checkout takes under 30 s at a till.\n")
	assert.Contains(t, diff, "\n+- **ORD-01** Checkout takes under 20 s at a till.\n")

	// Changed on disk while it was being approved: refused.
	writeSpecs(t, w, map[string]string{"002-orders.md": ordersSpec})
	_, err = w.ApproveSpec(ctx, ".agents/specs/002-missing.md", true)
	assert.Error(t, err)
}

// Verify finds each ID in the work; the model judges from that evidence
// (SDD-12).
func TestVerifySpec(t *testing.T) {
	w := openTest(t)
	ctx := context.Background()
	writeSpecs(t, w, map[string]string{"001-overview.md": shopSpecs["001-overview.md"], "002-orders.md": ordersSpec})
	writeTree(t, w, map[string]string{
		"internal/cart/checkout.go":      "package cart\n\n// Pay takes a payment (ORD-01).\nfunc Pay() {}\n",
		"internal/cart/checkout_test.go": "package cart\n\n// ORD-01: under 30 s.\n",
		"docs/orders.md":                 "ORD-02 is planned.\n",
		"logo.png":                       "\x89PNG\x00ORD-02",
	})

	v, err := w.VerifySpec(ctx, ".agents/specs/002-orders.md", false, false)
	require.NoError(t, err)
	require.Len(t, v.Requirements, 2)
	r1, r2 := v.Requirements[0], v.Requirements[1]
	assert.Equal(t, "traced", r1.State)
	require.Len(t, r1.Evidence, 2)
	assert.Equal(t, "internal/cart/checkout.go", r1.Evidence[0].Path)
	assert.Equal(t, 3, r1.Evidence[0].Line)
	assert.Equal(t, "test", r1.Evidence[1].Kind)
	assert.Equal(t, "untraced", r2.State, "a document only mentions it; the picture isn't read")
	assert.False(t, r1.Stale)
	assert.Zero(t, v.CostUSD)

	specs, err := w.ListSpecs()
	require.NoError(t, err)
	assert.Equal(t, 1, specs[1].Traced)

	llm := &promptLLM{reply: `{"requirements": [{"id": "ORD-01", "state": "Implemented", "reason": "checkout.go:3 takes the payment."}, {"id": "ORD-02", "state": "missing", "reason": "Only planned in docs/orders.md."}, {"id": "ORD-09", "state": "done"}]}`}
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return llm, nil }
	est, err := w.VerifySpec(ctx, ".agents/specs/002-orders.md", true, true)
	require.NoError(t, err)
	assert.Empty(t, llm.prompts, "an estimate asks nothing")
	assert.Equal(t, "traced", est.Requirements[0].State, "nor writes")

	v, err = w.VerifySpec(ctx, ".agents/specs/002-orders.md", true, false)
	require.NoError(t, err)
	require.Len(t, llm.prompts, 1)
	assert.Contains(t, llm.prompts[0], "internal/cart/checkout.go (code), line 3:\n1: package cart\n")
	assert.Contains(t, llm.prompts[0], "## ORD-02: Online orders wait in a queue.\n")
	assert.Equal(t, "implemented", v.Requirements[0].State)
	assert.True(t, v.Requirements[0].Judged)
	assert.Equal(t, "Only planned in docs/orders.md.", v.Requirements[1].Reason)
	specs, err = w.ListSpecs()
	require.NoError(t, err)
	assert.Equal(t, 1, specs[1].Implemented)

	// The text changes after verification: stale.
	writeSpecs(t, w, map[string]string{"002-orders.md": strings.Replace(ordersSpec, "30 s", "20 s", 1)})
	reqs, err := w.TraceSpecs(".agents/specs/002-orders.md")
	require.NoError(t, err)
	assert.True(t, reqs[0].Stale)
	assert.False(t, reqs[1].Stale)

	llm.reply = "Looks done."
	v, err = w.VerifySpec(ctx, ".agents/specs/002-orders.md", true, false)
	require.NoError(t, err)
	assert.Contains(t, v.Problem, "couldn't be read")
	assert.Equal(t, "traced", v.Requirements[0].State, "the check's result stands")
	llm.err = errors.New("quota exceeded")
	v, err = w.VerifySpec(ctx, ".agents/specs/002-orders.md", true, false)
	require.NoError(t, err)
	assert.Contains(t, v.Problem, "quota exceeded")
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return nil, errors.New("no key") }
	v, err = w.VerifySpec(ctx, ".agents/specs/002-orders.md", true, true)
	require.NoError(t, err)
	assert.Contains(t, v.Problem, "no key")

	_, err = w.VerifySpec(ctx, "docs/orders.md", false, false)
	assert.ErrorIs(t, err, ErrNotASpec)
	_, err = w.TraceSpecs("docs/orders.md")
	assert.ErrorIs(t, err, ErrNotASpec)
}

func TestParseVerdicts(t *testing.T) {
	_, ok := parseVerdicts("no")
	assert.False(t, ok)
	v, ok := parseVerdicts("```json\n{\"requirements\": [{\"id\": \" A-01 \", \"state\": \"partial\", \"reason\": \" half \"}]}\n```")
	require.True(t, ok)
	assert.Equal(t, verdict{ID: " A-01 ", State: "partial", Reason: "half"}, v["A-01"])
}
