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

package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A summary is saved per session, replaced when written again, and read
// back newest first; a bad ID is never a path.
func TestSummaries(t *testing.T) {
	dir := SummariesDir(t.TempDir())
	none, err := Summaries(dir)
	require.NoError(t, err)
	assert.Empty(t, none, "no folder yet")

	day := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	text := "Fixing the provider dropdown: done, waiting for review.\n\n## Next\nPush the branch."
	old, err := SaveSummary(dir, Summary{Session: "session-1", Title: `The "old" one`, Messages: 4, Updated: day, Text: text})
	require.NoError(t, err)
	info, err := os.Stat(old.Path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "the person's own")
	_, err = SaveSummary(dir, Summary{Session: "session-2", Title: "New", Messages: 2, Updated: day.Add(time.Hour), Text: "first"})
	require.NoError(t, err)
	_, err = SaveSummary(dir, Summary{Session: "session-2", Title: "New", Messages: 6, Updated: day.Add(2 * time.Hour), Text: "second"})
	require.NoError(t, err)

	all, err := Summaries(dir)
	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.Equal(t, "session-2", all[0].Session)
	assert.Equal(t, "second", all[0].Text, "replaced")
	assert.Equal(t, 6, all[0].Messages)
	got, err := FindSummary(dir, "session-1")
	require.NoError(t, err)
	assert.Equal(t, `The "old" one`, got.Title)
	assert.True(t, day.Equal(got.Updated))
	assert.Equal(t, text, got.Text)

	_, err = FindSummary(dir, "../notes")
	assert.ErrorIs(t, err, os.ErrNotExist)
	_, err = SaveSummary(dir, Summary{Session: "../x", Text: "x"})
	assert.Error(t, err)
	_, err = SaveSummary(dir, Summary{Session: "s", Text: "  "})
	assert.Error(t, err, "an empty summary isn't kept")
	entries, _ := os.ReadDir(dir)
	assert.Len(t, entries, 2, "no temporary files left")
	assert.Equal(t, filepath.Join(dir, "session-1.md"), old.Path)
}

func TestSummaryStands(t *testing.T) {
	for _, tt := range []struct{ name, text, want string }{
		{"first paragraph", "The goal is X;\nhalf done.\n\n## Done\n- a", "The goal is X; half done."},
		{"after a heading", "## Where it stands\n- Shipping v2.\n\n## Next\nTag it.", "Shipping v2."},
		{"empty", "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Summary{Text: tt.text}.Stands())
		})
	}
}

func TestRenderRecent(t *testing.T) {
	assert.Empty(t, RenderRecent(nil))
	day := time.Date(2026, 10, 29, 9, 30, 0, 0, time.Local)
	got := RenderRecent([]RecentSession{
		{ID: "session-2", Title: "Provider fix", Updated: day, Summary: &Summary{Text: "Done, not pushed.\n\n## Next\nPush."}},
		{ID: "session-1", Title: "Greeting", Updated: day.Add(-time.Hour)},
		{ID: "session-0", Title: "Long", Updated: day, Summary: &Summary{Text: strings.Repeat("word ", 200)}},
	})
	assert.Contains(t, got, "recall_session")
	assert.Contains(t, got, `- 2026-10-29 09:30 · "Provider fix" (session-2): Done, not pushed.`)
	assert.Contains(t, got, `- 2026-10-29 08:30 · "Greeting" (session-1)`+"\n")
	assert.Less(t, len(strings.Split(got, "\n")[3]), maxStandsChars+60, "a long summary is cut short")
}
