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

package session

import (
	"time"

	adksession "google.golang.org/adk/v2/session"
)

// withoutSummarised is the events a session keeps in memory: all but those
// a summary already stands in for, which no prompt sends again (they stay
// in the session's file). Without this a long conversation's memory grew
// with every turn it ever had, compacted or not.
//
// An event is left out only when ADK's own prompt assembly would drop it,
// asked more strictly than ADK asks it, so the prompts built from what's
// kept are the same:
//   - a usable summary (with content, its range not inverted) on the event's branch
//     covers its time, and every summary whose range covers it was written
//     after it (a summary never stands in for what came later);
//   - no summary names it as a hole (a straggler, or one a correction
//     excluded);
//   - it changes no session state (state is rebuilt from the events) and
//     takes no part in a long-running tool call (ADK restores those calls
//     from the whole list).
//
// The first event left out under each summary's range is kept after all:
// ADK puts the summary where the first event it covers sat.
func withoutSummarised(events []*adksession.Event) []*adksession.Event {
	type record struct {
		at  int
		rng *adksession.EventCompaction
		ev  *adksession.Event
	}
	var records []record
	holes := map[holeKey]bool{}
	longRunning := map[string]bool{}
	for i, ev := range events {
		if ev == nil {
			continue
		}
		for _, id := range ev.LongRunningToolIDs {
			longRunning[id] = true
		}
		if c := ev.Actions.Compaction; c != nil {
			records = append(records, record{i, c, ev})
			for _, ref := range c.ExcludedEvents {
				holes[holeOf(ref.InvocationID, ref.Timestamp)] = true
			}
		}
	}
	if len(records) == 0 {
		return events
	}
	inRange := func(ev *adksession.Event, c *adksession.EventCompaction) bool {
		return !ev.Timestamp.Before(c.StartTimestamp) && !ev.Timestamp.After(c.EndTimestamp)
	}
	usable := func(c *adksession.EventCompaction) bool {
		return c.CompactedContent != nil && len(c.CompactedContent.Parts) > 0 && !c.EndTimestamp.Before(c.StartTimestamp)
	}
	covered := func(i int, ev *adksession.Event) bool {
		if ev.Actions.Compaction != nil || len(ev.Actions.StateDelta) > 0 || holes[holeOf(ev.InvocationID, ev.Timestamp)] || touches(ev, longRunning) {
			return false
		}
		by := false
		for _, r := range records {
			if !inRange(ev, r.rng) {
				continue
			}
			if r.at < i {
				return false
			}
			if usable(r.rng) && r.ev.Branch == ev.Branch && r.ev.IsolationScope == ev.IsolationScope {
				by = true
			}
		}
		return by
	}
	drop := make([]bool, len(events))
	anchored := make([]bool, len(records))
	for i, ev := range events {
		if ev == nil || !covered(i, ev) {
			continue
		}
		drop[i] = true
		for k, r := range records {
			if !anchored[k] && inRange(ev, r.rng) {
				anchored[k] = true
				drop[i] = false
			}
		}
	}
	out := make([]*adksession.Event, 0, len(events))
	for i, ev := range events {
		if !drop[i] {
			out = append(out, ev)
		}
	}
	return out
}

// holeKey names an event the way a summary's holes do, at the precision
// ADK compares them.
type holeKey struct {
	invocation string
	at         int64
}

func holeOf(invocation string, at time.Time) holeKey {
	return holeKey{invocation, at.Truncate(time.Microsecond).UnixNano()}
}

// touches reports whether ev calls or answers one of the long-running tool
// calls.
func touches(ev *adksession.Event, ids map[string]bool) bool {
	if len(ids) == 0 || ev.Content == nil {
		return false
	}
	for _, p := range ev.Content.Parts {
		if p == nil {
			continue
		}
		if call := p.FunctionCall; call != nil && ids[call.ID] {
			return true
		}
		if resp := p.FunctionResponse; resp != nil && ids[resp.ID] {
			return true
		}
	}
	return false
}
