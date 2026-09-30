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
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A process's notice goes to the running turn, or waits for the next
// prompt.
func TestProcessNotices(t *testing.T) {
	w, llm := openTestWith(t, nil, text("seen"))
	s, _ := w.NewSession()

	w.processNotice(s.ID, "Background process 1 (`make`) exited with code 0")
	assert.Equal(t, []string{"Background process 1 (`make`) exited with code 0"}, w.TakeProcessNotices(s.ID))
	assert.Empty(t, w.TakeProcessNotices(s.ID), "once")

	w.processNotice(s.ID, "Background process 2 (`serve`) printed: listening on :8080")
	_, err := w.Run(context.Background(), s.ID, api.Turn{Text: "go on"}, func(api.Event) {})
	require.NoError(t, err)
	assert.Contains(t, userTextAt(llm.Requests[0].Contents), "listening on :8080", "with the next prompt")

	w.turnsMu.Lock()
	w.inTurn[s.ID]++
	w.turnsMu.Unlock()
	w.engine.OpenSteers(s.ID)
	w.processNotice(s.ID, "Background process 3 (`test`) exited with code 1")
	assert.Equal(t, []string{"(background process) Background process 3 (`test`) exited with code 1"}, w.engine.TakeSteers(s.ID), "during a turn: a steer")
	assert.Empty(t, w.TakeProcessNotices(s.ID))

	for range maxNotices + 5 {
		w.processNotice("other", "x")
	}
	assert.Len(t, w.TakeProcessNotices("other"), maxNotices)
}
