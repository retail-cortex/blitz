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
	"log/slog"
	"sync"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	adksession "google.golang.org/adk/v2/session"
)

// subagentEvents is what a turn does with the events of a sub-agent it runs
// in the foreground (runtime.WithSubagentEvents): its tool calls, results
// and task list show in the conversation as they happen, under the
// sub-agent's name, and every event goes to the session's sub-agent log.
// Its words go only to the agent that asked, as the tool's result, so they
// aren't shown or kept as the conversation's.
func (w *Workspace) subagentEvents(st *session.Storage, id string, on func(api.Event)) func(*adksession.Event) {
	return func(ev *adksession.Event) {
		if ev == nil || ev.Partial {
			return
		}
		if err := st.AppendSubagentEvent(id, ev); err != nil {
			slog.Warn("sub-agent event not saved", "session", id, "agent", ev.Author, "error", err)
		}
		for _, e := range events(ev) {
			if e.Text == nil {
				on(e)
			}
		}
	}
}

// serialized makes on safe to call from several goroutines at once, as a
// turn's sub-agents and the turn itself send their events.
func serialized(on func(api.Event)) func(api.Event) {
	var mu sync.Mutex
	return func(e api.Event) {
		mu.Lock()
		defer mu.Unlock()
		on(e)
	}
}
