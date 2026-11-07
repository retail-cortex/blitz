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
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
)

// writeSpecs puts files in the workspace's specs folder.
func writeSpecs(t *testing.T, w *Workspace, files map[string]string) {
	t.Helper()
	dir := filepath.Join(w.Dir(), ".agents", "specs")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	for name, text := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644))
	}
}

var shopSpecs = map[string]string{
	"README.md":       "# Specification\n\n1. [Overview](001-overview.md)\n",
	"001-overview.md": "# 001. Overview\nStatus: approved\nKind: software\n## Requirements\n- **OV-01** One store with 3 tills.\n## Done when\nIt takes orders.\n",
	"002-orders.md":   "# 002. Orders\nStatus: draft\nDepends on: 001-overview\n## Requirements\n- **ORD-01** Checkout is fast.\n## Done when\nOrders are paid.\n## Open questions\n- Cash?\n",
	"notes.txt":       "not a spec",
}

// The specification, in order, each with what the checks found (SDD-01).
func TestListSpecs(t *testing.T) {
	w := openTest(t)
	none, err := w.ListSpecs()
	require.NoError(t, err)
	assert.Empty(t, none, "no folder: no specs")

	writeSpecs(t, w, shopSpecs)
	got, err := w.ListSpecs()
	require.NoError(t, err)
	require.Len(t, got, 3, "Markdown only")
	assert.Equal(t, "README.md", got[0].File)
	assert.Equal(t, 1, got[0].Warnings, "002 isn't in the index")
	assert.Equal(t, "Overview", got[1].Title)
	assert.Equal(t, "approved", got[1].Status)
	assert.Equal(t, 1, got[1].Items)
	assert.Zero(t, got[1].Warnings+got[1].Errors)
	assert.Equal(t, ".agents/specs/002-orders.md", got[2].Path)
	assert.Equal(t, []int{1}, got[2].DependsOn)
	assert.Equal(t, 1, got[2].Warnings, "fast")
	assert.Equal(t, 1, got[2].OpenQuestions)
}

// A spec is reviewed as edited, by the checks and, asked, by the model
// (SDD-02, SDD-03).
func TestReviewSpec(t *testing.T) {
	w := openTest(t)
	writeSpecs(t, w, shopSpecs)
	ctx := context.Background()

	_, err := w.ReviewSpec(ctx, "docs/x.md", "", false)
	assert.ErrorIs(t, err, ErrNotASpec)

	edited := "# 002. Orders\nStatus: draft\nDepends on: 009\n- **OV-01** Taken again.\n"
	rev, err := w.ReviewSpec(ctx, ".agents/specs/002-orders.md", edited, false)
	require.NoError(t, err)
	var codes []string
	for _, f := range rev.Findings {
		codes = append(codes, f.Code)
	}
	assert.Subset(t, codes, []string{"unknown-dependency", "duplicate-id", "no-done-when"}, "the edited text, against the files")
	assert.Zero(t, rev.CostUSD, "no model")

	llm := &promptLLM{reply: "```json\n{\"findings\": [{\"line\": 4, \"id\": \"ORD-01\", \"severity\": \"warning\", \"message\": \"Say how fast: under 30 s at the till.\"}, {\"line\": 0, \"severity\": \"critical\", \"message\": \"No refunds.\"}, {\"message\": \" \"}]}\n```"}
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return llm, nil }
	rev, err = w.ReviewSpec(ctx, ".agents/specs/002-orders.md", shopSpecs["002-orders.md"], true)
	require.NoError(t, err)
	var judged []string
	for _, f := range rev.Findings {
		if f.Judged {
			judged = append(judged, f.Severity+" "+f.Message)
		}
	}
	assert.Equal(t, []string{"warning Say how fast: under 30 s at the till.", "warning No refunds."}, judged, "an unknown severity is a warning; empty ones dropped")
	require.Len(t, llm.prompts, 1)
	assert.Contains(t, llm.prompts[0], "5: - **ORD-01** Checkout is fast.", "numbered by line")
	assert.Contains(t, llm.prompts[0], "OV-01 One store with 3 tills.", "the rest of the specification")

	llm.reply = "Looks fine to me."
	rev, err = w.ReviewSpec(ctx, ".agents/specs/002-orders.md", shopSpecs["002-orders.md"], true)
	require.NoError(t, err)
	assert.Contains(t, rev.Problem, "couldn't be read")
	assert.NotEmpty(t, rev.Findings, "the checks' findings stay")

	llm.err = errors.New("quota exceeded")
	rev, err = w.ReviewSpec(ctx, ".agents/specs/002-orders.md", shopSpecs["002-orders.md"], true)
	require.NoError(t, err)
	assert.Contains(t, rev.Problem, "quota exceeded")

	rev, err = w.ReviewSpec(ctx, ".agents/specs/README.md", shopSpecs["README.md"], true)
	require.NoError(t, err)
	require.Len(t, rev.Findings, 1)
	assert.Equal(t, "unlisted", rev.Findings[0].Code, "the index: its own check, never the model")
}

// Refine is the composer with the spec recipe: its draft is checked
// against the rest of the specification (SDD-04).
func TestComposeSpecRecipe(t *testing.T) {
	w := openTest(t)
	writeSpecs(t, w, shopSpecs)
	r, err := w.recipe("spec")
	require.NoError(t, err)
	assert.Contains(t, r.Brief, "Done when")
	assert.Empty(t, r.Check("002-orders", shopSpecs["002-orders.md"]))
	assert.Equal(t, []string{"OV-01 is also in 001-overview.md (line 5)"}, r.Check("002-orders", "# 002. Orders\nStatus: draft\n- **OV-01** again\nDone when: x\n"))
}
