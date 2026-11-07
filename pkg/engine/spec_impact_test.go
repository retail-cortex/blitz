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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// An approved spec, the work that implements it, and a change to it.
func changedShop(t *testing.T) (*Workspace, string) {
	t.Helper()
	w := openTest(t)
	at := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	specNow = func() time.Time { return at }
	t.Cleanup(func() { specNow = time.Now })
	writeSpecs(t, w, map[string]string{"001-overview.md": shopSpecs["001-overview.md"], "002-orders.md": ordersSpec})
	writeTree(t, w, map[string]string{
		"internal/cart/checkout.go":      "package cart\n\n// Pay takes a payment (ORD-01).\nfunc Pay() error {\n\treturn nil\n}\n",
		"internal/cart/checkout_test.go": "package cart\n\nfunc TestPay(t *testing.T) {\n\t_ = Pay()\n}\n",
		"cmd/shop/main.go":               "package main\n\nfunc main() {\n\tcart.Pay()\n\tcart.PayLater()\n}\n",
	})
	ctx := context.Background()
	a, err := w.ApproveSpec(ctx, ".agents/specs/002-orders.md", false)
	require.NoError(t, err)
	_, err = w.VerifySpec(ctx, ".agents/specs/002-orders.md", false, false)
	require.NoError(t, err)
	changed := strings.Replace(a.Text, "under 30 s", "under 10 s", 1)
	changed = strings.Replace(changed, "## Done when", "- **ORD-03** Refunds within 30 days.\n## Done when", 1)
	writeSpecs(t, w, map[string]string{"002-orders.md": changed})
	return w, changed
}

// A saved change to an approved spec is recorded: Status: changed and a
// diff in the ledger, once (SDD-20).
func TestRecordSpecChange(t *testing.T) {
	w, changed := changedShop(t)
	ctx := context.Background()
	r, err := w.RecordSpecChange(ctx, ".agents/specs/002-orders.md")
	require.NoError(t, err)
	assert.Equal(t, []api.SpecChange{
		{ID: "ORD-01", Kind: "changed", Old: "Checkout takes under 30 s at a till.", New: "Checkout takes under 10 s at a till."},
		{ID: "ORD-03", Kind: "added", New: "Refunds within 30 days."},
	}, r.Changes)
	assert.Contains(t, r.Text, "\nStatus: changed\n")
	assert.Equal(t, r.Text, readWS(t, w, ".agents/specs/002-orders.md"))
	assert.Equal(t, ".agents/.ledger/diffs/20261107-120000-002-orders.diff", r.Diff)
	assert.Contains(t, readWS(t, w, r.Diff), "+- **ORD-03** Refunds within 30 days.")

	again, err := w.RecordSpecChange(ctx, ".agents/specs/002-orders.md")
	require.NoError(t, err)
	assert.Empty(t, again.Diff, "the same change isn't kept twice")
	assert.Empty(t, again.Text, "it says changed already")
	assert.Len(t, again.Changes, 2)

	// Never approved, or unchanged: nothing.
	none, err := w.RecordSpecChange(ctx, ".agents/specs/001-overview.md")
	require.NoError(t, err)
	assert.Empty(t, none.Changes)
	writeSpecs(t, w, map[string]string{"002-orders.md": strings.Replace(changed, "under 10 s", "under 30 s", 1)})
	_, err = w.RecordSpecChange(ctx, ".agents/specs/README.md")
	assert.ErrorIs(t, err, ErrNotASpec)
}

// The scan without a model follows the changed requirement from where it
// was found to the declaration holding it and its uses; the model writes
// the report after its estimate (SDD-20, SDD-21).
func TestImpactSpec(t *testing.T) {
	w, _ := changedShop(t)
	ctx := context.Background()
	im, err := w.ImpactSpec(ctx, ".agents/specs/002-orders.md", false, false)
	require.NoError(t, err)
	require.Len(t, im.Changes, 2)
	require.Len(t, im.Symbols, 1)
	assert.Equal(t, api.SpecSymbol{Name: "Pay", Path: "internal/cart/checkout.go", Line: 4, Uses: 3}, im.Symbols[0], "its doc comment too")
	var places []string
	for _, p := range im.Places {
		places = append(places, fmt.Sprintf("%s:%d %s %s", p.Path, p.Line, p.Kind, p.Why))
	}
	assert.Equal(t, []string{"internal/cart/checkout.go:3 code ORD-01", "cmd/shop/main.go:4 code Pay", "internal/cart/checkout_test.go:4 test Pay"}, places, "not PayLater; its declaration once")

	llm := &promptLLM{reply: `{"summary": "Checkout must take under 10 s.", "steps": [{"title": "Tighten the test", "detail": "Make TestPay fail over 10 s.", "files": ["internal/cart/checkout_test.go"]}, {"title": " ", "detail": "dropped"}], "conflicts": [{"id": "OV-01", "why": "Three tills can't all pay in 10 s."}, {"id": "X", "why": ""}]}`}
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return llm, nil }
	est, err := w.ImpactSpec(ctx, ".agents/specs/002-orders.md", true, true)
	require.NoError(t, err)
	assert.Empty(t, llm.prompts, "an estimate asks nothing")
	assert.Empty(t, est.Report)

	im, err = w.ImpactSpec(ctx, ".agents/specs/002-orders.md", true, false)
	require.NoError(t, err)
	require.Len(t, llm.prompts, 1)
	assert.Contains(t, llm.prompts[0], "- ORD-01 changed from: Checkout takes under 30 s at a till.\n  to: Checkout takes under 10 s at a till.")
	assert.Contains(t, llm.prompts[0], "- Pay in internal/cart/checkout.go:4 (3 uses found)")
	assert.Contains(t, llm.prompts[0], "- OV-01 (001-overview.md)")
	assert.Equal(t, "Checkout must take under 10 s.", im.Summary)
	require.Len(t, im.Steps, 1)
	require.Len(t, im.Conflicts, 1)
	assert.Equal(t, ".agents/.ledger/impact/20261107-120000-002-orders.md", im.Report)
	report := readWS(t, w, im.Report)
	assert.Contains(t, report, "1. **Tighten the test** Make TestPay fail over 10 s. (internal/cart/checkout_test.go)")
	assert.Contains(t, report, "- **OV-01**: Three tills can't all pay in 10 s.")
	assert.Contains(t, report, "- internal/cart/checkout.go:3 (code, ORD-01)")

	llm.reply = "Hmm."
	im, err = w.ImpactSpec(ctx, ".agents/specs/002-orders.md", true, false)
	require.NoError(t, err)
	assert.Contains(t, im.Problem, "couldn't be read")
	assert.NotEmpty(t, im.Places, "the scan stands")
	llm.err = errors.New("quota exceeded")
	im, err = w.ImpactSpec(ctx, ".agents/specs/002-orders.md", true, false)
	require.NoError(t, err)
	assert.Contains(t, im.Problem, "quota exceeded")
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return nil, errors.New("no key") }
	im, err = w.ImpactSpec(ctx, ".agents/specs/002-orders.md", true, true)
	require.NoError(t, err)
	assert.Contains(t, im.Problem, "no key")

	// Unchanged: nothing to scan.
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), ".agents", "specs", "002-orders.md"), []byte(readWS(t, w, ".agents/.ledger/baselines/002-orders.md")), 0o644))
	im, err = w.ImpactSpec(ctx, ".agents/specs/002-orders.md", true, false)
	require.NoError(t, err)
	assert.Empty(t, im.Changes)
	assert.Empty(t, im.Places)
}

// A turn that changes the work behind approved requirements says so
// (SDD-31).
func TestSpecDriftNotice(t *testing.T) {
	create := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "create_file", Args: map[string]any{"path": "internal/cart/checkout.go", "content": "package cart\n\n// Pay takes a payment (ORD-01), faster.\nfunc Pay() error { return nil }\n"}}}}}
	w, _ := openTestWith(t, nil, create, text("done"))
	w.SetUI(func(context.Context, api.ApprovalRequest) (api.Decision, error) { return 1, nil }, nil)
	writeSpecs(t, w, map[string]string{"001-overview.md": shopSpecs["001-overview.md"], "002-orders.md": ordersSpec})
	writeTree(t, w, map[string]string{"internal/cart/checkout.go": "package cart\n\n// Pay takes a payment (ORD-01).\nfunc Pay() error { return nil }\n"})
	ctx := context.Background()
	_, err := w.ApproveSpec(ctx, ".agents/specs/002-orders.md", false)
	require.NoError(t, err)
	_, err = w.VerifySpec(ctx, ".agents/specs/002-orders.md", false, false)
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(w.Dir(), "internal", "cart", "checkout.go")))

	s, _ := w.NewSession()
	var notices []string
	_, err = w.Run(ctx, s.ID, api.Turn{Text: "speed up checkout"}, func(e api.Event) {
		if e.Notice != nil {
			notices = append(notices, e.Notice.Text)
		}
	})
	require.NoError(t, err)
	assert.Contains(t, strings.Join(notices, "\n"), "approved requirements ORD-01 (002-orders.md)")
}

// Verification says which approved requirements drifted, and keeps the
// model's verdict while it holds; the plan is what's left (SDD-30..32).
func TestSpecDriftAndPlan(t *testing.T) {
	w := openTest(t)
	ctx := context.Background()
	writeSpecs(t, w, map[string]string{"001-overview.md": shopSpecs["001-overview.md"], "002-orders.md": ordersSpec})
	writeTree(t, w, map[string]string{"internal/cart/checkout.go": "// ORD-01\nfunc Pay() {}\n", "internal/tills.go": "// OV-01\n"})

	specs, err := w.ListSpecs()
	require.NoError(t, err)
	assert.True(t, specs[0].Next, "001 first")
	assert.Equal(t, []int{1}, specs[1].BlockedBy)

	plan, err := w.PlanSpec(".agents/specs/002-orders.md")
	require.NoError(t, err)
	require.Len(t, plan.Pending, 2)
	assert.Equal(t, []int{1}, plan.BlockedBy)
	assert.Contains(t, plan.Prompt, "- ORD-01: Checkout takes under 30 s at a till.\n")
	assert.Contains(t, plan.Prompt, "\nDone when:\nOrders are paid.\n")
	assert.Contains(t, plan.Prompt, "(Implements: ID)")
	assert.Contains(t, plan.Prompt, "It depends on 001")

	// 001 done: 002 is next.
	_, err = w.ApproveSpec(ctx, ".agents/specs/001-overview.md", false)
	require.NoError(t, err)
	_, err = w.VerifySpec(ctx, ".agents/specs/001-overview.md", false, false)
	require.NoError(t, err)
	specs, err = w.ListSpecs()
	require.NoError(t, err)
	assert.True(t, specs[0].Done)
	assert.True(t, specs[1].Next)
	assert.Empty(t, specs[1].BlockedBy)

	// The model judges ORD-01 implemented; a free pass keeps it while it's found.
	_, err = w.ApproveSpec(ctx, ".agents/specs/002-orders.md", false)
	require.NoError(t, err)
	llm := &promptLLM{reply: `{"requirements": [{"id": "ORD-01", "state": "implemented", "reason": "checkout.go:1"}, {"id": "ORD-02", "state": "missing", "reason": "nothing"}]}`}
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return llm, nil }
	v, err := w.VerifySpec(ctx, ".agents/specs/002-orders.md", true, false)
	require.NoError(t, err)
	assert.Equal(t, []api.SpecDrift{{ID: "ORD-02", Why: "judged"}}, v.Drift)
	v, err = w.VerifySpec(ctx, ".agents/specs/002-orders.md", false, false)
	require.NoError(t, err)
	assert.Equal(t, "implemented", v.Requirements[0].State, "the verdict stands")
	assert.Equal(t, "untraced", v.Requirements[1].State, "the check's own")
	plan, err = w.PlanSpec(".agents/specs/002-orders.md")
	require.NoError(t, err)
	require.Len(t, plan.Pending, 1)
	assert.Equal(t, "ORD-02", plan.Pending[0].ID)

	// The work loses ORD-01, and the spec changes: both drift.
	writeTree(t, w, map[string]string{"internal/cart/checkout.go": "func Pay() {}\n"})
	writeSpecs(t, w, map[string]string{"002-orders.md": strings.Replace(readWS(t, w, ".agents/specs/002-orders.md"), "Online orders wait in a queue.", "Online orders wait in a queue for 10 minutes.", 1)})
	v, err = w.VerifySpec(ctx, ".agents/specs/002-orders.md", false, false)
	require.NoError(t, err)
	assert.Equal(t, []api.SpecDrift{{ID: "ORD-01", Why: "lost"}, {ID: "ORD-02", Why: "changed"}}, v.Drift)

	// Nothing left: no prompt.
	writeSpecs(t, w, map[string]string{"003-x.md": "# 003. X\nStatus: draft\n"})
	plan, err = w.PlanSpec(".agents/specs/003-x.md")
	require.NoError(t, err)
	assert.Empty(t, plan.Prompt)
	_, err = w.PlanSpec(".agents/specs/README.md")
	assert.ErrorIs(t, err, ErrNotASpec)
}
