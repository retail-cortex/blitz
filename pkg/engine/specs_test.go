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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSpecStage(t *testing.T) {
	for _, tc := range []struct {
		name      string
		stem      string
		text      string
		wantTitle string
		wantStat  string
		wantKind  string
		wantDeps  []string
		wantItems []SpecItem
		wantDone  []string
		wantOpen  []string
	}{
		{
			name:      "overview with kind and requirements",
			stem:      "001-overview",
			text:      "# 001. Overview\nStatus: in-progress\nKind: software\nDepends on: none\n\n## Context\nA household recipe planner.\n\n## Goals\n- Plan weekly meals\n\n## Non-goals\n- Grocery delivery\n\n## Requirements\n- **OVR-01** — Support 4 household members under 200 ms.\n- `OVR-02`: Offline recipe browsing.\n\n## Done when\n- All stages pass end-to-end checks.\n\n## Open questions\n- None.\n",
			wantTitle: "Overview",
			wantStat:  "in-progress",
			wantKind:  "software",
			wantItems: []SpecItem{
				{ID: "OVR-01", Section: "requirements", Text: "Support 4 household members under 200 ms."},
				{ID: "OVR-02", Section: "requirements", Text: "Offline recipe browsing."},
			},
			wantDone: []string{"All stages pass end-to-end checks."},
		},
		{
			name:      "stage with dependencies and open questions",
			stem:      "003-api",
			text:      "# 003 — HTTP API\n**Status:** done\n**Depends on:** `001-overview.md`, 002\n\n## Context\nServes JSON endpoints.\n\n## Objectives\n- [x] **API-01** — Validate ingredients.\n\n## Acceptance criteria\n- Contract tests pass.\n\n## Open questions\n- Rate limit per IP or per account?\n",
			wantTitle: "HTTP API",
			wantStat:  "done",
			wantDeps:  []string{"001-overview", "002"},
			wantItems: []SpecItem{
				{ID: "API-01", Section: "objectives", Text: "Validate ingredients.", Done: true},
			},
			wantDone: []string{"Contract tests pass."},
			wantOpen: []string{"Rate limit per IP or per account?"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseSpecStage(tc.stem, ".agents/specs/"+tc.stem+".md", tc.text)
			assert.Equal(t, tc.wantTitle, got.Title, "stage title")
			assert.Equal(t, tc.wantStat, got.Status, "stage status")
			assert.Equal(t, tc.wantKind, got.Kind, "project kind")
			assert.Equal(t, tc.wantDeps, got.DependsOn, "parsed dependencies")
			assert.Equal(t, tc.wantItems, got.Items, "parsed items")
			assert.Equal(t, tc.wantDone, got.DoneWhen, "done-when criteria")
			assert.Equal(t, tc.wantOpen, got.OpenQuestions, "open questions")
		})
	}
}

func TestWorkspaceSpecsAndUpdate(t *testing.T) {
	w := openTest(t)
	specsDir := w.ProjectSpecsDir()

	// Missing directory returns no stages.
	assert.Empty(t, w.Specs().Stages, "missing specs dir")
	require.NoError(t, os.MkdirAll(specsDir, 0o755))

	// Empty directory returns no stages.
	empty := w.Specs()
	assert.Empty(t, empty.Stages, "no stage files yet")

	// Write README, two stage specs, one skill and one agent citing 002-data-model.
	require.NoError(t, os.WriteFile(filepath.Join(specsDir, "README.md"), []byte("# Specifications\n\nA recipe app for households (Kind: software).\n\n- 001-overview.md\n- 002-data-model.md\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(specsDir, "001-overview.md"), []byte("# 001. Overview\nStatus: done\nKind: software\nDepends on: none\n\n## Context\nShared constraints.\n\n## Requirements\n- **OVR-01** — Fast startup.\n"), 0o644))
	stage2Body := "# 002. Data model\nStatus: draft\nDepends on: 001\n\n## Context\nSQLite schema for recipes.\n\n## Requirements\n- **DAT-01** — Recipes and ingredients tables.\n\n## Open questions\n- Soft delete or hard delete?\n"
	require.NoError(t, os.WriteFile(filepath.Join(specsDir, "002-data-model.md"), []byte(stage2Body), 0o644))

	skillDir := filepath.Join(w.Dir(), ".agents", "skills", "sqlite-migrations")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: sqlite-migrations\ndescription: Schema migrations for 002-data-model\n---\nFollow .agents/specs/002-data-model.md.\n"), 0o644))

	agentDir := filepath.Join(w.Dir(), ".agents", "agents")
	require.NoError(t, os.MkdirAll(agentDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "db-reviewer.md"), []byte("---\nname: db-reviewer\ndescription: Checks 002-data-model\n---\nVerify 002-data-model requirements.\n"), 0o644))

	old := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(filepath.Join(skillDir, "SKILL.md"), old, old))
	require.NoError(t, os.Chtimes(filepath.Join(agentDir, "db-reviewer.md"), old, old))

	sum := w.Specs()
	require.Len(t, sum.Stages, 2, "two numbered stages")
	assert.Equal(t, "software", sum.Kind, "kind from 001-overview")
	assert.Equal(t, "A recipe app for households (Kind: software).", sum.Overview, "overview from README.md")
	assert.True(t, sum.HarnessStale, "specs are newer than skills and agents")

	st2 := sum.Stages[1]
	assert.Equal(t, "002-data-model", st2.Name, "second stage stem")
	assert.Equal(t, []string{"001-overview"}, st2.DependsOn, "001 resolved to 001-overview")
	assert.Equal(t, []string{"sqlite-migrations"}, st2.Skills, "citing skill linked")
	assert.Equal(t, []string{"db-reviewer"}, st2.Agents, "citing agent linked")

	t.Run("update status and dependencies preserves markdown body", func(t *testing.T) {
		updated, err := w.ApplySpecUpdate(SpecUpdate{Name: "002-data-model", Status: "in-progress", SetDependsOn: true, DependsOn: []string{"001-overview"}})
		require.NoError(t, err)
		assert.Equal(t, "in-progress", updated.Status, "updated status")
		assert.Equal(t, []string{"001-overview"}, updated.DependsOn, "kept dependency")

		disk, err := os.ReadFile(filepath.Join(specsDir, "002-data-model.md"))
		require.NoError(t, err)
		assert.Contains(t, string(disk), "Status: in-progress\nDepends on: 001-overview\n\n## Context\nSQLite schema for recipes.\n", "body preserved byte-for-byte")
	})

	t.Run("insert missing header lines when stage has none", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(specsDir, "003-ui.md"), []byte("# 003. UI\n\n## Context\nViews.\n"), 0o644))
		updated, err := w.ApplySpecUpdate(SpecUpdate{Name: "003-ui", Status: "done", SetDependsOn: true, DependsOn: []string{"002-data-model"}})
		require.NoError(t, err)
		assert.Equal(t, "done", updated.Status, "inserted status")
		assert.Equal(t, []string{"002-data-model"}, updated.DependsOn, "inserted dependency")
	})

	t.Run("reject unknown spec, bad status and cycles", func(t *testing.T) {
		_, err := w.ApplySpecUpdate(SpecUpdate{Name: "999-missing", Status: "done"})
		assert.ErrorIs(t, err, ErrUnknownSpec, "missing spec file")

		_, err = w.ApplySpecUpdate(SpecUpdate{Name: "../001-overview", Status: "done"})
		assert.ErrorIs(t, err, ErrUnknownSpec, "path traversal rejected")

		_, err = w.ApplySpecUpdate(SpecUpdate{Name: "002-data-model", Status: "invalid-status"})
		assert.ErrorIs(t, err, ErrBadSpecStatus, "unknown status rejected")

		_, err = w.ApplySpecUpdate(SpecUpdate{Name: "002-data-model", Status: "done", SetDependsOn: true, DependsOn: []string{"999-missing"}})
		assert.ErrorIs(t, err, ErrUnknownSpec, "unknown dependency rejected")

		_, err = w.ApplySpecUpdate(SpecUpdate{Name: "001-overview", Status: "done", SetDependsOn: true, DependsOn: []string{"003-ui"}})
		assert.ErrorIs(t, err, ErrSpecCycle, "cycle 001 -> 003 -> 002 -> 001 rejected")

		_, err = w.ApplySpecUpdate(SpecUpdate{Name: "", Title: "   "})
		assert.ErrorIs(t, err, ErrBadSpecTitle, "blank title rejected when creating stage")

		_, err = w.ApplySpecUpdate(SpecUpdate{Name: "", Title: "Search", Status: "bad"})
		assert.ErrorIs(t, err, ErrBadSpecStatus, "bad status rejected when creating stage")

		_, err = w.ApplySpecUpdate(SpecUpdate{Name: "", Title: "Search", DependsOn: []string{"999-missing"}})
		assert.ErrorIs(t, err, ErrUnknownSpec, "unknown dependency rejected when creating stage")
	})

	t.Run("create new stage, toggle checklists, add items and resolve questions", func(t *testing.T) {
		created, err := w.ApplySpecUpdate(SpecUpdate{
			Title:          "Search & Filter",
			Context:        "Full-text recipe search.",
			DependsOn:      []string{"002-data-model"},
			AddRequirement: "Query recipes by ingredient under 50 ms.",
			AddDoneWhen:    "Search index tests pass.",
		})
		require.NoError(t, err)
		assert.Equal(t, "004-search-filter", created.Name, "next stage number and slug")
		assert.Equal(t, []string{"002-data-model"}, created.DependsOn, "dependency wired")
		require.Len(t, created.Items, 1)
		assert.Equal(t, "SEA-01", created.Items[0].ID, "initial requirement ID")
		assert.False(t, created.Items[0].Done, "starts unchecked")

		// Toggle SEA-01 checkbox, add SEA-02, add an open question, and toggle Done when #1.
		toggled, err := w.ApplySpecUpdate(SpecUpdate{
			Name:                "004-search-filter",
			ToggleItemID:        "SEA-01",
			ItemDone:            true,
			ToggleDoneWhenIndex: 1,
			DoneWhenDone:        true,
			AddRequirement:      "Support dietary tag filters.",
			AddOpenQuestion:     "Use FTS5 or trigram index?",
		})
		require.NoError(t, err)
		require.Len(t, toggled.Items, 2)
		assert.True(t, toggled.Items[0].Done, "SEA-01 checked")
		assert.Equal(t, "SEA-02", toggled.Items[1].ID, "auto-numbered SEA-02")
		assert.Equal(t, []bool{true}, toggled.DoneWhenDone, "Done when #1 checked")
		assert.Equal(t, []string{"Use FTS5 or trigram index?"}, toggled.OpenQuestions, "open question added")

		// Dismiss open question #1 and uncheck SEA-01.
		resolved, err := w.ApplySpecUpdate(SpecUpdate{
			Name:                    "004-search-filter",
			ToggleItemID:            "SEA-01",
			ItemDone:                false,
			SetDependsOn:            true,
			DependsOn:               []string{"none"},
			RemoveOpenQuestionIndex: 1,
		})
		require.NoError(t, err)
		assert.Empty(t, resolved.OpenQuestions, "open question removed")
		assert.False(t, resolved.Items[0].Done, "SEA-01 unchecked")

		// Add two open questions and dismiss the first so the remaining question stays.
		_, err = w.ApplySpecUpdate(SpecUpdate{Name: "004-search-filter", AddOpenQuestion: "First question?"})
		require.NoError(t, err)
		withTwo, err := w.ApplySpecUpdate(SpecUpdate{Name: "004-search-filter", AddOpenQuestion: "Second question?"})
		require.NoError(t, err)
		require.Len(t, withTwo.OpenQuestions, 2)
		withOne, err := w.ApplySpecUpdate(SpecUpdate{Name: "004-search-filter", RemoveOpenQuestionIndex: 1})
		require.NoError(t, err)
		assert.Equal(t, []string{"Second question?"}, withOne.OpenQuestions, "remaining question kept")

		// Replace full stage markdown via Content (without a leading H1) and verify title restoration.
		redrafted, err := w.ApplySpecUpdate(SpecUpdate{
			Name:    "004-search-filter",
			Title:   "Search & Filter",
			Content: "Status: in-progress\nDepends on: 001-overview, 002-data-model\n\n## Context\nRedrafted with FTS5 decision.\n\n## Requirements\n- [x] **SEA-01** FTS5 query under 50 ms.\n\n## Done when\n- [ ] FTS5 tests pass.\n",
		})
		require.NoError(t, err)
		assert.Equal(t, "Search & Filter", redrafted.Title, "restored stage heading")
		assert.Equal(t, "in-progress", redrafted.Status, "parsed status from redrafted content")
		assert.Equal(t, []string{"001-overview", "002-data-model"}, redrafted.DependsOn, "parsed dependencies from redrafted content")

		// Append sections to a bare stage file that lacks Requirements, Done when, and Open questions headings,
		// and create a stage with pre-drafted Content when README.md is absent.
		require.NoError(t, os.Remove(filepath.Join(specsDir, "README.md")))
		bareCreated, err := w.ApplySpecUpdate(SpecUpdate{
			Title:   "AI",
			Content: "# 005. AI\nStatus: draft\nDepends on: none\n\n## Context\nModel routing.\n",
		})
		require.NoError(t, err)
		assert.Equal(t, "005-ai", bareCreated.Name, "short title slugged")
		assert.FileExists(t, filepath.Join(specsDir, "README.md"), "README recreated")

		appended, err := w.ApplySpecUpdate(SpecUpdate{
			Name:            "005-ai",
			AddRequirement:  "Fallback across providers.",
			AddDoneWhen:     "Fallback test passes.",
			AddOpenQuestion: "Default timeout?",
		})
		require.NoError(t, err)
		require.Len(t, appended.Items, 1)
		assert.Equal(t, "REQ-01", appended.Items[0].ID, "short slug falls back to REQ prefix")
		assert.Equal(t, []string{"Fallback test passes."}, appended.DoneWhen, "created Done when section")
		assert.Equal(t, []string{"Default timeout?"}, appended.OpenQuestions, "created Open questions section")
	})
}

func TestApplySpecUpdateSurgicalRoundTrip(t *testing.T) {
	w := openTest(t)
	specsDir := w.ProjectSpecsDir()
	require.NoError(t, os.MkdirAll(specsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(specsDir, "001-overview.md"), []byte("# 001. Overview\nStatus: done\nDepends on: none\n"), 0o644))

	const handWritten = "# 002. API\n" +
		"Status: approved\n" +
		"Depends on: 001-overview (shared rules), 000-legacy\n" +
		"\n" +
		"## Context\n" +
		"Hand-written notes with custom prose.\n" +
		"\n" +
		"## Requirements\n" +
		"- **API-01** — Serve it.\n" +
		"- [x] **API-02** — Log requests.\n" +
		"\n" +
		"## Done when\n" +
		"- Smoke test passes.\n" +
		"- [x] Load test passes.\n" +
		"\n" +
		"## Open questions\n" +
		"- Rate limit per IP?\n" +
		"- Token TTL?\n"

	stagePath := filepath.Join(specsDir, "002-api.md")
	require.NoError(t, os.WriteFile(stagePath, []byte(handWritten), 0o644))

	parsed := w.Specs()
	require.Len(t, parsed.Stages, 2)
	assert.Equal(t, "approved", parsed.Stages[1].Status, "preserves custom status")
	assert.NotEmpty(t, parsed.Stages[1].Hash, "computes content hash")

	for _, tc := range []struct {
		name   string
		update SpecUpdate
		want   string
	}{
		{
			name:   "tick requirement without checkbox preserves em-dash and headers",
			update: SpecUpdate{Name: "002-api", ToggleItemID: "API-01", ItemDone: true},
			want: "# 002. API\nStatus: approved\nDepends on: 001-overview (shared rules), 000-legacy\n\n## Context\nHand-written notes with custom prose.\n\n## Requirements\n" +
				"- [x] **API-01** — Serve it.\n- [x] **API-02** — Log requests.\n\n## Done when\n- Smoke test passes.\n- [x] Load test passes.\n\n## Open questions\n- Rate limit per IP?\n- Token TTL?\n",
		},
		{
			name:   "untick requirement preserves em-dash and headers",
			update: SpecUpdate{Name: "002-api", ToggleItemID: "API-02", ItemDone: false},
			want: "# 002. API\nStatus: approved\nDepends on: 001-overview (shared rules), 000-legacy\n\n## Context\nHand-written notes with custom prose.\n\n## Requirements\n" +
				"- **API-01** — Serve it.\n- [ ] **API-02** — Log requests.\n\n## Done when\n- Smoke test passes.\n- [x] Load test passes.\n\n## Open questions\n- Rate limit per IP?\n- Token TTL?\n",
		},
		{
			name:   "tick done-when criterion",
			update: SpecUpdate{Name: "002-api", ToggleDoneWhenIndex: 1, DoneWhenDone: true},
			want: "# 002. API\nStatus: approved\nDepends on: 001-overview (shared rules), 000-legacy\n\n## Context\nHand-written notes with custom prose.\n\n## Requirements\n" +
				"- **API-01** — Serve it.\n- [x] **API-02** — Log requests.\n\n## Done when\n- [x] Smoke test passes.\n- [x] Load test passes.\n\n## Open questions\n- Rate limit per IP?\n- Token TTL?\n",
		},
		{
			name:   "untick done-when criterion",
			update: SpecUpdate{Name: "002-api", ToggleDoneWhenIndex: 2, DoneWhenDone: false},
			want: "# 002. API\nStatus: approved\nDepends on: 001-overview (shared rules), 000-legacy\n\n## Context\nHand-written notes with custom prose.\n\n## Requirements\n" +
				"- **API-01** — Serve it.\n- [x] **API-02** — Log requests.\n\n## Done when\n- Smoke test passes.\n- [ ] Load test passes.\n\n## Open questions\n- Rate limit per IP?\n- Token TTL?\n",
		},
		{
			name:   "add requirement leaves headers and existing lines untouched",
			update: SpecUpdate{Name: "002-api", AddRequirement: "Validate payloads."},
			want: "# 002. API\nStatus: approved\nDepends on: 001-overview (shared rules), 000-legacy\n\n## Context\nHand-written notes with custom prose.\n\n## Requirements\n" +
				"- **API-01** — Serve it.\n- [x] **API-02** — Log requests.\n- [ ] **API-03** Validate payloads.\n\n## Done when\n- Smoke test passes.\n- [x] Load test passes.\n\n## Open questions\n- Rate limit per IP?\n- Token TTL?\n",
		},
		{
			name:   "add done-when criterion leaves headers and existing lines untouched",
			update: SpecUpdate{Name: "002-api", AddDoneWhen: "Fuzz test passes."},
			want: "# 002. API\nStatus: approved\nDepends on: 001-overview (shared rules), 000-legacy\n\n## Context\nHand-written notes with custom prose.\n\n## Requirements\n" +
				"- **API-01** — Serve it.\n- [x] **API-02** — Log requests.\n\n## Done when\n- Smoke test passes.\n- [x] Load test passes.\n- [ ] Fuzz test passes.\n\n## Open questions\n- Rate limit per IP?\n- Token TTL?\n",
		},
		{
			name:   "add open question leaves headers and existing lines untouched",
			update: SpecUpdate{Name: "002-api", AddOpenQuestion: "Retry backoff?"},
			want: "# 002. API\nStatus: approved\nDepends on: 001-overview (shared rules), 000-legacy\n\n## Context\nHand-written notes with custom prose.\n\n## Requirements\n" +
				"- **API-01** — Serve it.\n- [x] **API-02** — Log requests.\n\n## Done when\n- Smoke test passes.\n- [x] Load test passes.\n\n## Open questions\n- Rate limit per IP?\n- Token TTL?\n- Retry backoff?\n",
		},
		{
			name:   "remove open question leaves headers and existing lines untouched",
			update: SpecUpdate{Name: "002-api", RemoveOpenQuestionIndex: 1},
			want: "# 002. API\nStatus: approved\nDepends on: 001-overview (shared rules), 000-legacy\n\n## Context\nHand-written notes with custom prose.\n\n## Requirements\n" +
				"- **API-01** — Serve it.\n- [x] **API-02** — Log requests.\n\n## Done when\n- Smoke test passes.\n- [x] Load test passes.\n\n## Open questions\n- Token TTL?\n",
		},
		{
			name:   "updating depends_on alone leaves custom status untouched",
			update: SpecUpdate{Name: "002-api", SetDependsOn: true, DependsOn: []string{"001-overview"}},
			want: "# 002. API\nStatus: approved\nDepends on: 001-overview\n\n## Context\nHand-written notes with custom prose.\n\n## Requirements\n" +
				"- **API-01** — Serve it.\n- [x] **API-02** — Log requests.\n\n## Done when\n- Smoke test passes.\n- [x] Load test passes.\n\n## Open questions\n- Rate limit per IP?\n- Token TTL?\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, os.WriteFile(stagePath, []byte(handWritten), 0o644))
			curHash := w.Specs().Stages[1].Hash
			u := tc.update
			u.Hash = curHash
			st, err := w.ApplySpecUpdate(u)
			require.NoError(t, err)
			assert.NotEqual(t, curHash, st.Hash, "hash advances after edit")
			got, err := os.ReadFile(stagePath)
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
		})
	}

	t.Run("rejects stale hash with ErrSpecChanged", func(t *testing.T) {
		require.NoError(t, os.WriteFile(stagePath, []byte(handWritten), 0o644))
		_, err := w.ApplySpecUpdate(SpecUpdate{Name: "002-api", Hash: "sha256:stale", ToggleItemID: "API-01", ItemDone: true})
		assert.ErrorIs(t, err, ErrSpecChanged)
		got, err := os.ReadFile(stagePath)
		require.NoError(t, err)
		assert.Equal(t, handWritten, string(got), "file untouched on hash mismatch")
	})
}
