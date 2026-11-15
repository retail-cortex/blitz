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

package enterprise

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/enterprise/enterprisetest"
)

func write(t *testing.T, root, rel, text string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(text), 0o644))
}

func read(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	require.NoError(t, err)
	return string(b)
}

// EN-93: the person's own assets sync both ways: sent when changed here,
// brought here when changed elsewhere, removed on the other side when
// removed on one; changed on both, this computer's wins and the other's is
// kept under conflicts; what can't sync is said and left alone.
func TestSyncPersonal(t *testing.T) {
	_, s, c, _, _ := setup(t)
	ctx := context.Background()
	require.NoError(t, c.SignInBrowser(ctx, browse(t), nil))
	root, state := t.TempDir(), filepath.Join(t.TempDir(), "personal.json")
	_, err := SyncPersonal(ctx, c, root, state)
	assert.Error(t, err, "a service before them")

	s.Mu.Lock()
	s.Personal, s.PersonalAllowed = map[string]*enterprisetest.PersonalAsset{}, map[string]bool{"agent": true, "skill": true, "workflow": true}
	s.Mu.Unlock()
	write(t, root, "agents/a.md", "---\nname: a\n---\nA.\n")
	write(t, root, "agents/unreadable.md", "x")
	write(t, root, "skills/s/SKILL.md", "---\nname: s\n---\nS.\n")
	write(t, root, "skills/s/scripts/x.sh", "echo hi\n")
	write(t, root, "skills/s/.DS_Store", "junk")
	write(t, root, "skills/bin/SKILL.md", "---\nname: bin\n---\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "skills", "bin", "logo.png"), []byte{0xff, 0xfe, 0x00}, 0o644))
	write(t, root, "skills/Big Notes/SKILL.md", "x")
	write(t, root, "workflows/w.flow.yaml", "nodes: []\n")
	write(t, root, "rules/r.md", "Be brief.\n")
	require.NoError(t, os.Symlink(filepath.Join(root, "skills", "s"), filepath.Join(root, "skills", "linked")))

	rep, err := SyncPersonal(ctx, c, root, state)
	require.NoError(t, err)
	assert.Equal(t, []string{"agent/a", "skill/s", "workflow/w"}, rep.Sent)
	assert.Contains(t, rep.Skipped["rule/r"], "doesn't keep")
	assert.Contains(t, rep.Skipped["skill/bin"], "isn't text")
	assert.Contains(t, rep.Skipped["skill/Big Notes"], "its name")
	assert.Equal(t, "It doesn't read.", rep.Skipped["agent/unreadable"])
	assert.NotContains(t, rep.Sent, "skill/linked", "a linked folder is someone else's")
	s.Mu.Lock()
	assert.NotContains(t, s.Personal["skill/s"].Files, "skills/s/.DS_Store")
	assert.Equal(t, "echo hi\n", s.Personal["skill/s"].Files["skills/s/scripts/x.sh"])
	s.Mu.Unlock()

	steps := []struct {
		name   string
		change func()
		check  func(SyncReport)
	}{
		{"nothing changed", func() {}, func(r SyncReport) { assert.Empty(t, append(append(r.Sent, r.Received...), r.Removed...)) }},
		{"changed on another computer", func() {
			s.Mu.Lock()
			s.Personal["agent/a"] = &enterprisetest.PersonalAsset{Revision: 2, Files: map[string]string{"agents/a.md": "---\nname: a\n---\nA, there.\n"}}
			s.Mu.Unlock()
		}, func(r SyncReport) {
			assert.Equal(t, []string{"agent/a"}, r.Received)
			assert.Contains(t, read(t, root, "agents/a.md"), "A, there.")
		}},
		{"changed here", func() { write(t, root, "skills/s/SKILL.md", "---\nname: s\n---\nS, here.\n") }, func(r SyncReport) {
			assert.Equal(t, []string{"skill/s"}, r.Sent)
		}},
		{"changed on both", func() {
			write(t, root, "agents/a.md", "---\nname: a\n---\nMine.\n")
			s.Mu.Lock()
			s.Personal["agent/a"] = &enterprisetest.PersonalAsset{Revision: 3, Files: map[string]string{"agents/a.md": "---\nname: a\n---\nTheirs.\n"}}
			s.Mu.Unlock()
		}, func(r SyncReport) {
			assert.Equal(t, []string{"agent/a"}, r.Conflicts)
			assert.Contains(t, read(t, root, "conflicts/agents/a.md"), "Theirs.")
			assert.Contains(t, read(t, root, "agents/a.md"), "Mine.")
			s.Mu.Lock()
			assert.Contains(t, s.Personal["agent/a"].Files["agents/a.md"], "Mine.", "this computer's wins")
			s.Mu.Unlock()
		}},
		{"removed on another computer", func() {
			s.Mu.Lock()
			delete(s.Personal, "workflow/w")
			s.Mu.Unlock()
		}, func(r SyncReport) {
			assert.Equal(t, []string{"workflow/w"}, r.Removed)
			assert.NoFileExists(t, filepath.Join(root, "workflows", "w.flow.yaml"))
		}},
		{"removed here", func() { require.NoError(t, os.RemoveAll(filepath.Join(root, "skills", "s"))) }, func(r SyncReport) {
			assert.Equal(t, []string{"skill/s"}, r.Removed)
			s.Mu.Lock()
			assert.NotContains(t, s.Personal, "skill/s")
			s.Mu.Unlock()
		}},
		{"new on another computer", func() {
			s.Mu.Lock()
			s.Personal["skill/t"] = &enterprisetest.PersonalAsset{Revision: 1, Files: map[string]string{"skills/t/SKILL.md": "T", "skills/t/refs/a.md": "ref"}}
			s.Mu.Unlock()
		}, func(r SyncReport) {
			assert.Equal(t, []string{"skill/t"}, r.Received)
			assert.Equal(t, "ref", read(t, root, "skills/t/refs/a.md"))
		}},
		{"a file that isn't the asset's", func() {
			s.Mu.Lock()
			s.Personal["agent/evil"] = &enterprisetest.PersonalAsset{Revision: 1, Files: map[string]string{"agents/../../x.md": "x"}}
			s.Mu.Unlock()
		}, func(r SyncReport) {
			assert.NoFileExists(t, filepath.Join(filepath.Dir(root), "x.md"))
		}},
	}
	for _, st := range steps {
		t.Run(st.name, func(t *testing.T) {
			st.change()
			rep, err := SyncPersonal(ctx, c, root, state)
			if st.name == "a file that isn't the asset's" {
				assert.ErrorContains(t, err, "isn't one of evil's files")
			} else {
				require.NoError(t, err)
			}
			st.check(rep)
		})
	}

	// From an old revision: changed elsewhere.
	_, err = c.PutPersonal(ctx, "agent", "a", map[string]string{"agents/a.md": "x"}, 1)
	assert.ErrorIs(t, err, ErrChangedElsewhere)
	assert.ErrorIs(t, c.DeletePersonal(ctx, "agent", "a", 1), ErrChangedElsewhere)
}

// EN-94, EN-95: runs reported; a submission made, refused with the
// service's code, and listed.
func TestRunsAndShares(t *testing.T) {
	_, s, c, _, _ := setup(t)
	ctx := context.Background()
	require.NoError(t, c.SignInBrowser(ctx, browse(t), nil))
	end := time.Now()
	require.NoError(t, c.ReportRun(ctx, RunReport{Kind: "workflow", Name: "brief", Status: "succeeded", StartedAt: end.Add(-time.Minute), EndedAt: &end, CostUSD: 0.01}))
	s.Mu.Lock()
	require.Len(t, s.Reported, 1)
	assert.Equal(t, "brief", s.Reported[0]["name"])
	s.Mu.Unlock()

	sub, err := c.Share(ctx, "skill", "tone", map[string]string{"skills/tone/SKILL.md": "T"}, "for the team")
	require.NoError(t, err)
	assert.Equal(t, "judging", sub.State)
	list, err := c.Submissions(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "for the team", list[0].Note)
	s.Mu.Lock()
	s.ShareRefusal = "no_evaluator"
	s.Mu.Unlock()
	_, err = c.Share(ctx, "skill", "tone", nil, "")
	ref, ok := errorsAs(err)
	require.True(t, ok)
	assert.Equal(t, "no_evaluator", ref.Code)
}

func errorsAs(err error) (*Refusal, bool) { return errors.AsType[*Refusal](err) }
