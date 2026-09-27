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
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	cpsession "github.com/retail-cortex/blitz/pkg/engine/session"
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
		if n := f.eng.EventCount(ctx, "s"); n != 0 {
			t.Fatalf("a new session has %d events", n)
		}
		runTurns(t, f.eng, "s", "q1")
		keep := f.eng.EventCount(ctx, "s")
		runTurns(t, f.eng, "s", "q2-dropped")
		if err := f.eng.TruncateSession(ctx, "s", keep); err != nil {
			t.Fatal(err)
		}
		if n := f.eng.EventCount(ctx, "s"); n != keep {
			t.Fatalf("persistent=%v: %d events after truncating to %d", persistent, n, keep)
		}
		runTurns(t, f.eng, "s", "q3")
		got := lastRequestText(f.llm)
		if strings.Contains(got, "q2-dropped") || strings.Contains(got, "r2") || !strings.Contains(got, "q1") {
			t.Errorf("persistent=%v: after truncating: %s", persistent, got)
		}
		if err := f.eng.TruncateSession(ctx, "s", 999); err == nil {
			t.Errorf("persistent=%v: truncating past the end", persistent)
		}
		if persistent { // a later process reads the truncated log
			svc2, _ := cpsession.NewPersistentService(dir)
			g := newEngineWith(t, fixtureOpts{cfg: off, opts: []Option{WithSessionService(svc2)}}, textContent("later"))
			runTurns(t, g.eng, "s", "q4")
			if got := lastRequestText(g.llm); strings.Contains(got, "q2-dropped") || !strings.Contains(got, "q3") {
				t.Errorf("after reopening: %s", got)
			}
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
	if _, err := f.eng.CompactAt(ctx, "s", "", at+1, false); err == nil {
		t.Error("compacting at an event that doesn't start a prompt")
	}
	res, err := f.eng.CompactAt(ctx, "s", "", at, false) // q2 and q3
	if err != nil {
		t.Fatal(err)
	}
	if res.EventsCompacted != 4 {
		t.Errorf("compacted %d events", res.EventsCompacted)
	}
	runTurns(t, f.eng, "s", "q4")
	got := lastRequestText(f.llm)
	for _, want := range []string{"q1", "r1", "SUMMARY-FROM", "q4"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
	for _, gone := range []string{"q2", "r3"} {
		if strings.Contains(got, gone) {
			t.Errorf("summarized %q still sent: %s", gone, got)
		}
	}

	g := newEngineWith(t, fixtureOpts{}, textContent("r1"), textContent("r2"), textContent("SUMMARY-UPTO"), textContent("after"))
	runTurns(t, g.eng, "s", "q1")
	at = g.eng.EventCount(ctx, "s")
	runTurns(t, g.eng, "s", "q2")
	if _, err := g.eng.CompactAt(ctx, "s", "", 0, true); err == nil {
		t.Error("summarizing up to the first prompt")
	}
	if _, err := g.eng.CompactAt(ctx, "s", "", at, true); err != nil {
		t.Fatal(err)
	}
	runTurns(t, g.eng, "s", "q3")
	got = lastRequestText(g.llm)
	if !strings.Contains(got, "SUMMARY-UPTO") || strings.Contains(got, "q1") || !strings.Contains(got, "q2") {
		t.Errorf("up to here: %s", got)
	}
}
