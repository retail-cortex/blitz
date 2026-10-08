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

package runtime

import (
	"context"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	cpsession "github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Truncating a session's log rewinds the conversation, in memory and (with
// the persistent service) on disk.
func TestTruncateSessionRewindsTheConversation(t *testing.T) {
	off := func(c *config.Config) { c.Context.Compaction = false }
	for _, persistent := range []bool{false, true} {
		dir := t.TempDir()
		var opts []Option
		if persistent {
			svc, _ := cpsession.NewPersistentService(dir)
			opts = append(opts, WithSessionService(svc))
		}
		f := newEngineWith(t, fixtureOpts{cfg: off, opts: opts}, textContent("r1"), textContent("r2"), textContent("r3"))
		ctx := context.Background()
		n := f.eng.EventCount(ctx, "s")
		require.Equal(t, 0, n, "a new session has %d events", n)
		runTurns(t, f.eng, "s", "q1")
		keep := f.eng.EventCount(ctx, "s")
		runTurns(t, f.eng, "s", "q2-dropped")
		require.NoError(t, f.eng.TruncateSession(ctx, "s", keep))
		n = f.eng.EventCount(ctx, "s")
		require.Equal(t, keep, n, "persistent=%v: %d events after truncating to %d", persistent, n, keep)
		runTurns(t, f.eng, "s", "q3")
		got := lastRequestText(f.llm)
		assert.NotContains(t, got, "q2-dropped", "persistent=%v: after truncating: %s", persistent, got)
		assert.NotContains(t, got, "r2", "persistent=%v: after truncating: %s", persistent, got)
		assert.Contains(t, got, "q1", "persistent=%v: after truncating: %s", persistent, got)
		assert.Error(t, f.eng.TruncateSession(ctx, "s", 999), "persistent=%v: truncating past the end", persistent)
		if persistent { // a later process reads the truncated log
			svc2, _ := cpsession.NewPersistentService(dir)
			g := newEngineWith(t, fixtureOpts{cfg: off, opts: []Option{WithSessionService(svc2)}}, textContent("later"))
			runTurns(t, g.eng, "s", "q4")
			got := lastRequestText(g.llm)
			assert.NotContains(t, got, "q2-dropped", "after reopening: %s", got)
			assert.Contains(t, got, "q3", "after reopening: %s", got)
		}
	}
}

func TestCompactAtEitherSideOfAPrompt(t *testing.T) {
	ctx := context.Background()
	f := newEngineWith(t, fixtureOpts{}, textContent("r1"), textContent("r2"), textContent("r3"),
		textContent("SUMMARY-FROM"), textContent("after"))
	runTurns(t, f.eng, "s", "q1")
	at := f.eng.EventCount(ctx, "s")
	runTurns(t, f.eng, "s", "q2", "q3")
	_, err := f.eng.CompactAt(ctx, "s", "", at+1, false)
	assert.Error(t, err, "compacting at an event that doesn't start a prompt")
	res, err := f.eng.CompactAt(ctx, "s", "", at, false) // q2 and q3
	require.NoError(t, err)
	assert.Equal(t, 4, res.EventsCompacted, "compacted %d events", res.EventsCompacted)
	runTurns(t, f.eng, "s", "q4")
	got := lastRequestText(f.llm)
	for _, want := range []string{"q1", "r1", "SUMMARY-FROM", "q4"} {
		t.Run(want, func(t *testing.T) {
			assert.Contains(t, got, want, "missing %q: %s", want, got)
		})
	}
	for _, gone := range []string{"q2", "r3"} {
		t.Run(gone, func(t *testing.T) {
			assert.NotContains(t, got, gone, "summarized %q still sent: %s", gone, got)
		})
	}

	g := newEngineWith(t, fixtureOpts{}, textContent("r1"), textContent("r2"), textContent("SUMMARY-UPTO"), textContent("after"))
	runTurns(t, g.eng, "s", "q1")
	at = g.eng.EventCount(ctx, "s")
	runTurns(t, g.eng, "s", "q2")
	_, err = g.eng.CompactAt(ctx, "s", "", 0, true)
	assert.Error(t, err, "summarizing up to the first prompt")
	_, err = g.eng.CompactAt(ctx, "s", "", at, true)
	require.NoError(t, err)
	runTurns(t, g.eng, "s", "q3")
	got = lastRequestText(g.llm)
	assert.Contains(t, got, "SUMMARY-UPTO", "up to here: %s", got)
	assert.NotContains(t, got, "q1", "up to here: %s", got)
	assert.Contains(t, got, "q2", "up to here: %s", got)
}

// With no session kept in memory between turns, a turn still runs in its
// session (pinned while it runs), and the next one in it sees its history,
// loaded again from the file.
func TestTurnsWithSessionsDroppedBetween(t *testing.T) {
	old := cpsession.Resident
	cpsession.Resident = 0
	t.Cleanup(func() { cpsession.Resident = old })
	svc, err := cpsession.NewPersistentService(t.TempDir())
	require.NoError(t, err)
	f := newEngineWith(t, fixtureOpts{opts: []Option{WithSessionService(svc)}}, textContent("r1"), textContent("r2"), textContent("r3"))
	runTurns(t, f.eng, "a", "q1")
	runTurns(t, f.eng, "b", "other")
	runTurns(t, f.eng, "a", "q2")
	got := lastRequestText(f.llm)
	for _, want := range []string{"q1", "r1", "q2"} {
		assert.Contains(t, got, want)
	}
	assert.NotContains(t, got, "other")
}
